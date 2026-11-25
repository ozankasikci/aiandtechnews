package realphoto_test

import (
	"context"
	"image/color"
	"net/http"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

func twoFiles(t *testing.T, queries *[]string) sites {
	return sites{
		"commons.wikimedia.org": commonsAPI(t, []commonsFile{
			{title: "File:Good.jpg", license: "CC BY-SA 4.0", artist: "Jane Doe", width: 3200, height: 1800, thumb: "https://upload.wikimedia.org/good.png"},
			{title: "File:Better.jpg", license: "CC BY 2.0", artist: "John Roe", width: 4000, height: 2250, thumb: "https://upload.wikimedia.org/better.png"},
		}, queries),
		"upload.wikimedia.org": func(w http.ResponseWriter, r *http.Request) {
			fill := map[string]color.RGBA{"/good.png": green, "/better.png": blue}[r.URL.Path]
			serveBytes(solidPNG(t, 1600, 900, fill), "image/png")(w, r)
		},
	}
}

func TestFindSkipsExcludedPhotos(t *testing.T) {
	var queries []string
	vision := &fakeVision{verdicts: map[string]string{"green": verdict(8, "Good."), "blue": verdict(9, "Better.")}}
	finder := realphoto.NewFinder(&fakeText{plan: robotPlan}, vision, &http.Client{Transport: twoFiles(t, &queries)}, nil)

	result := finder.Find(context.Background(), realphoto.Request{
		Slug: "wici", Title: "WiCi One is a desk robot", Text: "...",
		Exclude: []string{"https://upload.wikimedia.org/better.png"},
	})
	if !result.Found || result.Chosen.PageURL != "https://commons.wikimedia.org/wiki/File:Good.jpg" {
		t.Fatalf("chosen = %+v", result.Chosen)
	}
	if vision.calls != 1 {
		t.Fatalf("vision calls = %d, want 1: an excluded photo is not checked", vision.calls)
	}
	for _, candidate := range result.Candidates {
		if candidate.PageURL != "https://commons.wikimedia.org/wiki/File:Better.jpg" {
			continue
		}
		if !strings.Contains(candidate.Reason, "already used") {
			t.Fatalf("excluded candidate reason = %q", candidate.Reason)
		}
		return
	}
	t.Fatal("excluded candidate missing from result")
}

func TestFindStopsSearchingCommonsWhenRateLimited(t *testing.T) {
	requests := 0
	routes := sites{"commons.wikimedia.org": func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}}
	finder := realphoto.NewFinder(&fakeText{plan: robotPlan}, &fakeVision{}, &http.Client{Transport: routes}, nil)

	result := finder.Find(context.Background(), realphoto.Request{Slug: "wici", Title: "WiCi One is a desk robot", Text: "..."})
	if result.Found {
		t.Fatal("a rate-limited search must not find a photo")
	}
	if requests != 1 {
		t.Fatalf("Commons requests = %d, want 1: the second query must not be sent", requests)
	}
	if !strings.Contains(strings.Join(result.Notes, " "), "rate limited") {
		t.Fatalf("notes = %v", result.Notes)
	}
}
