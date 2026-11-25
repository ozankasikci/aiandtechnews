package realphoto_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

func fixedPhotoTime() time.Time {
	return time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
}

func TestCommonsCaptureYear(t *testing.T) {
	for _, tc := range []struct {
		name, date string
		year       int
	}{
		{"plain date", "2022-04-10 09:30:00", 2022},
		{"HTML date", `<span title="1901"><b>10 April 2023</b></span>`, 2023},
		{"HTML with quoted bracket", `<span title="> 1901">2023</span>`, 2023},
		{"year only", "2024", 2024},
		{"unreadable", "circa the nineties", 0},
		{"absent", "", 0},
		{"first plausible year", "ID 1232025, 1899, 2027, c. 1900 or 2020", 1900},
		{"current year", "2026", 2026},
	} {
		t.Run(tc.name, func(t *testing.T) {
			results, _, _ := findCommonsAtFixedTime(t, robotPlan, realphoto.SlotInline, tc.date)
			if len(results.Candidates) != 1 {
				t.Fatalf("got %d candidates, want one", len(results.Candidates))
			}
			if got := results.Candidates[0].CaptureYear; got != tc.year {
				t.Fatalf("capture year = %d, want %d", got, tc.year)
			}
		})
	}
}

func TestCommonsAgeGateRunsBeforeDownloadAndVision(t *testing.T) {
	const personPlan = `{"photographable":true,"subject":"Bret Taylor","kind":"person"}`
	for _, mode := range []struct {
		name, plan, slot string
	}{
		{"person inline", personPlan, realphoto.SlotInline},
		{"person featured", personPlan, realphoto.SlotFeatured},
		{"person stand-in", fundingPlan, realphoto.SlotFeatured},
	} {
		t.Run(mode.name, func(t *testing.T) {
			for _, tc := range []struct {
				name, date, reason string
				want               bool
			}{
				{"old", "2020", "more than 5 years", false},
				{"unknown", "", "capture year unknown", false},
				{"recent", "2021", "", true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					assertCommonsAgeOutcome(t, mode.plan, mode.slot, tc.date, tc.reason, tc.want)
				})
			}
		})
	}
	for _, tc := range []struct {
		name, slot, date, reason string
		want                     bool
	}{
		{"old featured", realphoto.SlotFeatured, "2015", "more than 10 years", false},
		{"boundary featured", realphoto.SlotFeatured, "2016", "", true},
		{"old inline", realphoto.SlotInline, "2015", "", true},
		{"unknown featured", realphoto.SlotFeatured, "", "", true},
		{"unknown inline", realphoto.SlotInline, "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertCommonsAgeOutcome(t, robotPlan, tc.slot, tc.date, tc.reason, tc.want)
		})
	}
}

func assertCommonsAgeOutcome(t *testing.T, plan, slot, date, reason string, want bool) {
	t.Helper()
	result, downloads, checks := findCommonsAtFixedTime(t, plan, slot, date)
	wantCalls := 0
	if want {
		wantCalls = 1
	}
	if result.Found != want || downloads != wantCalls || checks != wantCalls || result.Transient {
		t.Fatalf("found %t, transient %t, downloads %d and checks %d, want found %t with %d calls",
			result.Found, result.Transient, downloads, checks, want, wantCalls)
	}
	if !want && (len(result.Candidates) != 1 || !strings.Contains(result.Candidates[0].Reason, reason)) {
		t.Fatalf("candidates = %+v, want reason containing %q", result.Candidates, reason)
	}
}

func findCommonsAtFixedTime(t *testing.T, plan, slot, date string) (realphoto.Result, int, int) {
	t.Helper()
	var queries []string
	api := commonsAPI(t, []commonsFile{{
		title: "File:Bret_Taylor.jpg", date: date,
		license: "CC0", width: 1600, height: 900, thumb: "https://upload.wikimedia.org/photo.png",
	}}, &queries)
	downloads := 0
	routes := sites{
		"commons.wikimedia.org": func(w http.ResponseWriter, r *http.Request) {
			fields := strings.Split(r.URL.Query().Get("iiextmetadatafilter"), "|")
			if !slices.Contains(fields, "DateTimeOriginal") {
				t.Error("metadata filter lacks DateTimeOriginal")
			}
			api(w, r)
		},
		"upload.wikimedia.org": func(w http.ResponseWriter, r *http.Request) {
			downloads++
			serveBytes(solidPNG(t, 1600, 900, green), "image/png")(w, r)
		},
	}
	vision := &fakeVision{verdicts: map[string]string{"green": verdict(9, "")}}
	finder := realphoto.NewFinder(&fakeText{plan: plan}, vision, &http.Client{Transport: routes}, nil).WithClock(fixedPhotoTime)
	result := finder.Find(context.Background(), realphoto.Request{Slot: slot})
	return result, downloads, vision.calls
}
