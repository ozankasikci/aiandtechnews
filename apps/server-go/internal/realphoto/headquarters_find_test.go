package realphoto_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

func TestFindSkipsAFormerOfficeForAHeadquartersStandIn(t *testing.T) {
	const subject = "OpenAI headquarters, 1515 Third Street, San Francisco"
	const plan = `{"photographable":false,"story_type":"funding","maker":"OpenAI",
		"stand_ins":[{"subject":"` + subject + `","kind":"place","queries":["1515 Third Street San Francisco"]}]}`
	const formerURL = "https://upload.wikimedia.org/pioneer.png"
	const currentURL = "https://upload.wikimedia.org/1515-third-street.png"
	former := solidPNG(t, 1600, 900, green)
	current := solidPNG(t, 1600, 900, blue)
	var queries []string
	routes := sites{
		"commons.wikimedia.org": commonsAPI(t, []commonsFile{
			{title: "File:Pioneer Building, San Francisco.png", description: "OpenAI's former office", date: "2025", license: "CC0", width: 1600, height: 900, thumb: formerURL},
			{title: "File:1515 Third Street, San Francisco.png", description: "OpenAI headquarters", date: "2025", license: "CC0", width: 1600, height: 900, thumb: currentURL},
		}, &queries),
		"upload.wikimedia.org": func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.String() {
			case formerURL:
				serveBytes(former, "image/png")(w, r)
			case currentURL:
				serveBytes(current, "image/png")(w, r)
			default:
				http.NotFound(w, r)
			}
		},
	}
	vision := &fakeVision{verdicts: map[string]string{
		"green": `{"match":"exact","photographic":true,"subject_prominent":true,"other_site":true,"quality":10}`,
		"blue":  `{"match":"exact","photographic":true,"subject_prominent":true,"other_site":false,"quality":9}`,
	}}
	finder := realphoto.NewFinder(&fakeText{plan: plan}, vision, &http.Client{Transport: routes}, nil).WithClock(fixedPhotoTime)
	result := finder.Find(context.Background(), realphoto.Request{Title: "OpenAI raises funding", Slot: realphoto.SlotFeatured})

	if vision.calls != 2 || len(result.Candidates) != 2 {
		t.Fatalf("vision calls = %d, candidates = %d, want two reviewed photos", vision.calls, len(result.Candidates))
	}
	first := result.Candidates[0]
	if first.ImageURL != formerURL || first.Usable || !strings.Contains(first.Reason, "different site") {
		t.Errorf("first candidate = %q, usable = %t, reason = %q, want the former office rejected as a different site", first.ImageURL, first.Usable, first.Reason)
	}
	const wantSubject = "OpenAI headquarters, 1515 Third Street, Mission Bay, San Francisco"
	if !result.Found || result.StandIn == nil || result.StandIn.Subject != wantSubject || result.Chosen == nil ||
		result.Chosen.ImageURL != currentURL || !bytes.Equal(result.Image, current) {
		t.Errorf("found = %t, stand-in = %v, reason = %q, want the current headquarters photo", result.Found, result.StandIn, result.Reason)
	}
}

func TestFindChecksHeadquartersCaptureYearBeforeDownloadAndVision(t *testing.T) {
	const anthropic = "Anthropic headquarters"
	const tooEarly = "captured in 2018, before the current tenant moved in (2024)"
	for _, tc := range []struct {
		name, subject, kind, date string
		nowYear                   int
		plannerFields, rejection  string
	}{
		{"before move-in", anthropic, "place", "2018", 2026, "", tooEarly},
		{"unknown capture year", anthropic, "place", "", 2026, "", "capture year unknown. The site changed tenant in 2024"},
		{"move-in year", anthropic, "place", "2024", 2026, "", ""},
		{"after move-in", anthropic, "place", "2025", 2026, "", ""},
		{"existing featured age limit", anthropic, "place", "2025", 2036, "", "more than 10 years old"},
		{"listed without move-in year", "Stripe headquarters", "place", "2018", 2026, "", ""},
		{"unlisted place", "Sierra headquarters", "place", "2018", 2026, "", ""},
		// The person rule already rejects 2018 photos against a 2026 clock.
		{"person within existing age limit", "Bret Taylor", "person", "2018", 2023, "", ""},
		{"planner cannot set move-in year", "Sierra headquarters", "place", "2018", 2026, `,"since":2024,"since_year":2024,"headquartersSince":2024`, ""},
		{"planner cannot override list year", anthropic, "place", "2018", 2026, `,"since":2000,"since_year":2000,"headquartersSince":2000`, tooEarly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := fmt.Sprintf(`{"photographable":false,"story_type":"funding","stand_ins":[{"subject":%q,"kind":%q%s}]}`,
				tc.subject, tc.kind, tc.plannerFields)
			var queries []string
			downloads := 0
			routes := sites{
				"commons.wikimedia.org": commonsAPI(t, []commonsFile{{
					title: "File:" + tc.subject + ".png", date: tc.date,
					license: "CC0", width: 1600, height: 900, thumb: "https://upload.wikimedia.org/photo.png",
				}}, &queries),
				"upload.wikimedia.org": func(w http.ResponseWriter, r *http.Request) {
					downloads++
					serveBytes(solidPNG(t, 1600, 900, green), "image/png")(w, r)
				},
			}
			vision := &fakeVision{verdicts: map[string]string{"green": verdict(9, "")}}
			clock := func() time.Time { return time.Date(tc.nowYear, time.November, 25, 0, 0, 0, 0, time.UTC) }
			finder := realphoto.NewFinder(&fakeText{plan: plan}, vision, &http.Client{Transport: routes}, nil).WithClock(clock)
			result := finder.Find(context.Background(), realphoto.Request{Slot: realphoto.SlotFeatured})

			if len(result.Candidates) != 1 {
				t.Fatalf("got %d candidates, want one", len(result.Candidates))
			}
			candidate := result.Candidates[0]
			if tc.rejection != "" {
				if result.Found || candidate.Usable || downloads != 0 || vision.calls != 0 || !strings.Contains(candidate.Reason, tc.rejection) {
					t.Errorf("found %t, usable %t, downloads %d, vision calls %d, reason %q, want rejection containing %q before download and vision",
						result.Found, candidate.Usable, downloads, vision.calls, candidate.Reason, tc.rejection)
				}
				return
			}
			if !result.Found || result.StandIn == nil || !candidate.Usable || downloads != 1 || vision.calls != 1 {
				t.Errorf("found %t, stand-in %v, usable %t, downloads %d, vision calls %d, reason %q, want one downloaded and reviewed stand-in photo",
					result.Found, result.StandIn, candidate.Usable, downloads, vision.calls, candidate.Reason)
			}
		})
	}
}
