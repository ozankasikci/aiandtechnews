package realphoto_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

func TestFindChecksCommonsAspectForTheRequestedSlot(t *testing.T) {
	const photoURL = "https://upload.wikimedia.org/robot.png"
	slotCases := []struct {
		slot         string
		minimumLabel string
	}{
		{realphoto.SlotFeatured, "1.33"},
		{realphoto.SlotInline, "1.50"},
	}
	shapes := []struct {
		name                 string
		width, height        int
		featuredOK, inlineOK bool
	}{
		{"4:3", 1600, 1200, true, false},
		{"3:2", 1800, 1200, true, true},
		{"16:9", 1600, 900, true, true},
		{"5:4", 1500, 1200, false, false},
		{"square", 1200, 1200, false, false},
		{"portrait", 1200, 1600, false, false},
		{"one pixel below 3:2", 6000, 4001, true, true},
		{"one pixel below 4:3", 4032, 3025, true, false},
		{"featured lower tolerance edge", 3970, 3000, true, false},
		{"inline lower tolerance edge", 1490, 1000, true, true},
		{"upper tolerance edge", 2110, 1000, true, true},
		{"above upper tolerance", 2111, 1000, false, false},
	}
	thumbnail := serveBytes(solidPNG(t, 1600, 1200, green), "image/png")
	for _, slotCase := range slotCases {
		for _, shape := range shapes {
			t.Run(slotCase.slot+"/"+shape.name, func(t *testing.T) {
				want := shape.inlineOK
				if slotCase.slot == realphoto.SlotFeatured {
					want = shape.featuredOK
				}
				var queries []string
				file := commonsFile{title: "File:Robot.png", license: "CC0", width: shape.width, height: shape.height, thumb: photoURL}
				downloads := 0
				routes := sites{
					"commons.wikimedia.org": commonsAPI(t, []commonsFile{file}, &queries),
					"upload.wikimedia.org": func(w http.ResponseWriter, r *http.Request) {
						downloads++
						thumbnail(w, r)
					},
				}
				vision := &fakeVision{verdicts: map[string]string{"green": verdict(9, "The robot.")}}
				finder := realphoto.NewFinder(&fakeText{plan: robotPlan}, vision, &http.Client{Transport: routes}, nil)
				result := finder.Find(context.Background(), realphoto.Request{Slot: slotCase.slot})
				wantCalls := 0
				if want {
					wantCalls = 1
				}
				if result.Found != want || downloads != wantCalls || vision.calls != wantCalls {
					t.Fatalf("found = %t, downloads = %d, vision calls = %d, want found = %t with %d calls", result.Found, downloads, vision.calls, want, wantCalls)
				}
				if !want {
					wantLimits := "outside " + slotCase.minimumLabel + "-2.10"
					if len(result.Candidates) != 1 || !strings.Contains(result.Candidates[0].Reason, wantLimits) {
						t.Fatalf("candidates = %+v, want rejection with %q", result.Candidates, wantLimits)
					}
				}
			})
		}
	}
}

func TestFindChecksOfficialAspectForTheRequestedSlot(t *testing.T) {
	const page = `<img src="/four-thirds.png"><img src="/five-fourths.png"><img src="/square.png"><img src="/portrait.png"><img src="/three-halves.png">`
	images := map[string][]byte{
		"/four-thirds.png":  solidPNG(t, 1600, 1200, green),
		"/five-fourths.png": solidPNG(t, 1500, 1200, red),
		"/square.png":       solidPNG(t, 1200, 1200, red),
		"/portrait.png":     solidPNG(t, 1200, 1600, red),
		"/three-halves.png": solidPNG(t, 1800, 1200, blue),
	}
	for _, tc := range []struct {
		slot, chosen string
		visionCalls  int
	}{
		{realphoto.SlotFeatured, "/four-thirds.png", 2},
		{realphoto.SlotInline, "/three-halves.png", 1},
	} {
		t.Run(tc.slot, func(t *testing.T) {
			var queries, downloads []string
			routes := sites{
				"commons.wikimedia.org": commonsAPI(t, nil, &queries),
				"wici.ai": func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/wici-one" {
						serveBytes([]byte(page), "text/html")(w, r)
						return
					}
					downloads = append(downloads, r.URL.Path)
					serveBytes(images[r.URL.Path], "image/png")(w, r)
				},
			}
			vision := &fakeVision{verdicts: map[string]string{"green": verdict(9, "The robot."), "blue": verdict(8, "The robot.")}}
			finder := realphoto.NewFinder(&fakeText{plan: officialRobotPlan}, vision, &http.Client{Transport: routes}, nil)
			result := finder.Find(context.Background(), realphoto.Request{Slot: tc.slot})
			if !result.Found {
				t.Fatalf("no official image chosen: %s", result.Reason)
			}
			if result.Chosen.ImageURL != "https://wici.ai"+tc.chosen || vision.calls != tc.visionCalls {
				t.Fatalf("chosen = %s, vision calls = %d, want %s with %d calls", result.Chosen.ImageURL, vision.calls, tc.chosen, tc.visionCalls)
			}
			// Official pages can provide URLs alone, so the finder measures the downloaded files.
			wantDownloads := []string{"/four-thirds.png", "/five-fourths.png", "/square.png", "/portrait.png", "/three-halves.png"}
			if !slices.Equal(downloads, wantDownloads) || len(result.Candidates) != len(wantDownloads) {
				t.Fatalf("downloads = %v, candidates = %d, want %v", downloads, len(result.Candidates), wantDownloads)
			}
			for _, candidate := range result.Candidates[1:4] {
				if candidate.Verdict != nil || !strings.HasPrefix(candidate.Reason, "aspect ") {
					t.Errorf("candidate = %+v, want aspect rejection before vision", candidate)
				}
			}
		})
	}
}
