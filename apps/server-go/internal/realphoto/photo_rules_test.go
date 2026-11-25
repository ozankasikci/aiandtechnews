package realphoto_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

func TestFindRequiresCommonsMetadataToNameAPerson(t *testing.T) {
	const personPlan = `{"photographable":true,"subject":"Bret Taylor","kind":"person","queries":["Bret Taylor"]}`
	files := []commonsFile{
		{title: "File:Stage.jpg", objectName: "<b>Conference keynote</b>", description: "Another speaker at Taylorville.", license: "CC0", width: 1600, height: 900, thumb: "https://upload.wikimedia.org/stage.png"},
		{title: "File:Talk.jpg", objectName: "<b>Evening talk</b>", description: "<p>Bret <b>TAYLOR</b> &amp; guests.</p>" + strings.Repeat(" filler", 200) + " end marker", license: "CC0", width: 1600, height: 900, thumb: "https://upload.wikimedia.org/talk.png"},
		{title: "File:Bret_Taylor.jpg", description: "A conference portrait.", license: "CC0", width: 1600, height: 900, thumb: "https://upload.wikimedia.org/portrait.png"},
	}
	photo := serveBytes(solidPNG(t, 1600, 900, green), "image/png")
	for _, tc := range []struct {
		name, plan, slot string
		paths            []string
	}{
		{"person in body", personPlan, realphoto.SlotInline, []string{"/talk.png", "/portrait.png"}},
		{"person featured", personPlan, realphoto.SlotFeatured, []string{"/talk.png", "/portrait.png"}},
		{"featured stand-in", fundingPlan, realphoto.SlotFeatured, []string{"/talk.png", "/portrait.png"}},
		{"non-person in body", robotPlan, realphoto.SlotInline, []string{"/stage.png", "/talk.png", "/portrait.png"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var queries, downloads []string
			api := commonsAPI(t, files, &queries)
			routes := sites{
				"commons.wikimedia.org": func(w http.ResponseWriter, r *http.Request) {
					fields := strings.Split(r.URL.Query().Get("iiextmetadatafilter"), "|")
					for _, field := range []string{"ObjectName", "ImageDescription"} {
						if !slices.Contains(fields, field) {
							t.Errorf("metadata filter lacks %s", field)
						}
					}
					api(w, r)
				},
				"upload.wikimedia.org": func(w http.ResponseWriter, r *http.Request) {
					downloads = append(downloads, r.URL.Path)
					photo(w, r)
				},
			}
			vision := &fakeVision{verdicts: map[string]string{"green": verdict(9, "A speaker.")}}
			finder := realphoto.NewFinder(&fakeText{plan: tc.plan}, vision, &http.Client{Transport: routes}, nil)
			result := finder.Find(context.Background(), realphoto.Request{Title: "A keynote", Slot: tc.slot})
			if !result.Found || !slices.Equal(downloads, tc.paths) || vision.calls != len(tc.paths) {
				t.Fatalf("found %t, downloads %v, vision calls %d, want %v", result.Found, downloads, vision.calls, tc.paths)
			}
			if tc.plan != robotPlan {
				if !strings.Contains(result.Candidates[0].Reason, "surname") || !strings.Contains(result.Candidates[0].Reason, "Taylor") {
					t.Errorf("rejection reason = %q", result.Candidates[0].Reason)
				}
				for _, want := range []string{"title or description", "unmistakable visible context", "facial resemblance alone"} {
					if !strings.Contains(vision.prompts[0], want) {
						t.Errorf("person prompt lacks %q", want)
					}
				}
			}
			prompt := vision.prompts[slices.Index(tc.paths, "/talk.png")]
			for _, want := range []string{"Evening talk", "Bret TAYLOR & guests."} {
				if !strings.Contains(prompt, want) {
					t.Errorf("vision prompt lacks %q", want)
				}
			}
			if strings.Contains(prompt, "<b>") || strings.Contains(prompt, "<p>") || strings.Contains(prompt, "end marker") {
				t.Errorf("vision prompt contains unclean or unbounded metadata:\n%s", prompt)
			}
		})
	}
}

