package realphoto_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

func TestFindChecksQualityAndPromptForTheRequestedSlot(t *testing.T) {
	var queries []string
	routes := sites{
		"commons.wikimedia.org": commonsAPI(t, []commonsFile{
			{title: "File:Robot.jpg", license: "CC0", width: 1600, height: 900, thumb: "https://upload.wikimedia.org/robot.png"},
		}, &queries),
		"upload.wikimedia.org": serveBytes(solidPNG(t, 1600, 900, green), "image/png"),
	}
	vision := &fakeVision{verdicts: map[string]string{"green": verdict(7, "A robot.")}}
	finder := realphoto.NewFinder(&fakeText{plan: robotPlan}, vision, &http.Client{Transport: routes}, nil)
	request := realphoto.Request{Title: "WiCi One is a desk robot", Text: "..."}

	inline := finder.Find(context.Background(), request)
	if !inline.Found || inline.Chosen.Verdict.Quality != 7 {
		t.Fatalf("inline = %+v, want the quality-7 photo", inline)
	}
	request.Slot = realphoto.SlotFeatured
	featured := finder.Find(context.Background(), request)
	if featured.Found {
		t.Fatal("featured slot accepted the quality-7 photo")
	}
	want := fmt.Sprintf("quality 7 below %d for the featured slot", realphoto.MinFeaturedQuality)
	if len(featured.Candidates) != 1 || featured.Candidates[0].Reason != want {
		t.Fatalf("featured candidates = %+v, want reason %q", featured.Candidates, want)
	}
	if len(vision.prompts) != 2 || !strings.Contains(vision.prompts[1], "lead picture") {
		t.Fatalf("vision prompts = %q, want the second to name the lead picture", vision.prompts)
	}
}

func TestFindStopsDownloadingCommonsImagesWhenRateLimited(t *testing.T) {
	var queries []string
	requests := 0
	routes := twoFiles(t, &queries)
	routes["upload.wikimedia.org"] = func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}
	finder := realphoto.NewFinder(&fakeText{plan: robotPlan}, &fakeVision{}, &http.Client{Transport: routes}, nil)
	result := finder.Find(context.Background(), realphoto.Request{Title: "WiCi One", Text: "..."})

	if requests != 1 {
		t.Fatalf("upload.wikimedia.org requests = %d, want 1", requests)
	}
	assertRateLimitedCandidates(t, result)
}

const officialRobotPlan = `{"photographable":true,"subject":"WiCi One desk robot","kind":"robot","queries":["WiCi One robot"],"maker":"WiCi","maker_domain":"wici.ai","official_pages":["https://wici.ai/wici-one"]}`

func TestFindStopsDownloadingOfficialImagesWhenRateLimited(t *testing.T) {
	var queries, downloads []string
	routes := sites{
		"commons.wikimedia.org": commonsAPI(t, nil, &queries),
		"wici.ai": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/wici-one" {
				serveBytes([]byte(`<img src="/first.png"><img src="/second.png">`), "text/html")(w, r)
				return
			}
			downloads = append(downloads, r.URL.Path)
			http.Error(w, "slow down", http.StatusTooManyRequests)
		},
	}
	finder := realphoto.NewFinder(&fakeText{plan: officialRobotPlan}, &fakeVision{}, &http.Client{Transport: routes}, nil)
	result := finder.Find(context.Background(), realphoto.Request{Title: "WiCi One", Text: "..."})

	if !slices.Equal(downloads, []string{"/first.png"}) {
		t.Fatalf("official downloads = %v, want only /first.png", downloads)
	}
	assertRateLimitedCandidates(t, result)
}

func assertRateLimitedCandidates(t *testing.T, result realphoto.Result) {
	t.Helper()
	if result.Found || len(result.Candidates) != 2 {
		t.Fatalf("result = %+v, want two rejected candidates", result)
	}
	if !strings.Contains(result.Candidates[0].Reason, "HTTP 429") || result.Candidates[1].Reason != "not checked (rate limited)" {
		t.Fatalf("candidate reasons = %q and %q", result.Candidates[0].Reason, result.Candidates[1].Reason)
	}
}

func TestFindSkipsExcludedOfficialImagesBeforeDownloading(t *testing.T) {
	var queries, downloads []string
	photo := serveBytes(solidPNG(t, 1600, 900, green), "image/png")
	const excluded = "https://wici.ai/excluded.png"
	routes := sites{
		"commons.wikimedia.org": commonsAPI(t, nil, &queries),
		"wici.ai": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/wici-one" {
				serveBytes([]byte(`<img src="/excluded.png"><img src="/fresh.png">`), "text/html")(w, r)
				return
			}
			downloads = append(downloads, r.URL.Path)
			photo(w, r)
		},
	}
	vision := &fakeVision{verdicts: map[string]string{"green": verdict(8, "A robot.")}}
	finder := realphoto.NewFinder(&fakeText{plan: officialRobotPlan}, vision, &http.Client{Transport: routes}, nil)
	result := finder.Find(context.Background(), realphoto.Request{Title: "WiCi One", Text: "...", Exclude: []string{excluded}})

	if !slices.Equal(downloads, []string{"/fresh.png"}) {
		t.Fatalf("official downloads = %v, want only /fresh.png", downloads)
	}
	if !result.Found || result.Credit.Kind != realphoto.KindOfficial || result.Chosen.ImageURL != "https://wici.ai/fresh.png" {
		t.Fatalf("result = %+v, want the fresh official image", result)
	}
	if len(result.Candidates) != 2 || result.Candidates[0].ImageURL != excluded || result.Candidates[0].Reason != "already used by a recent featured image" {
		t.Fatalf("official candidates = %+v, want the excluded candidate and its reason", result.Candidates)
	}
}
