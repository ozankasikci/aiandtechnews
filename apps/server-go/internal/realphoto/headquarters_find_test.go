package realphoto_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

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
			{title: "File:Pioneer Building, San Francisco.png", description: "OpenAI's former office", date: "2019", license: "CC0", width: 1600, height: 900, thumb: formerURL},
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