func TestOfficialPhotoPromptRequiresARealPhotographOnlyForFeatured(t *testing.T) {
	plan := realphoto.Plan{Subject: "WiCi One"}
	commons := realphoto.VerifyPromptFor(plan, "Headline", realphoto.KindCommons, realphoto.SlotFeatured)
	featured := realphoto.VerifyPromptFor(plan, "Headline", realphoto.KindOfficial, realphoto.SlotFeatured)
	inline := realphoto.VerifyPromptFor(plan, "Headline", realphoto.KindOfficial, realphoto.SlotInline)
	photoRule := func(prompt string) string {
		_, tail, _ := strings.Cut(prompt, "\nphotographic: ")
		line, _, _ := strings.Cut(tail, "\n")
		return line
	}
	if rule := photoRule(featured); rule != photoRule(commons) || !strings.Contains(rule, "only if it is a real photograph") {
		t.Errorf("featured official photo rule = %q", rule)
	}
	if rule := photoRule(inline); !strings.Contains(rule, "or a photorealistic official product or press image") {
		t.Errorf("inline official photo rule = %q", rule)
	}
}

func TestFeaturedPersonPhotoMustComeFromCommons(t *testing.T) {
	const personPlan = `{"photographable":true,"subject":"Bret Taylor","kind":"person","queries":["Bret Taylor"]}`
	var queries []string
	routes := sites{
		"commons.wikimedia.org": commonsAPI(t, nil, &queries),
		"publisher.test":        serveBytes([]byte(`<a href="https://sierra.test/team">Sierra</a>`), "text/html"),
		"sierra.test": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/team" {
				serveBytes([]byte(`<img src="/speaker.png">`), "text/html")(w, r)
				return
			}
			serveBytes(solidPNG(t, 1600, 900, green), "image/png")(w, r)
		},
	}
	vision := &fakeVision{verdicts: map[string]string{"green": verdict(9, "A speaker.")}}
	finder := realphoto.NewFinder(&fakeText{plan: personPlan, official: `{"index":0,"maker":"Sierra"}`}, vision, &http.Client{Transport: routes}, nil)
	request := realphoto.Request{Title: "A keynote", SourceURL: "https://publisher.test/story", Slot: realphoto.SlotFeatured}
	if result := finder.Find(context.Background(), request); result.Found || vision.calls != 0 {
		t.Fatalf("featured found = %t, vision calls = %d, want no official fallback", result.Found, vision.calls)
	}
	request.Slot = realphoto.SlotInline
	if result := finder.Find(context.Background(), request); !result.Found || result.Credit.Kind != realphoto.KindOfficial {
		t.Fatalf("inline result = %+v, want the official fallback", result)
	}
}

func TestOfficialPageRejectsImagesOnNewsHostsBeforeDownloading(t *testing.T) {
	var queries, downloads []string
	photo := serveBytes(solidPNG(t, 1600, 900, green), "image/png")
	getImage := func(w http.ResponseWriter, r *http.Request) {
		downloads = append(downloads, r.URL.String())
		photo(w, r)
	}
	routes := sites{
		"commons.wikimedia.org": commonsAPI(t, nil, &queries),
		"publisher.test":        serveBytes([]byte(`<title>News source</title>`), "text/html"),
		"cdn.publisher.test":    getImage,
		"cdn.theverge.com":      getImage,
		"wici.ai": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/wici-one" {
				serveBytes([]byte(`<img src="https://cdn.theverge.com/blocked.png"><img src="https://cdn.publisher.test/source.png"><img src="/official.png">`), "text/html")(w, r)
				return
			}
			getImage(w, r)
		},
	}
	vision := &fakeVision{verdicts: map[string]string{"green": verdict(9, "The WiCi One.")}}
	finder := realphoto.NewFinder(&fakeText{plan: officialRobotPlan}, vision, &http.Client{Transport: routes}, nil)
	result := finder.Find(context.Background(), realphoto.Request{Title: "WiCi One", SourceURL: "https://publisher.test/article"})
	if !slices.Equal(downloads, []string{"https://wici.ai/official.png"}) || vision.calls != 1 {
		t.Fatalf("downloads = %v, vision calls = %d", downloads, vision.calls)
	}
	if !result.Found || len(result.Candidates) != 3 || result.Candidates[0].Reason == "" || result.Candidates[1].Reason == "" {
		t.Fatalf("got %+v, want both news images rejected and the official image chosen", result)
	}
}
