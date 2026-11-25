# Real Photos for Featured Images Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The publisher looks for a real, credited photo for an article's featured image first, and draws an illustration only when no photo passes the checks.

**Architecture:** A new `photo` step in the featured-image chain calls the existing `realphoto.Finder` with a featured slot. The Finder learns stand-in subjects, stricter featured checks, a crop focus point, an exclusion list and rate-limit handling. The chosen photo's credit travels from the pipeline through the publisher into a new table, out through the article API, and onto the article page.

**Tech Stack:** Go 1.x (`apps/server-go`, SQLite, Gemini text and vision), Next.js 15 and TypeScript (`apps/web`, tests with `tsx --test`).

**Spec:** `docs/superpowers/specs/2026-11-24-real-photo-featured-images-design.md`

---

## Before you start

- Work on a branch in a git worktree of this repository, not on `main`.
- Go commands run from `apps/server-go`. Web commands run from `apps/web`.
- Go tests: `go test -p 2 ./internal/<package>/...`. Web tests: `pnpm test`.
- Commit messages are one plain sentence saying what changed. No `feat:` style prefix and no co-author line.
- Nothing changes in production until `FEATURED_IMAGE_CHAIN` names `photo`. That is the owner's step, after Task 13.

**One deliberate difference from the spec.** The spec says repeat photos are compared by credit URL. This plan compares them by the photo's own URL (`photo_url`), stored in the new table. An official image's credit URL is the maker's page, which every image on that page shares, so credit URLs would block too much.

## File map

| File | Responsibility | Task |
|---|---|---|
| `apps/server-go/internal/imaging/crop.go` (new) | Crop to an aspect ratio around a focus point | 1 |
| `apps/server-go/internal/realphoto/verify.go` | Verdict fields `dated` and focus; featured-slot rules | 2 |
| `apps/server-go/internal/realphoto/realphoto.go`, `fetch.go`, `commons.go` | Request slot and exclusions; rate-limit handling | 3 |
| `apps/server-go/internal/realphoto/plan.go`, `realphoto.go` | Stand-in subjects | 4 |
| `apps/server-go/internal/content/migrations/017_article_featured_image_credits.sql` (new), `content/migrations.go` | Credit table | 5 |
| `apps/server-go/internal/publisher/publisher.go`, `articles.go` | Credit type; store it with the article; recent photo URLs | 5, 6 |
| `apps/server-go/internal/illustration/photo.go` (new), `pipeline.go`, `providers.go` | The `photo` chain step | 7 |
| `apps/server-go/internal/config/config.go`, `illustration/wiring.go`, `app/app.go` | Accept and wire `photo` | 8 |
| `apps/server-go/internal/content/model.go`, `sqlite.go`, `contracts/openapi.yaml` | Return the credit from the article API | 9 |
| `apps/server-go/internal/inlineimage/store.go`, `worker.go` | In-article image never repeats the featured photo | 10 |
| `apps/web/app/data/articles.ts`, `lib/inline-images.ts`, `lib/api.ts`, `article/[slug]/page.tsx` | Show the credit | 11 |
| `apps/server-go/cmd/photo-try/main.go`, `report.go` (new) | Dry-run report | 12 |
| `NEWS_PUBLISHING_POLICY.md`, `apps/server-go/docs/technews.env.example` | Policy and configuration docs | 13 |

---

### Task 1: Crop around a focus point

**Files:**
- Create: `apps/server-go/internal/imaging/crop.go`
- Test: `apps/server-go/internal/imaging/crop_test.go`

- [ ] **Step 1: Write the failing tests**

Create `apps/server-go/internal/imaging/crop_test.go`:

```go
package imaging_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
)

func TestCropRectKeepsTheFocusInFrame(t *testing.T) {
	ratio := 16.0 / 9.0
	for _, tc := range []struct {
		name   string
		w, h   int
		fx, fy float64
	}{
		{"portrait, focus near the top", 1200, 1600, 0.5, 0.05},
		{"portrait, focus near the bottom", 1200, 1600, 0.5, 0.98},
		{"panorama, focus at the left edge", 3000, 1000, 0.02, 0.5},
		{"panorama, focus at the right edge", 3000, 1000, 0.99, 0.5},
		{"centre", 1600, 1200, 0.5, 0.5},
		{"out of range", 1600, 1200, -1, 2},
	} {
		rect := imaging.CropRect(tc.w, tc.h, ratio, tc.fx, tc.fy)
		if rect.Min.X < 0 || rect.Min.Y < 0 || rect.Max.X > tc.w || rect.Max.Y > tc.h {
			t.Errorf("%s: %v leaves the %dx%d image", tc.name, rect, tc.w, tc.h)
		}
		if got := float64(rect.Dx()) / float64(rect.Dy()); math.Abs(got-ratio) > 0.01 {
			t.Errorf("%s: aspect %.3f, want %.3f", tc.name, got, ratio)
		}
		fx, fy := math.Min(math.Max(tc.fx, 0), 1), math.Min(math.Max(tc.fy, 0), 1)
		focus := image.Pt(min(int(fx*float64(tc.w)), tc.w-1), min(int(fy*float64(tc.h)), tc.h-1))
		if !focus.In(rect) {
			t.Errorf("%s: focus %v is outside %v", tc.name, focus, rect)
		}
	}
}

// halves is a square image whose top half is red and bottom half is blue.
func halves(t *testing.T, size int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		fill := color.RGBA{R: 220, A: 255}
		if y >= size/2 {
			fill = color.RGBA{B: 220, A: 255}
		}
		for x := 0; x < size; x++ {
			img.Set(x, y, fill)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestCropToAspectCropsAroundTheFocus(t *testing.T) {
	for name, tc := range map[string]struct {
		focusY   float64
		wantBlue bool
	}{"top": {0.1, false}, "bottom": {0.9, true}} {
		cropped, err := imaging.CropToAspect(halves(t, 400), 16.0/9.0, 0.5, tc.focusY)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(cropped))
		if err != nil {
			t.Fatal(err)
		}
		if b := img.Bounds(); b.Dx() != 400 || b.Dy() != 225 {
			t.Fatalf("%s: size %dx%d, want 400x225", name, b.Dx(), b.Dy())
		}
		_, _, blue, _ := img.At(200, 112).RGBA()
		if (blue > 30000) != tc.wantBlue {
			t.Errorf("%s: centre pixel blue=%d, want blue %t", name, blue, tc.wantBlue)
		}
	}
}

func TestCropToAspectLeavesAMatchingImageAlone(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1600, 900))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	out, err := imaging.CropToAspect(buf.Bytes(), 16.0/9.0, 0.2, 0.8)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, buf.Bytes()) {
		t.Fatal("a 16:9 image must be returned unchanged")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -p 2 ./internal/imaging/...`
Expected: FAIL to compile with `undefined: imaging.CropRect` and `undefined: imaging.CropToAspect`.

- [ ] **Step 3: Write the implementation**

Create `apps/server-go/internal/imaging/crop.go`:

```go
package imaging

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
)

// CropRect is the largest rectangle of the given aspect ratio (width over
// height) inside a width by height image, placed so that the focus point is
// as close to its centre as the image's edges allow. The focus is given as
// fractions of the width and height; values outside 0..1 are clamped.
func CropRect(width, height int, ratio, focusX, focusY float64) image.Rectangle {
	if width <= 0 || height <= 0 || ratio <= 0 {
		return image.Rect(0, 0, max(width, 0), max(height, 0))
	}
	cropW, cropH := width, int(math.Round(float64(width)/ratio))
	if cropH > height {
		cropH = height
		cropW = int(math.Round(float64(height) * ratio))
	}
	cropW, cropH = max(1, min(cropW, width)), max(1, min(cropH, height))
	fraction := func(value float64) float64 { return math.Min(math.Max(value, 0), 1) }
	left := int(math.Round(fraction(focusX)*float64(width))) - cropW/2
	top := int(math.Round(fraction(focusY)*float64(height))) - cropH/2
	left, top = min(max(left, 0), width-cropW), min(max(top, 0), height-cropH)
	return image.Rect(left, top, left+cropW, top+cropH)
}

// CropToAspect crops an image to the aspect ratio around the focus point and
// returns it as PNG. An image that already has the ratio is returned as it
// came, byte for byte.
func CropToAspect(data []byte, ratio, focusX, focusY float64) ([]byte, error) {
	src, err := decode(data)
	if err != nil {
		return nil, err
	}
	bounds := src.Bounds()
	rect := CropRect(bounds.Dx(), bounds.Dy(), ratio, focusX, focusY)
	if rect.Dx() == bounds.Dx() && rect.Dy() == bounds.Dy() {
		return data, nil
	}
	canvas := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(canvas, canvas.Bounds(), src, bounds.Min.Add(rect.Min), draw.Src)
	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		return nil, fmt.Errorf("encode cropped image: %w", err)
	}
	return out.Bytes(), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -p 2 ./internal/imaging/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/server-go/internal/imaging/crop.go apps/server-go/internal/imaging/crop_test.go
git commit -m "Crop an image to an aspect ratio around a focus point"
```

---

### Task 2: Verdict fields and featured-slot rules

**Files:**
- Modify: `apps/server-go/internal/realphoto/verify.go`
- Modify: `apps/server-go/internal/realphoto/realphoto.go` (the `Request` struct and one line of `verifyAll`)
- Test: `apps/server-go/internal/realphoto/verify_slot_internal_test.go` (new), `apps/server-go/internal/realphoto/verify_slot_test.go` (new)

- [ ] **Step 1: Write the failing tests**

Create `apps/server-go/internal/realphoto/verify_slot_internal_test.go`:

```go
package realphoto

import "testing"

func TestUsableForAppliesTheFeaturedRules(t *testing.T) {
	good := Verdict{Match: MatchExact, Photographic: true, Prominent: true, Quality: MinFeaturedQuality}
	if ok, why := good.usableFor(SlotFeatured, ""); !ok {
		t.Fatalf("good featured verdict rejected: %s", why)
	}
	seven := good
	seven.Quality = MinFeaturedQuality - 1
	if ok, _ := seven.usableFor(SlotFeatured, ""); ok {
		t.Fatal("quality 7 must be rejected for the featured slot")
	}
	if ok, why := seven.usableFor(SlotInline, ""); !ok {
		t.Fatalf("quality 7 must still pass in the body: %s", why)
	}
	dated := good
	dated.Dated = true
	if ok, _ := dated.usableFor(SlotFeatured, ""); ok {
		t.Fatal("a dated photo must be rejected for the featured slot")
	}
	if ok, why := dated.usableFor(SlotInline, ""); !ok {
		t.Fatalf("the body ignores dated: %s", why)
	}
	branded := good
	branded.OtherBranding = true
	if ok, why := branded.usableFor(SlotFeatured, KindPerson); !ok {
		t.Fatalf("a person in front of an event backdrop must pass: %s", why)
	}
	if ok, _ := branded.usableFor(SlotFeatured, "place"); ok {
		t.Fatal("other branding is only allowed for a person")
	}
	if ok, _ := branded.usableFor(SlotInline, KindPerson); ok {
		t.Fatal("the body keeps rejecting other branding")
	}
}
```

Create `apps/server-go/internal/realphoto/verify_slot_test.go`:

```go
package realphoto_test

import (
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

func TestParseVerdictReadsDatedAndFocus(t *testing.T) {
	raw := `{"match":"exact","photographic":true,"watermark":false,"text_heavy":false,"marketing_banner":false,"misleading":false,"subject_prominent":true,"other_branding":false,"dated":true,"focus_x":0.3,"focus_y":1.7,"quality":9,"alt":"A.","notes":"n"}`
	got, err := realphoto.ParseVerdict(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Dated || got.FocusX != 0.3 || got.FocusY != 1 {
		t.Fatalf("verdict = %+v, want dated, focus 0.3 and 1 (clamped)", got)
	}
}

func TestParseVerdictDefaultsTheFocusToTheCentre(t *testing.T) {
	got, err := realphoto.ParseVerdict(verdict(8, "A robot."))
	if err != nil {
		t.Fatal(err)
	}
	if got.Dated || got.FocusX != 0.5 || got.FocusY != 0.5 {
		t.Fatalf("verdict = %+v, want not dated and a centred focus", got)
	}
}

func TestVerifyPromptForNamesTheSlot(t *testing.T) {
	plan := realphoto.Plan{Subject: "Bret Taylor", Maker: "Sierra"}
	featured := realphoto.VerifyPromptFor(plan, "Headline", realphoto.KindCommons, realphoto.SlotFeatured)
	inline := realphoto.VerifyPromptFor(plan, "Headline", realphoto.KindCommons, realphoto.SlotInline)
	if !strings.Contains(featured, "lead picture") || strings.Contains(featured, "in-body picture") {
		t.Fatalf("featured prompt = %q", featured)
	}
	if !strings.Contains(inline, "in-body picture") {
		t.Fatalf("inline prompt = %q", inline)
	}
	for _, field := range []string{"dated:", "focus_x, focus_y:", `"dated":false`, `"focus_x":0.5`} {
		if !strings.Contains(featured, field) {
			t.Errorf("prompt lacks %q", field)
		}
	}
	if realphoto.VerifyPrompt(plan, "Headline", realphoto.KindCommons) != inline {
		t.Fatal("VerifyPrompt must stay the in-body prompt")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -p 2 ./internal/realphoto/...`
Expected: FAIL to compile with `undefined: SlotFeatured`, `undefined: MinFeaturedQuality`, `good.usableFor undefined` and `undefined: realphoto.VerifyPromptFor`.

- [ ] **Step 3: Add the slot to the request**

In `apps/server-go/internal/realphoto/realphoto.go`, replace the `Request` struct with:

```go
// Request describes the article. SourceURL is the news source's page: it is
// only read to find the maker's link and is never credited or shown.
type Request struct {
	Slug      string
	Title     string
	Text      string
	SourceURL string
	// Slot is where the photo will be shown: SlotInline (the default) or
	// SlotFeatured, which has stricter checks and may use a stand-in subject.
	Slot string
	// Exclude lists photo URLs (Candidate.ImageURL) that must not be chosen.
	Exclude []string
}
```

Below the `MinQuality` constant block in the same file, add:

```go
// Slots a photo can fill.
const (
	SlotInline   = "inline"
	SlotFeatured = "featured"
)

// MinFeaturedQuality is the lowest vision quality score for the featured slot.
const MinFeaturedQuality = 8

// KindPerson is the plan kind of a person. In the featured slot a person may
// be shown in front of another organisation's backdrop (a conference stage).
const KindPerson = "person"
```

- [ ] **Step 4: Extend the verdict**

In `apps/server-go/internal/realphoto/verify.go`, replace the `Verdict` struct and add `usableFor` directly after the existing `usable` method (leave `usable` as it is):

```go
// Verdict is the vision model's judgement of one candidate photo.
type Verdict struct {
	Match           string `json:"match"`
	Photographic    bool   `json:"photographic"`
	Watermark       bool   `json:"watermark"`
	TextHeavy       bool   `json:"text_heavy"`
	MarketingBanner bool   `json:"marketing_banner"`
	Misleading      bool   `json:"misleading"`
	Prominent       bool   `json:"subject_prominent"`
	OtherBranding   bool   `json:"other_branding"`
	// Dated is true when the hardware, interface or setting is visibly out
	// of date. Only the featured slot rejects it.
	Dated bool `json:"dated"`
	// FocusX and FocusY are the centre of the subject as fractions of the
	// image's width and height; a 16:9 crop is centred on it.
	FocusX  float64 `json:"focus_x"`
	FocusY  float64 `json:"focus_y"`
	Quality int     `json:"quality"`
	Alt     string  `json:"alt"`
	Notes   string  `json:"notes"`
}
```

```go
// usableFor is usable with the slot's own rules. The featured slot needs a
// higher quality, rejects dated photos, and lets a person stand in front of
// another organisation's backdrop.
func (v Verdict) usableFor(slot, kind string) (bool, string) {
	if slot != SlotFeatured {
		return v.usable("")
	}
	relaxed := v
	if kind == KindPerson {
		relaxed.OtherBranding = false
	}
	if ok, why := relaxed.usable(""); !ok {
		return false, why
	}
	switch {
	case v.Dated:
		return false, "visibly dated"
	case v.Quality < MinFeaturedQuality:
		return false, fmt.Sprintf("quality %d below %d for the featured slot", v.Quality, MinFeaturedQuality)
	}
	return true, ""
}
```

- [ ] **Step 5: Extend the prompt, the schema and the parser**

Still in `verify.go`, make four edits.

Edit A. In the `verifyPrompt` constant, replace its first line:

```
You are checking a candidate image for the in-body picture of a technology news article. It will be shown full width (16:9 crop) in the middle of the article.
```

with:

```
You are checking a candidate image for %s
```

Edit B. In the same constant, insert these two lines directly after the line that starts with `other_branding:`:

```
dated: true if the hardware, the software on a screen, or the setting is visibly out of date for a story published today: an old phone model, an old version of an app, a photo that is clearly decades old.
focus_x, focus_y: the centre of the subject as fractions of the image's width and height, from 0 (left or top edge) to 1 (right or bottom edge). For a person, the centre of the face.
```

Edit C. In the same constant, replace the last line (the JSON shape) with:

```
{"match":"exact","photographic":true,"watermark":false,"text_heavy":false,"marketing_banner":false,"misleading":false,"subject_prominent":true,"other_branding":false,"dated":false,"focus_x":0.5,"focus_y":0.4,"quality":8,"alt":"...","notes":"..."}`
```

Edit D. Add the placements next to the other prompt constants, add the three fields to `verdictSchema`, and replace `VerifyPrompt`, `verify` and `ParseVerdict`:

```go
const (
	inlinePlacement   = "the in-body picture of a technology news article. It will be shown full width (16:9 crop) in the middle of the article."
	featuredPlacement = "the lead picture of a technology news article. It will be shown large on the homepage and at the top of the article, cropped to 16:9."
)
```

In `verdictSchema`, add to `"properties"`:

```go
		"dated":             map[string]any{"type": "BOOLEAN"},
		"focus_x":           map[string]any{"type": "NUMBER"},
		"focus_y":           map[string]any{"type": "NUMBER"},
```

and change `"required"` to:

```go
	"required": []string{"match", "photographic", "watermark", "text_heavy", "marketing_banner", "misleading", "subject_prominent", "other_branding", "dated", "focus_x", "focus_y", "quality", "alt", "notes"},
```

```go
// VerifyPrompt is the vision prompt for one in-body candidate in the given mode.
func VerifyPrompt(plan Plan, title, mode string) string {
	return VerifyPromptFor(plan, title, mode, SlotInline)
}

// VerifyPromptFor is the vision prompt for one candidate in the given mode
// and slot.
func VerifyPromptFor(plan Plan, title, mode, slot string) string {
	contextLine, photographic := commonsContext, commonsPhoto
	if mode == KindOfficial {
		contextLine, photographic = officialContext, officialPhotoArg
	}
	placement := inlinePlacement
	if slot == SlotFeatured {
		placement = featuredPlacement
	}
	maker := plan.Maker
	if maker == "" {
		maker = "unknown"
	}
	return fmt.Sprintf(verifyPrompt, placement, title, plan.Subject, maker, contextLine, photographic)
}

func (f *Finder) verify(ctx context.Context, plan Plan, request Request, mode string, data []byte) (Verdict, error) {
	jpeg, err := imaging.FitJPEG(data, verifyMaxEdge, verifyQuality)
	if err != nil {
		return Verdict{}, err
	}
	raw, err := f.vision.ReviewImage(ctx, VerifyPromptFor(plan, request.Title, mode, request.Slot), jpeg, verdictSchema)
	if err != nil {
		return Verdict{}, err
	}
	return ParseVerdict(raw)
}

// ParseVerdict reads the vision model's answer. A missing focus point means
// the centre of the image.
func ParseVerdict(raw string) (Verdict, error) {
	var answer struct {
		Verdict
		Quality *int     `json:"quality"`
		FocusX  *float64 `json:"focus_x"`
		FocusY  *float64 `json:"focus_y"`
	}
	if err := json.Unmarshal([]byte(cleanJSON(raw)), &answer); err != nil {
		return Verdict{}, fmt.Errorf("unparsable verdict: %w", err)
	}
	if answer.Quality == nil || answer.Match == "" {
		return Verdict{}, fmt.Errorf("incomplete verdict")
	}
	verdict := answer.Verdict
	verdict.Quality = min(max(*answer.Quality, 0), 10)
	verdict.FocusX, verdict.FocusY = 0.5, 0.5
	if answer.FocusX != nil && answer.FocusY != nil {
		verdict.FocusX = min(max(*answer.FocusX, 0), 1)
		verdict.FocusY = min(max(*answer.FocusY, 0), 1)
	}
	verdict.Match = strings.ToLower(strings.TrimSpace(verdict.Match))
	verdict.Alt = oneLine(verdict.Alt, 250)
	verdict.Notes = oneLine(verdict.Notes, 250)
	return verdict, nil
}
```

- [ ] **Step 6: Use the slot rules when checking candidates**

In `apps/server-go/internal/realphoto/realphoto.go`, in `verifyAll`, replace:

```go
		if ok, why := verdict.usable(mode); ok {
```

with:

```go
		if ok, why := verdict.usableFor(request.Slot, plan.Kind); ok {
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test -p 2 ./internal/realphoto/...`
Expected: PASS, including the existing tests.

- [ ] **Step 8: Commit**

```bash
git add apps/server-go/internal/realphoto
git commit -m "Give the photo check a featured slot: quality 8, no dated photos, event backdrops allowed behind a person, and a focus point for the crop"
```

---

### Task 3: Exclusions and rate limits

**Files:**
- Modify: `apps/server-go/internal/realphoto/realphoto.go` (`verifyAll`)
- Modify: `apps/server-go/internal/realphoto/fetch.go`
- Modify: `apps/server-go/internal/realphoto/commons.go` (`commons`)
- Test: `apps/server-go/internal/realphoto/exclude_test.go` (new)

- [ ] **Step 1: Write the failing tests**

Create `apps/server-go/internal/realphoto/exclude_test.go`:

```go
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
		if candidate.PageURL == "https://commons.wikimedia.org/wiki/File:Better.jpg" && !strings.Contains(candidate.Reason, "already used") {
			t.Fatalf("excluded candidate reason = %q", candidate.Reason)
		}
	}
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -p 2 ./internal/realphoto/ -run 'TestFindSkipsExcludedPhotos|TestFindStopsSearchingCommonsWhenRateLimited' -v`
Expected: both FAIL. The first chooses `File:Better.jpg`; the second counts 2 requests.

- [ ] **Step 3: Skip excluded photos**

In `apps/server-go/internal/realphoto/realphoto.go`, add `"slices"` to the imports. In `verifyAll`, directly after:

```go
		if candidate.Reason != "" {
			continue
		}
```

add:

```go
		if slices.Contains(request.Exclude, candidate.ImageURL) {
			candidate.Reason = "already used by a recent featured image"
			continue
		}
```

- [ ] **Step 4: Report a rate limit as its own error**

In `apps/server-go/internal/realphoto/fetch.go`, add below `errNotHTTP`:

```go
// ErrRateLimited reports an HTTP 429. The caller stops sending requests to
// that service for the rest of the search.
var ErrRateLimited = errors.New("rate limited")
```

and in `get`, directly before:

```go
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
```

add:

```go
	if resp.StatusCode == http.StatusTooManyRequests {
		return fetched{}, fmt.Errorf("HTTP 429: %w", ErrRateLimited)
	}
```

- [ ] **Step 5: Stop querying Commons after a rate limit**

In `apps/server-go/internal/realphoto/commons.go`, in `commons`, replace:

```go
		if err != nil {
			errs = append(errs, fmt.Errorf("search %q: %w", query, err))
			continue
		}
```

with:

```go
		if err != nil {
			errs = append(errs, fmt.Errorf("search %q: %w", query, err))
			if errors.Is(err, ErrRateLimited) {
				break
			}
			continue
		}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test -p 2 ./internal/realphoto/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add apps/server-go/internal/realphoto
git commit -m "Let a photo search exclude photos by URL, and stop querying Commons once it answers 429"
```

---

### Task 4: Stand-in subjects

**Files:**
- Modify: `apps/server-go/internal/realphoto/plan.go`
- Modify: `apps/server-go/internal/realphoto/realphoto.go` (`Result`, `find`, `choose`)
- Test: `apps/server-go/internal/realphoto/standin_test.go` (new)

- [ ] **Step 1: Write the failing tests**

Create `apps/server-go/internal/realphoto/standin_test.go`:

```go
package realphoto_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

const fundingPlan = `{"photographable":false,"subject":"","kind":"none","queries":[],"maker":"Sierra","reason":"A funding round.",
"stand_ins":[{"subject":"Bret Taylor","kind":"person","queries":["Bret Taylor","Bret Taylor Sierra"]}]}`

func TestParsePlanCleansStandIns(t *testing.T) {
	plan, err := realphoto.ParsePlan(`{"photographable":false,"reason":"r","stand_ins":[
		{"subject":"  Bret   Taylor ","kind":"Person","queries":["a","b","c"]},
		{"subject":"A mood","kind":"metaphor","queries":["x"]},
		{"subject":"","kind":"place","queries":["y"]},
		{"subject":"Sierra headquarters","kind":"place","queries":[]},
		{"subject":"Sierra app on a phone","kind":"product_in_use","queries":["Sierra app"]},
		{"subject":"Fourth","kind":"place","queries":["z"]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.StandIns) != realphoto.MaxStandIns {
		t.Fatalf("stand-ins = %+v, want %d", plan.StandIns, realphoto.MaxStandIns)
	}
	first, second := plan.StandIns[0], plan.StandIns[1]
	if first.Subject != "Bret Taylor" || first.Kind != "person" || len(first.Queries) != 2 {
		t.Fatalf("first = %+v", first)
	}
	if second.Subject != "Sierra headquarters" || len(second.Queries) != 1 || second.Queries[0] != "Sierra headquarters" {
		t.Fatalf("second = %+v: a stand-in without queries searches for its subject", second)
	}
}

func TestParsePlanDropsStandInsOfAPhotographableStory(t *testing.T) {
	plan, err := realphoto.ParsePlan(`{"photographable":true,"subject":"WiCi One","queries":["WiCi One"],"stand_ins":[{"subject":"x","kind":"place","queries":["x"]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.StandIns) != 0 {
		t.Fatalf("stand-ins = %+v, want none", plan.StandIns)
	}
}

func TestFindUsesAStandInOnlyForTheFeaturedSlot(t *testing.T) {
	var queries []string
	vision := &fakeVision{verdicts: map[string]string{"green": verdict(8, ""), "blue": verdict(9, "")}}
	finder := realphoto.NewFinder(&fakeText{plan: fundingPlan}, vision, &http.Client{Transport: twoFiles(t, &queries)}, nil)
	request := realphoto.Request{Slug: "sierra", Title: "Sierra raises $1.75 billion", Text: "..."}

	inline := finder.Find(context.Background(), request)
	if inline.Found || len(queries) != 0 {
		t.Fatalf("the body must not use a stand-in: found %t, queries %v", inline.Found, queries)
	}

	request.Slot = realphoto.SlotFeatured
	featured := finder.Find(context.Background(), request)
	if !featured.Found || featured.StandIn == nil || featured.StandIn.Subject != "Bret Taylor" {
		t.Fatalf("featured = %+v", featured)
	}
	if featured.Credit.Kind != realphoto.KindCommons || featured.Alt != "Photo of Bret Taylor." {
		t.Fatalf("credit %+v alt %q", featured.Credit, featured.Alt)
	}
	if len(queries) != 2 || queries[0] != "filetype:bitmap Bret Taylor" {
		t.Fatalf("queries = %v", queries)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -p 2 ./internal/realphoto/...`
Expected: FAIL to compile with `plan.StandIns undefined` and `undefined: realphoto.MaxStandIns`.

- [ ] **Step 3: Add stand-ins to the plan**

In `apps/server-go/internal/realphoto/plan.go`, add `"slices"` to the imports, add the field to `Plan` (after `OfficialPages`), and add the types below the struct:

```go
	// StandIns are real things a photo could honestly show in place of a
	// story with nothing physical in it. Only the featured slot uses them.
	StandIns []StandIn `json:"stand_ins,omitempty"`
```

```go
// StandIn is one stand-in subject and how to search Commons for it.
type StandIn struct {
	Subject string   `json:"subject"`
	Kind    string   `json:"kind"`
	Queries []string `json:"queries"`
}

// Kinds of stand-in.
const (
	StandInPerson  = KindPerson
	StandInPlace   = "place"
	StandInProduct = "product_in_use"
	// MaxStandIns caps the stand-ins kept from one plan.
	MaxStandIns = 3
)

var standInKinds = []string{StandInPerson, StandInPlace, StandInProduct}
```

- [ ] **Step 4: Ask the planner for stand-ins**

In the `planPrompt` constant, make two edits.

Replace the JSON shape line:

```
{"photographable":true,"subject":"...","kind":"product|device|robot|vehicle|building|place|event|person|none","queries":["...","..."],"maker":"...","maker_domain":"...","official_pages":["..."],"reason":"..."}
```

with:

```
{"photographable":true,"subject":"...","kind":"product|device|robot|vehicle|building|place|event|person|none","queries":["...","..."],"maker":"...","maker_domain":"...","official_pages":["..."],"reason":"...","stand_ins":[{"subject":"...","kind":"person|place|product_in_use","queries":["...","..."]}]}
```

Insert these lines directly after the line `reason: one short sentence.`:

```
stand_ins: [] when photographable is true. When it is false, up to three real things a photo could honestly show in place of the subject, the most fitting first, or [] if none is honest:
- "person": the one clear leader of the company the story is about (its chief executive or founder), only when the story is a corporate action by that company: funding, a leadership change, earnings, an acquisition, a lawsuit. subject is the person's full name.
- "place": the headquarters of the company, regulator or court the story is about. subject names the building, e.g. "Nvidia headquarters".
- "product_in_use": a software product people see on a device, when the story is about that product. subject says what the photo shows, e.g. "ChatGPT app on a smartphone".
Never offer a stand-in for a study, a market trend, or a story about several companies at once. Each stand-in has two short Wikimedia Commons search queries, written like queries above.
```

- [ ] **Step 5: Clean the stand-ins when parsing**

In `ParsePlan`, replace the query-cleaning loop:

```go
	var queries []string
	for _, query := range plan.Queries {
		if query = oneLine(strings.NewReplacer(`"`, "", "|", " ").Replace(query), 100); query != "" {
			queries = append(queries, query)
		}
		if len(queries) == 2 {
			break
		}
	}
	plan.Queries = queries
```

with:

```go
	plan.Queries = cleanQueries(plan.Queries)
	plan.StandIns = cleanStandIns(plan.StandIns)
	if plan.Photographable {
		plan.StandIns = nil
	}
```

and add below `ParsePlan`:

```go
// cleanQueries keeps the first two non-empty search queries, without quotes
// or operators.
func cleanQueries(raw []string) []string {
	var queries []string
	for _, query := range raw {
		if query = oneLine(strings.NewReplacer(`"`, "", "|", " ").Replace(query), 100); query != "" {
			queries = append(queries, query)
		}
		if len(queries) == 2 {
			break
		}
	}
	return queries
}

// cleanStandIns keeps at most MaxStandIns stand-ins that have a subject and a
// known kind. One without a query searches for its subject.
func cleanStandIns(raw []StandIn) []StandIn {
	var kept []StandIn
	for _, standIn := range raw {
		standIn.Subject = oneLine(standIn.Subject, 120)
		standIn.Kind = strings.ToLower(strings.TrimSpace(standIn.Kind))
		if standIn.Subject == "" || !slices.Contains(standInKinds, standIn.Kind) {
			continue
		}
		if standIn.Queries = cleanQueries(standIn.Queries); len(standIn.Queries) == 0 {
			standIn.Queries = []string{standIn.Subject}
		}
		kept = append(kept, standIn)
		if len(kept) == MaxStandIns {
			break
		}
	}
	return kept
}
```

- [ ] **Step 6: Search for a stand-in in the featured slot**

In `apps/server-go/internal/realphoto/realphoto.go`:

Add `"errors"` to the imports.

Add a field to `Result`, after `Plan`:

```go
	// StandIn is set when the chosen photo shows a stand-in subject.
	StandIn *StandIn `json:"stand_in,omitempty"`
```

In `find`, replace:

```go
	if !plan.Photographable {
		result.Reason = "no photographable subject: " + plan.Reason
		return result
	}
```

with:

```go
	if !plan.Photographable {
		if request.Slot != SlotFeatured || len(plan.StandIns) == 0 {
			result.Reason = "no photographable subject: " + plan.Reason
			return result
		}
		return f.findStandIn(ctx, request, plan, result)
	}
```

In `find`, change the two `result.choose(...)` calls to pass the subject:

```go
		result.choose(best, commonsCredit(*best), plan.Subject)
```

```go
		result.choose(best, Credit{Kind: KindOfficial, Text: "Image: " + maker, URL: best.PageURL}, plan.Subject)
```

Replace `choose` and add `findStandIn` below it:

```go
func (r *Result) choose(best *Candidate, credit Credit, subject string) {
	chosen := *best
	r.Found = true
	r.Image = best.data
	r.Alt = strings.TrimSpace(best.Verdict.Alt)
	if r.Alt == "" && subject != "" {
		r.Alt = "Photo of " + subject + "."
	}
	r.Credit = &credit
	r.Chosen = &chosen
}

// findStandIn searches Commons for each stand-in subject in turn and takes
// the first that yields a usable photo. A maker's official page is not
// searched: it is the wrong source for a subject the story is not about.
func (f *Finder) findStandIn(ctx context.Context, request Request, plan Plan, result Result) Result {
	for i := range plan.StandIns {
		standIn := plan.StandIns[i]
		subPlan := Plan{Photographable: true, Subject: standIn.Subject, Kind: standIn.Kind, Queries: standIn.Queries, Maker: plan.Maker}
		candidates, err := f.commons(ctx, request, subPlan)
		result.Candidates = append(result.Candidates, candidates...)
		if err != nil {
			result.Notes = append(result.Notes, "commons: "+err.Error())
		}
		if best := bestCandidate(candidates); best != nil {
			result.choose(best, commonsCredit(*best), standIn.Subject)
			result.StandIn = &standIn
			result.Reason = fmt.Sprintf("Wikimedia Commons photo of stand-in %s (%s), score %d", standIn.Subject, standIn.Kind, best.Verdict.Quality)
			return result
		}
		if ctx.Err() != nil {
			result.Reason = "cancelled"
			return result
		}
		if errors.Is(err, ErrRateLimited) {
			break
		}
	}
	result.Reason = "no usable photo of a stand-in subject"
	return result
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test -p 2 ./internal/realphoto/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add apps/server-go/internal/realphoto
git commit -m "Let the photo planner offer stand-in subjects (a leader, a headquarters, the product on a device) and search Commons for them in the featured slot"
```

---

### Task 5: Store a featured photo's credit

**Files:**
- Create: `apps/server-go/internal/content/migrations/017_article_featured_image_credits.sql`
- Modify: `apps/server-go/internal/content/migrations.go`
- Modify: `apps/server-go/internal/publisher/publisher.go` (types)
- Modify: `apps/server-go/internal/publisher/articles.go`
- Test: `apps/server-go/internal/publisher/articles_credit_test.go` (new)

- [ ] **Step 1: Write the failing tests**

Create `apps/server-go/internal/publisher/articles_credit_test.go`:

```go
package publisher_test

import (
	"context"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

var photoCredit = &publisher.ImageCredit{
	Kind: "commons", Text: "Photo: Jane Doe / CC BY 4.0, via Wikimedia Commons",
	URL: "https://commons.wikimedia.org/wiki/File:X.jpg", License: "CC BY 4.0",
	LicenseURL: "https://creativecommons.org/licenses/by/4.0", PhotoURL: "https://upload.wikimedia.org/x.jpg",
	Alt: "Bret Taylor on stage.",
}

func TestPublishStoresTheFeaturedCreditOnlyWhenPresent(t *testing.T) {
	db := openDB(t)
	articles := publisher.NewSQLiteArticles(db, func() time.Time { return publishNow })
	ctx := context.Background()

	with := newArticle("sierra-raises", "https://techcrunch.com/sierra", "Sierra raises a round", "<p>Body</p>")
	with.FeaturedCredit = photoCredit
	id, err := articles.Publish(ctx, with)
	if err != nil {
		t.Fatal(err)
	}
	var got publisher.ImageCredit
	err = db.QueryRow(`SELECT kind, credit, credit_url, license, license_url, photo_url, alt FROM article_featured_image_credits WHERE article_id = ?`, id).
		Scan(&got.Kind, &got.Text, &got.URL, &got.License, &got.LicenseURL, &got.PhotoURL, &got.Alt)
	if err != nil {
		t.Fatal(err)
	}
	if got != *photoCredit {
		t.Fatalf("stored credit = %+v", got)
	}

	if _, err := articles.Publish(ctx, newArticle("drawn", "https://techcrunch.com/drawn", "A drawn story", "<p>Body</p>")); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM article_featured_image_credits`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("credit rows = %d, want 1: a drawn image has no credit", rows)
	}
}

func TestRecentFeaturedPhotoURLsHonoursTheWindow(t *testing.T) {
	db := openDB(t)
	now := publishNow
	articles := publisher.NewSQLiteArticles(db, func() time.Time { return now })
	ctx := context.Background()

	old := newArticle("old", "https://techcrunch.com/old", "An old story", "<p>Body</p>")
	old.FeaturedCredit = &publisher.ImageCredit{Kind: "commons", Text: "Photo: A", PhotoURL: "https://upload.wikimedia.org/old.jpg"}
	if _, err := articles.Publish(ctx, old); err != nil {
		t.Fatal(err)
	}
	now = publishNow.Add(40 * 24 * time.Hour)
	fresh := newArticle("fresh", "https://techcrunch.com/fresh", "A fresh story", "<p>Body</p>")
	fresh.FeaturedCredit = &publisher.ImageCredit{Kind: "official", Text: "Image: Nvidia", PhotoURL: "https://nvidia.com/rack.jpg"}
	if _, err := articles.Publish(ctx, fresh); err != nil {
		t.Fatal(err)
	}

	urls, err := articles.RecentFeaturedPhotoURLs(ctx, now.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 || urls[0] != "https://nvidia.com/rack.jpg" {
		t.Fatalf("recent photo URLs = %v, want only the fresh one", urls)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -p 2 ./internal/publisher/...`
Expected: FAIL to compile with `undefined: publisher.ImageCredit`.

- [ ] **Step 3: Add the migration**

Create `apps/server-go/internal/content/migrations/017_article_featured_image_credits.sql`:

```sql
-- The credit of a featured image that is a real photo: a Wikimedia Commons
-- photo (kind 'commons', with its licence) or the maker's own official image
-- (kind 'official'). A generated illustration has no row. credit is the
-- caption shown on the article page; credit_url is the Commons file page or
-- the maker's page, never the news source. photo_url is the file the photo
-- was copied from, kept so the same photo is not used again soon. A separate
-- table because adoption (internal/database/adopt) can verify only
-- migrations that create new schema objects.
CREATE TABLE article_featured_image_credits (
    article_id INTEGER PRIMARY KEY REFERENCES articles(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK(kind IN ('commons', 'official')),
    credit TEXT NOT NULL,
    credit_url TEXT NOT NULL DEFAULT '',
    license TEXT NOT NULL DEFAULT '',
    license_url TEXT NOT NULL DEFAULT '',
    photo_url TEXT NOT NULL DEFAULT '',
    alt TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
```

In `apps/server-go/internal/content/migrations.go`, add next to the other embeds:

```go
//go:embed migrations/017_article_featured_image_credits.sql
var articleFeaturedImageCreditsSchema string
```

and add to the slice in `Migrations()`, after version 16:

```go
		{Version: 17, Name: "article featured image credits", SQL: articleFeaturedImageCreditsSchema},
```

- [ ] **Step 4: Add the credit type**

In `apps/server-go/internal/publisher/publisher.go`, replace the `Illustration` struct and add `ImageCredit` above it:

```go
// ImageCredit credits a featured image that is a real photo. Text is the
// caption ("Photo: Jane Doe / CC BY 4.0, via Wikimedia Commons"), URL the
// page it links to, PhotoURL the file the photo was copied from, and Alt one
// sentence saying what the photo shows.
type ImageCredit struct {
	Kind       string
	Text       string
	URL        string
	License    string
	LicenseURL string
	PhotoURL   string
	Alt        string
}

// Illustration is a stored featured image. Discard removes it when the
// article could not be published after the image was uploaded. Credit is set
// only when the image is a real photo.
type Illustration struct {
	URL     string
	Credit  *ImageCredit
	Discard func(context.Context)
}
```

In `apps/server-go/internal/publisher/articles.go`, add to `NewArticle`, after `FeaturedImage`:

```go
	// FeaturedCredit is set when the featured image is a real photo.
	FeaturedCredit *ImageCredit
```

- [ ] **Step 5: Write the credit with the article and read recent photo URLs**

In `apps/server-go/internal/publisher/articles.go`, in `Publish`, directly before:

```go
	if article.CandidateID != 0 {
```

add:

```go
	if article.FeaturedCredit != nil {
		if err = insertFeaturedCredit(ctx, tx, id, *article.FeaturedCredit, stamp); err != nil {
			return 0, err
		}
	}
```

and add below `insertSummary`:

```go
func insertFeaturedCredit(ctx context.Context, tx *sql.Tx, articleID int64, credit ImageCredit, stamp string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO article_featured_image_credits
		(article_id, kind, credit, credit_url, license, license_url, photo_url, alt, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		articleID, credit.Kind, credit.Text, credit.URL, credit.License, credit.LicenseURL, credit.PhotoURL, credit.Alt, stamp); err != nil {
		return fmt.Errorf("insert featured image credit: %w", err)
	}
	return nil
}

// RecentFeaturedPhotoURLs lists the photos used as featured images since
// since, newest first, so the photo step does not pick one of them again.
func (a *SQLiteArticles) RecentFeaturedPhotoURLs(ctx context.Context, since time.Time) ([]string, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT photo_url FROM article_featured_image_credits
		WHERE photo_url <> '' AND created_at >= ? ORDER BY article_id DESC`, since.UTC().Format(sqliteDateTime))
	if err != nil {
		return nil, fmt.Errorf("read recent featured photos: %w", err)
	}
	defer rows.Close()
	var urls []string
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			return nil, fmt.Errorf("scan recent featured photo: %w", err)
		}
		urls = append(urls, url)
	}
	return urls, rows.Err()
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test -p 2 ./internal/publisher/... ./internal/content/... ./internal/app/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add apps/server-go/internal/content apps/server-go/internal/publisher
git commit -m "Store a featured photo's credit with the article, in a new table, and list the photos used recently"
```

---

### Task 6: The publisher passes the source page in and the credit out

**Files:**
- Modify: `apps/server-go/internal/publisher/publisher.go`
- Test: `apps/server-go/internal/publisher/publisher_test.go`

- [ ] **Step 1: Write the failing test**

In `apps/server-go/internal/publisher/publisher_test.go`, replace the `fakeIllustrator` type and its method, add a `db` field to `harness`, set it in `newHarness`, and add the test.

```go
type fakeIllustrator struct {
	err       error
	reference string
	source    string
	credit    *publisher.ImageCredit
	discarded bool
}

func (f *fakeIllustrator) Illustrate(_ context.Context, request publisher.IllustrationRequest) (publisher.Illustration, error) {
	f.reference = request.ReferenceImageURL
	f.source = request.SourceURL
	if f.err != nil {
		return publisher.Illustration{}, f.err
	}
	return publisher.Illustration{URL: "https://images.example.com/x.webp", Credit: f.credit, Discard: func(context.Context) { f.discarded = true }}, nil
}
```

In the `harness` struct add the field `db *sql.DB` (add `"database/sql"` to the imports if it is not there). In `newHarness`, change the struct literal to set it:

```go
	h := &harness{db: db, store: store, illustrator: &fakeIllustrator{err: illustratorErr}, notifier: &fakeNotifier{}, now: publishNow}
```

Add the test:

```go
func TestPublishNextStoresAPhotoCreditAndPassesTheSourcePage(t *testing.T) {
	h := newHarness(t, fakeFetcher{body: sourcePage()}, fakeRewriter{}, nil)
	h.illustrator.credit = photoCredit
	if published, err := h.pub.PublishNext(context.Background()); err != nil || !published {
		t.Fatalf("published=%v err=%v", published, err)
	}
	if !strings.Contains(h.illustrator.source, "techcrunch.com") {
		t.Fatalf("source page = %q, want the article's canonical URL", h.illustrator.source)
	}
	var text, photoURL string
	if err := h.db.QueryRow(`SELECT credit, photo_url FROM article_featured_image_credits`).Scan(&text, &photoURL); err != nil {
		t.Fatal(err)
	}
	if text != photoCredit.Text || photoURL != photoCredit.PhotoURL {
		t.Fatalf("stored credit = %q %q", text, photoURL)
	}
}
```

Add `"strings"` to the test file's imports if it is not there.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -p 2 ./internal/publisher/ -run TestPublishNextStoresAPhotoCredit -v`
Expected: FAIL to compile with `request.SourceURL undefined`.

- [ ] **Step 3: Implement**

In `apps/server-go/internal/publisher/publisher.go`, add to `IllustrationRequest`, after `ReferenceImageURL`:

```go
	// SourceURL is the news source's page. The photo step reads it only to
	// find the maker's own site; it is never credited or shown.
	SourceURL string
```

Replace the `Illustrate` call and the `Publish` call in the publish function:

```go
	illustration, err := p.Illustrator.Illustrate(ctx, IllustrationRequest{
		Slug: finalSlug, Title: article.Title, Excerpt: article.Excerpt, Content: article.Content, ReferenceImageURL: reference,
		SourceURL: canonicalURL,
	})
	if err != nil {
		return 0, "", fmt.Errorf("featured image: %w", err)
	}
	articleID, err := p.Articles.Publish(ctx, NewArticle{
		CandidateID: candidate.ID, Title: article.Title, Slug: finalSlug, Excerpt: article.Excerpt, Content: article.Content,
		FeaturedImage: illustration.URL, FeaturedCredit: illustration.Credit, Source: source, SourceURL: canonicalURL,
		MetaTitle: article.MetaTitle, MetaDescription: article.MetaDescription,
		TLDR: article.TLDR, WhyItMatters: article.WhyItMatters,
	})
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -p 2 ./internal/publisher/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/server-go/internal/publisher
git commit -m "Tell the illustrator the source page, and store the credit when it returns a real photo"
```

---

### Task 7: The photo step in the featured-image chain

**Files:**
- Create: `apps/server-go/internal/illustration/photo.go`
- Modify: `apps/server-go/internal/illustration/providers.go`
- Modify: `apps/server-go/internal/illustration/pipeline.go`
- Test: `apps/server-go/internal/illustration/photo_test.go` (new)

- [ ] **Step 1: Write the failing tests**

Create `apps/server-go/internal/illustration/photo_test.go`:

```go
package illustration_test

import (
	"bytes"
	"context"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

type fakePhotoFinder struct {
	result   realphoto.Result
	requests []realphoto.Request
}

func (f *fakePhotoFinder) Find(_ context.Context, request realphoto.Request) realphoto.Result {
	f.requests = append(f.requests, request)
	return f.result
}

type fakeRecentPhotos struct {
	urls  []string
	err   error
	since time.Time
}

func (f *fakeRecentPhotos) RecentFeaturedPhotoURLs(_ context.Context, since time.Time) ([]string, error) {
	f.since = since
	return f.urls, f.err
}

func foundPhoto(t *testing.T) realphoto.Result {
	t.Helper()
	return realphoto.Result{
		Found: true, Image: solidPNG(t, 400, 400), Alt: "Bret Taylor on stage.",
		Credit: &realphoto.Credit{Kind: realphoto.KindCommons, Text: "Photo: Jane Doe / CC BY 4.0, via Wikimedia Commons",
			URL: "https://commons.wikimedia.org/wiki/File:X.jpg", License: "CC BY 4.0", LicenseURL: "https://creativecommons.org/licenses/by/4.0"},
		Chosen: &realphoto.Candidate{ImageURL: "https://upload.wikimedia.org/x.jpg", Verdict: &realphoto.Verdict{Quality: 9, FocusX: 0.5, FocusY: 0.2}},
		Reason: "Wikimedia Commons photo of stand-in Bret Taylor (person), score 9",
	}
}

func TestPhotoStepWinsWithoutAnalysisOrGeneration(t *testing.T) {
	finder := &fakePhotoFinder{result: foundPhoto(t)}
	gem := &fakeProvider{name: "gemini", attempts: 3, image: solidPNG(t, 64, 36)}
	fixture := newFixture(t, illustration.PhotoStep(finder), illustration.ProviderStep(gem))
	recent := &fakeRecentPhotos{urls: []string{"https://upload.wikimedia.org/used.jpg"}}
	fixture.deps.RecentPhotos = recent
	fixture.request.Content = "<p>Sierra raised <b>$1.75 billion</b>.</p>"
	fixture.request.SourceURL = "https://techcrunch.com/sierra"

	result, err := fixture.produce(t)
	if err != nil {
		t.Fatal(err)
	}
	if result.Report.Provider != "photo" || fixture.analyzer.calls != 0 || len(gem.requests) != 0 || len(fixture.reviewer.reviewed) != 0 {
		t.Fatalf("report %+v analyzer calls %d gemini calls %d", result.Report, fixture.analyzer.calls, len(gem.requests))
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(result.Image))
	if err != nil || config.Width != 400 || config.Height != 225 {
		t.Fatalf("image %dx%d err %v, want a 400x225 crop", config.Width, config.Height, err)
	}
	want := publisher.ImageCredit{Kind: "commons", Text: "Photo: Jane Doe / CC BY 4.0, via Wikimedia Commons",
		URL: "https://commons.wikimedia.org/wiki/File:X.jpg", License: "CC BY 4.0", LicenseURL: "https://creativecommons.org/licenses/by/4.0",
		PhotoURL: "https://upload.wikimedia.org/x.jpg", Alt: "Bret Taylor on stage."}
	if result.Credit == nil || *result.Credit != want {
		t.Fatalf("credit = %+v", result.Credit)
	}
	request := finder.requests[0]
	if request.Slot != realphoto.SlotFeatured || request.SourceURL != "https://techcrunch.com/sierra" ||
		strings.Contains(request.Text, "<") || !strings.Contains(request.Text, "$1.75 billion") {
		t.Fatalf("finder request = %+v", request)
	}
	if len(request.Exclude) != 1 || request.Exclude[0] != "https://upload.wikimedia.org/used.jpg" {
		t.Fatalf("exclude = %v", request.Exclude)
	}
	if recent.since.IsZero() {
		t.Fatal("the recent photos were not asked for")
	}
}

func TestPhotoStepHandsOverWhenNoPhotoPasses(t *testing.T) {
	finder := &fakePhotoFinder{result: realphoto.Result{Reason: "no usable photo of a stand-in subject"}}
	gem := &fakeProvider{name: "gemini", attempts: 3, image: solidPNG(t, 64, 36)}
	fixture := newFixture(t, illustration.PhotoStep(finder), illustration.ProviderStep(gem))

	result, err := fixture.produce(t)
	if err != nil {
		t.Fatal(err)
	}
	if result.Report.Provider != "gemini" || result.Credit != nil || fixture.analyzer.calls != 1 {
		t.Fatalf("report %+v credit %+v analyzer calls %d", result.Report, result.Credit, fixture.analyzer.calls)
	}
	if len(result.Report.Attempts) == 0 || result.Report.Attempts[0].Provider != "photo" {
		t.Fatalf("attempts = %+v, want the photo step first", result.Report.Attempts)
	}
}

func TestPhotoStepIsSkippedWhenOnlyADrawingIsWanted(t *testing.T) {
	finder := &fakePhotoFinder{result: foundPhoto(t)}
	gem := &fakeProvider{name: "gemini", attempts: 3, image: solidPNG(t, 64, 36)}
	fixture := newFixture(t, illustration.PhotoStep(finder), illustration.ProviderStep(gem))

	result, err := illustration.NewPipeline(fixture.deps).ProduceGenerated(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if len(finder.requests) != 0 || result.Report.Provider != "gemini" {
		t.Fatalf("finder calls %d provider %q", len(finder.requests), result.Report.Provider)
	}
}

func TestAChainThatOnlyFindsNoPhotoFailsForGood(t *testing.T) {
	finder := &fakePhotoFinder{result: realphoto.Result{Reason: "no photographable subject"}}
	fixture := newFixture(t, illustration.PhotoStep(finder))

	_, err := fixture.produce(t)
	if err == nil || !publisher.IsPermanent(err) || !strings.Contains(err.Error(), illustration.ErrNoPhoto.Error()) {
		t.Fatalf("err = %v, want a permanent failure that says no usable real photo", err)
	}
}

func TestIllustrateReturnsThePhotoCredit(t *testing.T) {
	finder := &fakePhotoFinder{result: foundPhoto(t)}
	fixture := newFixture(t, illustration.PhotoStep(finder))
	fixture.deps.Store = &fakeImageStore{}

	made, err := illustration.NewPipeline(fixture.deps).Illustrate(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if made.Credit == nil || made.Credit.PhotoURL != "https://upload.wikimedia.org/x.jpg" || made.URL == "" {
		t.Fatalf("illustration = %+v", made)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -p 2 ./internal/illustration/ -run 'Photo|NoPhoto' -v`
Expected: FAIL to compile with `undefined: illustration.PhotoStep`, `fixture.deps.RecentPhotos undefined`, `result.Credit undefined` and `undefined: illustration.ErrNoPhoto`.

- [ ] **Step 3: Name the provider**

In `apps/server-go/internal/illustration/providers.go`, add `ProviderPhoto` to the provider name constants:

```go
	ProviderCodex  = "codex"
	ProviderGemini = "gemini"
	ProviderSource = "source"
	ProviderPhoto  = "photo"
```

- [ ] **Step 4: Write the photo step**

Create `apps/server-go/internal/illustration/photo.go`:

```go
package illustration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

// ErrNoPhoto reports that the "photo" step found no real photo that passed
// the checks.
var ErrNoPhoto = errors.New("no usable real photo")

// photoReuseWindow is how long a photo stays off limits after it was used as
// a featured image.
const photoReuseWindow = 30 * 24 * time.Hour

// featuredAspect is the shape every featured image is cropped to.
const featuredAspect = 16.0 / 9.0

// PhotoFinder looks for a real photo of the article's subject
// (realphoto.Finder). It never fails; Result.Found says whether it found one.
type PhotoFinder interface {
	Find(ctx context.Context, request realphoto.Request) realphoto.Result
}

// RecentPhotos lists the photos used as featured images since a time
// (publisher.SQLiteArticles), so the same photo is not chosen again soon.
type RecentPhotos interface {
	RecentFeaturedPhotoURLs(ctx context.Context, since time.Time) ([]string, error)
}

// PhotoStep is the chain step that uses a real, credited photo.
func PhotoStep(finder PhotoFinder) Step { return Step{Name: ProviderPhoto, Photos: finder} }

// PhotoReport says which photo the "photo" step chose.
type PhotoReport struct {
	Kind      string `json:"kind"`
	Credit    string `json:"credit"`
	CreditURL string `json:"credit_url,omitempty"`
	PhotoURL  string `json:"photo_url"`
	StandIn   string `json:"stand_in,omitempty"`
	Quality   int    `json:"quality"`
}

// recentPhotos is the photos used lately. A failed read only costs variety.
func (r *pipelineRun) recentPhotos(ctx context.Context) []string {
	deps := r.p.deps
	if deps.RecentPhotos == nil {
		return nil
	}
	urls, err := deps.RecentPhotos.RecentFeaturedPhotoURLs(ctx, deps.Now().Add(-photoReuseWindow))
	if err != nil {
		deps.Logger.WarnContext(ctx, "recent featured photos unavailable", "error", err)
		return nil
	}
	return urls
}

// photo runs the "photo" step: find a real photo, crop it to 16:9 around the
// subject and keep its credit. It reports false when no photo passed, so the
// chain moves on.
func (r *pipelineRun) photo(ctx context.Context, step Step) ([]byte, bool) {
	deps := r.p.deps
	started := deps.Now()
	found := step.Photos.Find(ctx, realphoto.Request{
		Slug: r.request.Slug, Title: r.request.Title, Text: content.StripHTML(r.request.Content),
		SourceURL: r.request.SourceURL, Slot: realphoto.SlotFeatured, Exclude: r.recentPhotos(ctx),
	})
	if !found.Found || found.Credit == nil || found.Chosen == nil || len(found.Image) == 0 {
		r.fail(ProviderPhoto, 0, started, fmt.Errorf("%w: %s", ErrNoPhoto, found.Reason))
		return nil, false
	}
	focusX, focusY, quality := 0.5, 0.5, 0
	if verdict := found.Chosen.Verdict; verdict != nil {
		focusX, focusY, quality = verdict.FocusX, verdict.FocusY, verdict.Quality
	}
	cropped, err := imaging.CropToAspect(found.Image, featuredAspect, focusX, focusY)
	if err != nil {
		r.fail(ProviderPhoto, 0, started, fmt.Errorf("%w: crop: %v", ErrNoPhoto, err))
		return nil, false
	}
	r.credit = &publisher.ImageCredit{
		Kind: found.Credit.Kind, Text: found.Credit.Text, URL: found.Credit.URL,
		License: found.Credit.License, LicenseURL: found.Credit.LicenseURL,
		PhotoURL: found.Chosen.ImageURL, Alt: found.Alt,
	}
	report := &PhotoReport{Kind: found.Credit.Kind, Credit: found.Credit.Text, CreditURL: found.Credit.URL, PhotoURL: found.Chosen.ImageURL, Quality: quality}
	if found.StandIn != nil {
		report.StandIn = found.StandIn.Subject
	}
	r.report.Provider = ProviderPhoto
	r.report.Photo = report
	r.report.Collage = false
	r.report.Attempts = append(r.report.Attempts, Attempt{Provider: ProviderPhoto, Outcome: "ok", Seconds: deps.Now().Sub(started).Seconds()})
	deps.Logger.InfoContext(ctx, "featured photo chosen", "slug", r.request.Slug, "kind", report.Kind, "stand_in", report.StandIn, "quality", quality, "url", report.PhotoURL)
	return cropped, true
}
```

- [ ] **Step 5: Wire the step into the pipeline**

In `apps/server-go/internal/illustration/pipeline.go`, make these edits.

Edit A. Replace the `Step` type and its comment:

```go
// Step is one entry of FEATURED_IMAGE_CHAIN: a generating provider, a photo
// finder for "photo", or neither for "source" (copy the source image).
type Step struct {
	Name     string
	Provider Provider
	Photos   PhotoFinder
}
```

Edit B. Add to `PipelineDeps`, after `History`:

```go
	// RecentPhotos lists photos used lately so the "photo" step does not
	// repeat one; nil lets it repeat.
	RecentPhotos RecentPhotos
```

Edit C. Add to `Report`, after `SourceImage`:

```go
	Photo       *PhotoReport `json:"photo,omitempty"`
```

Edit D. Replace the `Result` type:

```go
// Result is the final image (PNG, or the source's own bytes), its report,
// and its credit when it is a real photo.
type Result struct {
	Image  []byte
	Report Report
	Credit *publisher.ImageCredit
}
```

Edit E. In `Illustrate`, replace:

```go
	p.recordChoice(ctx, result.Report)
	return illustration, nil
```

with:

```go
	illustration.Credit = result.Credit
	p.recordChoice(ctx, result.Report)
	return illustration, nil
```

Edit F. In `Pipeline.produce`, replace the final return:

```go
	return Result{Image: image, Report: *run.report}, nil
```

with:

```go
	return Result{Image: image, Report: *run.report, Credit: run.credit}, nil
```

Edit G. Add a field to `pipelineRun`, after `source`:

```go
	// credit is set when the "photo" step supplied the image.
	credit *publisher.ImageCredit
```

Edit H. In `pipelineRun.produce`, replace the opening of the function, from `deps := r.p.deps` down to and including the line `for _, step := range deps.Chain {` of the first loop:

```go
	deps := r.p.deps
	if !r.generatedOnly {
		r.fetchSource(ctx)
	}

	var brief *Brief
	for _, step := range deps.Chain {
```

with:

```go
	deps := r.p.deps
	// A real photo needs neither the source image nor a brief, so the photo
	// steps that lead the chain run before both.
	lead := 0
	for ; lead < len(deps.Chain) && deps.Chain[lead].Photos != nil; lead++ {
		if r.generatedOnly {
			continue
		}
		if image, ok := r.photo(ctx, deps.Chain[lead]); ok {
			return image, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	chain := deps.Chain[lead:]
	if !r.generatedOnly {
		r.fetchSource(ctx)
	}

	var brief *Brief
	for _, step := range chain {
```

Edit I. In the same function, replace the start of the second loop:

```go
	for _, step := range deps.Chain {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if step.Provider == nil {
```

with:

```go
	for _, step := range chain {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if step.Photos != nil {
			if r.generatedOnly {
				continue
			}
			if image, ok := r.photo(ctx, step); ok {
				return image, nil
			}
			continue
		}
		if step.Provider == nil {
```

Edit J. In `classifyChainFailure`, replace:

```go
		if !publisher.IsPermanent(err) && !errors.Is(err, ErrNoSourceImage) && !errors.Is(err, ErrNoBrief) {
```

with:

```go
		if !publisher.IsPermanent(err) && !errors.Is(err, ErrNoSourceImage) && !errors.Is(err, ErrNoBrief) && !errors.Is(err, ErrNoPhoto) {
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test -p 2 ./internal/illustration/...`
Expected: PASS, including every existing pipeline test.

- [ ] **Step 7: Commit**

```bash
git add apps/server-go/internal/illustration
git commit -m "Add a photo step to the featured-image chain: a real photo, cropped to 16:9 around its subject, wins before any brief is written"
```

---

### Task 8: Accept and wire `photo`

**Files:**
- Modify: `apps/server-go/internal/config/config.go`
- Modify: `apps/server-go/internal/config/config_test.go`
- Modify: `apps/server-go/internal/illustration/wiring.go`
- Modify: `apps/server-go/internal/app/app.go`
- Test: `apps/server-go/internal/illustration/wiring_test.go` (new)

- [ ] **Step 1: Write the failing tests**

In `apps/server-go/internal/config/config_test.go`, in the table of chain cases (the slice with `wantChain` and `wantAnalyzers`), add:

```go
		{map[string]string{"FEATURED_IMAGE_CHAIN": "photo,codex,gemini"}, []string{"photo", "codex", "gemini"}, []string{"codex", "gemini"}},
```

and in the list of bad environments in the same test, add:

```go
		{"FEATURED_IMAGE_CHAIN": "photo"},
```

Create `apps/server-go/internal/illustration/wiring_test.go`:

```go
package illustration_test

import (
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

func TestBuildPipelineDepsAddsThePhotoStepOnlyWithAFinder(t *testing.T) {
	recent := &fakeRecentPhotos{}
	with := illustration.BuildPipelineDeps(illustration.PipelineConfig{
		Chain: []string{"photo", "source"}, Photos: &fakePhotoFinder{result: realphoto.Result{}}, RecentPhotos: recent,
	})
	if len(with.Chain) != 2 || with.Chain[0].Name != "photo" || with.Chain[0].Photos == nil || with.RecentPhotos == nil {
		t.Fatalf("chain = %+v", with.Chain)
	}
	without := illustration.BuildPipelineDeps(illustration.PipelineConfig{Chain: []string{"photo", "source"}})
	if len(without.Chain) != 1 || without.Chain[0].Name != "source" {
		t.Fatalf("chain without a finder = %+v, want only source", without.Chain)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -p 2 ./internal/config/... ./internal/illustration/...`
Expected: the config test FAILS with `unknown provider "photo"`; the illustration package FAILS to compile with `unknown field Photos in struct literal`.

- [ ] **Step 3: Accept `photo` in the configuration**

In `apps/server-go/internal/config/config.go`, add the name and replace `ParseImageChain`:

```go
const (
	ImageProviderCodex  = "codex"
	ImageProviderGemini = "gemini"
	ImageProviderSource = "source"
	ImageProviderPhoto  = "photo"
)
```

```go
// ParseImageChain parses FEATURED_IMAGE_CHAIN: a comma-separated, non-empty
// list of photo, codex, gemini and source without repeats. photo (a real,
// credited photo) often finds nothing, so it must not be the only provider.
func ParseImageChain(value string) ([]string, error) {
	chain, err := parseProviderList("FEATURED_IMAGE_CHAIN", value, []string{ImageProviderPhoto, ImageProviderCodex, ImageProviderGemini, ImageProviderSource})
	if err != nil {
		return nil, err
	}
	if len(chain) == 0 {
		return nil, errors.New("FEATURED_IMAGE_CHAIN must name at least one of photo, codex, gemini, source")
	}
	if len(chain) == 1 && chain[0] == ImageProviderPhoto {
		return nil, errors.New("FEATURED_IMAGE_CHAIN: photo needs another provider to fall back on (codex, gemini or source)")
	}
	return chain, nil
}
```

Also update the comment on the `FeaturedImageChain` field so it reads:

```go
	// FeaturedImageChain is FEATURED_IMAGE_CHAIN, the providers tried in
	// order: photo, codex, gemini and source, e.g. "photo,codex,gemini".
```

- [ ] **Step 4: Build the step from configuration**

In `apps/server-go/internal/illustration/wiring.go`, add two fields to `PipelineConfig`, after `History`:

```go
	// Photos finds real photos for the "photo" step; nil drops the step.
	Photos       PhotoFinder
	RecentPhotos RecentPhotos
```

In `BuildPipelineDeps`, add `RecentPhotos: cfg.RecentPhotos,` to the `deps` literal (after `History: cfg.History,`), update the comment on `Chain` in `PipelineConfig` to `// photo, codex, gemini, source`, and add a case to the chain switch:

```go
		case ProviderPhoto:
			if cfg.Photos != nil {
				deps.Chain = append(deps.Chain, PhotoStep(cfg.Photos))
			}
```

- [ ] **Step 5: Wire it in the application**

In `apps/server-go/internal/app/app.go`, in the `if cfg.PublisherEnabled {` block, add these two lines directly before `illustrator := illustration.NewPipeline(`:

```go
		// One finder serves the featured "photo" step and the in-article image.
		photos := realphoto.NewFinder(geminiClient, geminiClient, illustration.NewReferenceClient(), logger).WithSearch(geminiClient)
		publishedArticles := publisher.NewSQLiteArticles(db, now)
```

Add two fields to the `illustration.PipelineConfig{...}` literal, after `History:`:

```go
			Photos:       photos,
			RecentPhotos: publishedArticles,
```

In the `publisher.New(publisher.Deps{...})` literal, replace:

```go
			Articles:    publisher.NewSQLiteArticles(db, now),
```

with:

```go
			Articles:    publishedArticles,
```

In the `if cfg.InlineImagesEnabled {` block, delete the line that builds its own finder:

```go
			photos := realphoto.NewFinder(geminiClient, geminiClient, illustration.NewReferenceClient(), logger).WithSearch(geminiClient)
```

The block keeps using `photos`, which is now the one declared above.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go build ./... && go test -p 2 ./internal/config/... ./internal/illustration/... ./internal/app/...`
Expected: the build succeeds and the tests PASS.

- [ ] **Step 7: Commit**

```bash
git add apps/server-go/internal/config apps/server-go/internal/illustration apps/server-go/internal/app
git commit -m "Accept photo in FEATURED_IMAGE_CHAIN and wire the photo finder into the featured-image pipeline"
```

---

### Task 9: Return the credit from the article API

**Files:**
- Modify: `apps/server-go/internal/content/model.go`
- Modify: `apps/server-go/internal/content/sqlite.go`
- Modify: `apps/server-go/contracts/openapi.yaml`
- Test: `apps/server-go/internal/content/sqlite_test.go`

- [ ] **Step 1: Write the failing test**

Add to `apps/server-go/internal/content/sqlite_test.go`:

```go
func TestSQLiteStoreReturnsTheFeaturedImageCreditWithArticleBySlug(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	seed(t, db)
	if _, err := db.Exec(`INSERT INTO article_featured_image_credits
		(article_id, kind, credit, credit_url, license, license_url, photo_url, alt, created_at)
		VALUES (301, 'commons', 'Photo: Jane Doe / CC BY 4.0, via Wikimedia Commons', 'https://commons.wikimedia.org/wiki/File:X.jpg',
		'CC BY 4.0', 'https://creativecommons.org/licenses/by/4.0', 'https://upload.wikimedia.org/x.jpg', 'Bret Taylor on stage.', '2026-09-19 12:00:00')`); err != nil {
		t.Fatal(err)
	}
	store := content.NewSQLiteStore(db)
	with, err := store.PublishedBySlug(context.Background(), "synthetic-published-newer")
	if err != nil {
		t.Fatal(err)
	}
	want := content.FeaturedImageCredit{
		Credit: "Photo: Jane Doe / CC BY 4.0, via Wikimedia Commons", CreditURL: "https://commons.wikimedia.org/wiki/File:X.jpg",
		License: "CC BY 4.0", LicenseURL: "https://creativecommons.org/licenses/by/4.0", Alt: "Bret Taylor on stage.",
	}
	if with.FeaturedImageCredit == nil || *with.FeaturedImageCredit != want {
		t.Fatalf("credit = %+v", with.FeaturedImageCredit)
	}
	without, err := store.PublishedBySlug(context.Background(), "synthetic-published-null-options")
	if err != nil {
		t.Fatal(err)
	}
	if without.FeaturedImageCredit != nil {
		t.Fatalf("credit without a row = %+v", without.FeaturedImageCredit)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -p 2 ./internal/content/ -run TestSQLiteStoreReturnsTheFeaturedImageCredit -v`
Expected: FAIL to compile with `undefined: content.FeaturedImageCredit`.

- [ ] **Step 3: Add the model**

In `apps/server-go/internal/content/model.go`, add a field to `Article`, directly after `InlineImages`:

```go
	// FeaturedImageCredit is set when the featured image is a real photo;
	// loaded only for a single article read by slug.
	FeaturedImageCredit *FeaturedImageCredit `json:"featuredImageCredit,omitempty"`
```

and add the type below `InlineImage`:

```go
// FeaturedImageCredit credits a featured image that is a real photo. Credit
// links to CreditURL (the Commons file page or the maker's page); License and
// LicenseURL are for Commons photos; Alt says what the photo shows. The
// photo's own source URL is never returned.
type FeaturedImageCredit struct {
	Credit     string `json:"credit"`
	CreditURL  string `json:"creditUrl,omitempty"`
	License    string `json:"license,omitempty"`
	LicenseURL string `json:"licenseUrl,omitempty"`
	Alt        string `json:"alt,omitempty"`
}
```

- [ ] **Step 4: Load it with the article**

In `apps/server-go/internal/content/sqlite.go`, in `PublishedBySlug`, directly after the block:

```go
	if err = loadInlineImages(ctx, tx, &article); err != nil {
```

and its closing brace (the block returns the error), add:

```go
	if err = loadFeaturedImageCredit(ctx, tx, &article); err != nil {
		return Article{}, err
	}
```

Match the return statement to the one the `loadInlineImages` block uses. Then add below `loadInlineImages`:

```go
// loadFeaturedImageCredit reads the credit of a featured image that is a
// real photo; a generated illustration has none.
func loadFeaturedImageCredit(ctx context.Context, tx *sql.Tx, article *Article) error {
	var credit FeaturedImageCredit
	err := tx.QueryRowContext(ctx, `SELECT credit, credit_url, license, license_url, alt
		FROM article_featured_image_credits WHERE article_id = ?`, article.ID).
		Scan(&credit.Credit, &credit.CreditURL, &credit.License, &credit.LicenseURL, &credit.Alt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read featured image credit: %w", err)
	}
	article.FeaturedImageCredit = &credit
	return nil
}
```

- [ ] **Step 5: Document it in the contract**

In `apps/server-go/contracts/openapi.yaml`, in the article schema, directly after the `inlineImages` property (which ends with `licenseUrl: {type: string}`) and before `created_at`, add at the same indentation as `inlineImages`:

```yaml
        # Public single-article reads only, when the featured image is a real
        # photo. credit links to creditUrl (the Wikimedia Commons file page or
        # the maker's page); license and licenseUrl are for Commons photos;
        # alt says what the photo shows. A generated illustration has none.
        featuredImageCredit:
          type: object
          required: [credit]
          properties:
            credit: {type: string}
            creditUrl: {type: string}
            license: {type: string}
            licenseUrl: {type: string}
            alt: {type: string}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test -p 2 ./internal/content/... ./internal/contracttest/... ./internal/httpserver/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add apps/server-go/internal/content apps/server-go/contracts/openapi.yaml
git commit -m "Return a featured photo's credit with the article"
```

---

### Task 10: The in-article image never repeats the featured photo

**Files:**
- Modify: `apps/server-go/internal/inlineimage/store.go`
- Modify: `apps/server-go/internal/inlineimage/worker.go`
- Test: `apps/server-go/internal/inlineimage/worker_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `apps/server-go/internal/inlineimage/worker_test.go`:

```go
func TestWorkerExcludesTheFeaturedPhotoFromTheSearch(t *testing.T) {
	article := photoArticle()
	article.FeaturedPhotoURL = "https://upload.wikimedia.org/x.jpg"
	store := &fakeStore{articles: []inlineimage.Article{article}}
	finder := &fakeFinder{result: realphoto.Result{Found: true, Image: []byte("photo"), Alt: "Another view.", Credit: photoCredit}}
	if _, err := newWorker(store, &fakeIllustrator{}).WithPhotos(finder, &fakePhotoStore{}).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(finder.requests) != 1 || len(finder.requests[0].Exclude) != 1 || finder.requests[0].Exclude[0] != "https://upload.wikimedia.org/x.jpg" {
		t.Fatalf("finder requests = %+v", finder.requests)
	}
}

// An inline illustration copies the style of the featured illustration. A
// featured photo has no drawn style, so without a second photo the article
// gets no inline image.
func TestWorkerDoesNotDrawWhenTheFeaturedImageIsAPhoto(t *testing.T) {
	article := photoArticle()
	article.FeaturedPhotoURL = "https://upload.wikimedia.org/x.jpg"
	store := &fakeStore{articles: []inlineimage.Article{article}}
	illustrator := &fakeIllustrator{}
	finder := &fakeFinder{result: realphoto.Result{Reason: "no usable photo"}}
	if _, err := newWorker(store, illustrator).WithPhotos(finder, &fakePhotoStore{}).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(illustrator.requests) != 0 {
		t.Fatal("drew an illustration although the featured image is a photo")
	}
	if len(store.marks) != 1 || store.marks[0].ready || store.marks[0].attempts != inlineimage.MaxAttempts {
		t.Fatalf("marks = %+v, want one final failure", store.marks)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -p 2 ./internal/inlineimage/...`
Expected: FAIL to compile with `article.FeaturedPhotoURL undefined`.

- [ ] **Step 3: Read the featured photo's URL with the article**

In `apps/server-go/internal/inlineimage/store.go`, add a field to `Article`, after `SourceURL`:

```go
	// FeaturedPhotoURL is the file the featured image was copied from when
	// it is a real photo; empty for a drawn illustration.
	FeaturedPhotoURL string
```

Replace the block that runs from the `articleColumns` constant through the `scanArticle` function (the `scanner` interface sits between them and stays as it is):

```go
// articleColumns are the columns scanArticle reads, for articles a.
const articleColumns = `a.id, a.slug, a.title, a.content, COALESCE(a.featured_image, ''), COALESCE(a.source_url, ''),
	COALESCE((SELECT c.source_image_url FROM candidates c WHERE c.article_id = a.id ORDER BY c.id LIMIT 1), ''),
	COALESCE((SELECT i.attempts FROM article_images i WHERE i.article_id = a.id), 0),
	COALESCE((SELECT f.photo_url FROM article_featured_image_credits f WHERE f.article_id = a.id), '')`

type scanner interface{ Scan(dest ...any) error }

func scanArticle(row scanner) (Article, error) {
	var article Article
	err := row.Scan(&article.ID, &article.Slug, &article.Title, &article.Content, &article.FeaturedImage, &article.SourceURL, &article.SourceImage, &article.Attempts, &article.FeaturedPhotoURL)
	return article, err
}
```

- [ ] **Step 4: Exclude it, and do not draw over a photo**

In `apps/server-go/internal/inlineimage/worker.go`, replace `PhotoRequest`:

```go
// PhotoRequest is what the photo finder is told about an article. The
// source URL is only read, never credited. A featured image that is a real
// photo is excluded, so the body never shows the same picture twice.
func PhotoRequest(article Article) realphoto.Request {
	request := realphoto.Request{Slug: article.Slug, Title: article.Title, Text: strings.Join(Paragraphs(article.Content), "\n\n"), SourceURL: article.SourceURL}
	if article.FeaturedPhotoURL != "" {
		request.Exclude = []string{article.FeaturedPhotoURL}
	}
	return request
}
```

In `illustrate`, directly after:

```go
	if done, err := w.tryPhoto(ctx, logger, article, photoSlot); done || err != nil {
		return err
	}
```

add:

```go
	if article.FeaturedPhotoURL != "" {
		// An inline illustration is drawn in the style of the featured
		// illustration; a featured photo has no drawn style to copy.
		logger.InfoContext(ctx, "inline image skipped", "reason", "the featured image is a photo and no second photo was found")
		return w.fail(ctx, article, "the featured image is a photo and no second real photo was found", MaxAttempts)
	}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test -p 2 ./internal/inlineimage/... ./cmd/imagegen-try/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add apps/server-go/internal/inlineimage
git commit -m "Keep the in-article image from repeating a featured photo, and skip the drawn in-article image when the featured image is a photo"
```

---

### Task 11: Show the credit on the article page

**Files:**
- Modify: `apps/web/app/data/articles.ts`
- Modify: `apps/web/app/lib/inline-images.ts`
- Modify: `apps/web/app/lib/api.ts`
- Modify: `apps/web/app/article/[slug]/page.tsx`
- Test: `apps/web/app/lib/inline-images.test.ts`

- [ ] **Step 1: Write the failing tests**

In `apps/web/app/lib/inline-images.test.ts`, change the import from `./inline-images` to also import `featuredCaption` and `usableImageCredit`, and add:

```ts
test("a featured photo's credit is kept only when it has text and safe links", () => {
  assert.equal(usableImageCredit(undefined), undefined);
  assert.equal(usableImageCredit({ credit: "  " }), undefined);
  assert.deepEqual(
    usableImageCredit({
      credit: " Photo: Jane Doe / CC BY 4.0, via Wikimedia Commons ",
      creditUrl: "https://commons.wikimedia.org/wiki/File:X.jpg",
      license: "CC BY 4.0",
      licenseUrl: "javascript:alert(1)",
      alt: " Bret Taylor on stage. ",
    }),
    {
      credit: "Photo: Jane Doe / CC BY 4.0, via Wikimedia Commons",
      creditUrl: "https://commons.wikimedia.org/wiki/File:X.jpg",
      license: "CC BY 4.0",
      alt: "Bret Taylor on stage.",
    },
  );
});

test("the featured caption links the credit and the licence, and is absent for an illustration", () => {
  assert.equal(featuredCaption(undefined), undefined);
  const html = featuredCaption({
    credit: "Photo: Jane Doe / CC BY 4.0, via Wikimedia Commons",
    creditUrl: "https://commons.wikimedia.org/wiki/File:X.jpg",
    license: "CC BY 4.0",
    licenseUrl: "https://creativecommons.org/licenses/by/4.0",
  });
  assert.ok(html);
  assert.match(html, /href="https:\/\/commons\.wikimedia\.org\/wiki\/File:X\.jpg"[^>]*>Photo: Jane Doe<\/a> \/ <a href="https:\/\/creativecommons\.org\/licenses\/by\/4\.0"/);
  assert.ok(html.endsWith(", via Wikimedia Commons"));
  assert.equal(featuredCaption({ credit: "Image: <Nvidia>" }), "Image: &lt;Nvidia&gt;");
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `pnpm test`
Expected: FAIL: `./inline-images` has no exported member `featuredCaption` or `usableImageCredit`.

- [ ] **Step 3: Add the type**

In `apps/web/app/data/articles.ts`, add below the `InlineImage` type:

```ts
// The credit of a featured image that is a real photo: credit links to
// creditUrl, a Commons photo also has its licence, and alt says what the
// photo shows. A generated illustration has none.
export type ImageCredit = {
  credit: string;
  creditUrl?: string;
  license?: string;
  licenseUrl?: string;
  alt?: string;
};
```

and add to the `Article` type, directly after `image: string;`:

```ts
  imageCredit?: ImageCredit;
```

- [ ] **Step 4: Add the helpers**

In `apps/web/app/lib/inline-images.ts`, change the first line to:

```ts
import type { ImageCredit, InlineImage } from "../data/articles";
```

and add at the end of the file:

```ts
// The credit of a featured image that is a real photo, from the API. It is
// undefined for a generated illustration or a malformed value; links are
// kept only when they are http(s) URLs.
export function usableImageCredit(raw: unknown): ImageCredit | undefined {
  if (!raw || typeof raw !== "object") return undefined;
  const { credit, creditUrl, license, licenseUrl, alt } = raw as Record<string, unknown>;
  if (!nonEmpty(credit)) return undefined;
  const usable: ImageCredit = { credit: credit.trim() };
  if (isHttpUrl(creditUrl)) usable.creditUrl = creditUrl;
  if (nonEmpty(license)) usable.license = license.trim();
  if (nonEmpty(license) && isHttpUrl(licenseUrl)) usable.licenseUrl = licenseUrl;
  if (nonEmpty(alt)) usable.alt = alt.trim();
  return usable;
}

// The featured image's credit line as HTML, or undefined when the image is a
// generated illustration, which carries no credit.
export function featuredCaption(credit: ImageCredit | undefined): string | undefined {
  if (!credit) return undefined;
  return inlineCaption({ url: "", alt: "", afterParagraph: 1, ...credit });
}
```

- [ ] **Step 5: Map the API field**

In `apps/web/app/lib/api.ts`, add to the `ApiArticle` interface, after `inlineImages`:

```ts
  featuredImageCredit?: unknown;
```

In the function that maps an `ApiArticle` to an `Article`, add directly after the line `image: ownImageOr(a.featured_image),`:

```ts
    imageCredit: usableImageCredit(a.featuredImageCredit),
```

and add `usableImageCredit` to the existing import from `./inline-images`.

- [ ] **Step 6: Render the credit**

In `apps/web/app/article/[slug]/page.tsx`, change the import:

```ts
import { featuredCaption, insertInlineImages } from "../../lib/inline-images";
```

In the page component, directly after the line:

```ts
  const body = insertInlineImages(topicLinked.html, article.inlineImages);
```

add:

```ts
  const imageCaption = featuredCaption(article.imageCredit);
```

Replace the hero block:

```tsx
      <div className="relative w-full h-[300px] md:h-[450px] lg:h-[500px]">
        <Image src={article.image} alt={article.headline} fill className="object-cover" sizes="100vw" priority />
        <div className="absolute inset-0 bg-gradient-to-t from-bg via-bg/40 to-transparent" />
      </div>
```

with:

```tsx
      <div className="relative w-full h-[300px] md:h-[450px] lg:h-[500px]">
        <Image src={article.image} alt={article.imageCredit?.alt || article.headline} fill className="object-cover" sizes="100vw" priority />
        <div className="absolute inset-0 bg-gradient-to-t from-bg via-bg/40 to-transparent" />
        {imageCaption && (
          <p
            className="absolute top-3 right-4 lg:right-8 z-20 max-w-[70%] text-right text-[11px] leading-snug text-white/75 [text-shadow:0_1px_2px_rgba(0,0,0,0.85)]"
            dangerouslySetInnerHTML={{ __html: imageCaption }}
          />
        )}
      </div>
```

The credit sits at the top right of the image because the headline block overlaps the bottom of it.

- [ ] **Step 7: Run the tests and the type check**

Run: `pnpm test && pnpm exec tsc --noEmit`
Expected: the tests PASS and the type check prints nothing.

- [ ] **Step 8: Check it in the browser**

Start the site with `pnpm dev`. Open any article and confirm it looks as before, since no published article has a credit yet. Then, to see the credit, temporarily hard-code `const imageCaption = "Photo: Jane Doe / CC BY 4.0, via Wikimedia Commons";` in the page, check the article at a phone width (375 px) and a desktop width, confirm the credit is readable and does not cover the headline or the section tag, and remove the hard-coded line again.

- [ ] **Step 9: Commit**

```bash
git add apps/web/app
git commit -m "Show a featured photo's credit on the article page and use its description as alt text"
```

---

### Task 12: The dry-run report

**Files:**
- Create: `apps/server-go/cmd/photo-try/main.go`
- Create: `apps/server-go/cmd/photo-try/report.go`
- Test: `apps/server-go/cmd/photo-try/report_test.go`

- [ ] **Step 1: Write the failing test**

Create `apps/server-go/cmd/photo-try/report_test.go`:

```go
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

func TestRenderReportShowsChosenAndRejectedPhotos(t *testing.T) {
	rows := []reportRow{
		{
			Slug: "sierra-raises", Title: "Sierra raises <$1.75 billion>", Current: "https://img.test/features/sierra.webp",
			Found: true, Image: "sierra-raises.png", Credit: "Photo: TechCrunch / CC BY 2.0, via Wikimedia Commons",
			CreditURL: "https://commons.wikimedia.org/wiki/File:Bret.jpg", StandIn: "Bret Taylor", Quality: 9,
			Reason: "Wikimedia Commons photo of stand-in Bret Taylor (person), score 9",
			Candidates: []realphoto.Candidate{
				{Source: "commons", PageURL: "https://commons.wikimedia.org/wiki/File:Bret.jpg", Usable: true, Verdict: &realphoto.Verdict{Quality: 9}},
				{Source: "commons", PageURL: "https://commons.wikimedia.org/wiki/File:Old.jpg", Reason: "visibly dated"},
			},
		},
		{Slug: "a-study", Title: "A study", Current: "https://img.test/features/study.webp", Reason: "no photographable subject: a study"},
	}
	var out bytes.Buffer
	if err := renderReport(&out, rows); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{
		"1 of 2 articles got a photo",
		"Sierra raises &lt;$1.75 billion&gt;",
		`src="sierra-raises.png"`,
		"Photo: TechCrunch / CC BY 2.0, via Wikimedia Commons",
		"Stand-in: Bret Taylor",
		"visibly dated",
		"no photographable subject: a study",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("report lacks %q", want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -p 2 ./cmd/photo-try/...`
Expected: FAIL to compile with `undefined: reportRow` and `undefined: renderReport`.

- [ ] **Step 3: Write the report renderer**

Create `apps/server-go/cmd/photo-try/report.go`:

```go
package main

import (
	"html/template"
	"io"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

// reportRow is one article of the report.
type reportRow struct {
	Slug, Title string
	// Current is the article's published featured image.
	Current string
	Found   bool
	// Image is the file name of the chosen photo, cropped, in the report folder.
	Image      string
	Credit     string
	CreditURL  string
	StandIn    string
	Quality    int
	Reason     string
	Candidates []realphoto.Candidate
}

var reportTemplate = template.Must(template.New("report").Funcs(template.FuncMap{
	"found": func(rows []reportRow) int {
		count := 0
		for _, row := range rows {
			if row.Found {
				count++
			}
		}
		return count
	},
}).Parse(`<!doctype html>
<meta charset="utf-8">
<title>Featured photo dry run</title>
<style>
body{font:14px/1.45 system-ui,sans-serif;background:#1a1a1a;color:#eee;margin:24px}
h1{font-size:20px} h2{font-size:16px;margin:28px 0 8px}
.pair{display:grid;grid-template-columns:1fr 1fr;gap:12px;max-width:1100px}
.pair img{width:100%;aspect-ratio:16/9;object-fit:cover;background:#333;display:block}
.label{font-size:11px;color:#999;text-transform:uppercase;letter-spacing:.08em;margin-bottom:4px}
.none{aspect-ratio:16/9;display:flex;align-items:center;justify-content:center;background:#262626;color:#999;padding:12px;text-align:center}
.meta{color:#bbb;margin:6px 0}
table{border-collapse:collapse;max-width:1100px;width:100%;font-size:12px;margin-top:6px}
td{border-top:1px solid #333;padding:4px 8px 4px 0;vertical-align:top;color:#bbb;word-break:break-all}
a{color:#c4a1ff}
</style>
<h1>{{found .}} of {{len .}} articles got a photo</h1>
{{range .}}
<h2>{{.Title}}</h2>
<div class="pair">
  <div><div class="label">Current</div><img src="{{.Current}}" alt=""></div>
  <div><div class="label">Real photo</div>
  {{if .Found}}<img src="{{.Image}}" alt="">{{else}}<div class="none">{{.Reason}}</div>{{end}}</div>
</div>
{{if .Found}}<p class="meta"><a href="{{.CreditURL}}">{{.Credit}}</a> · quality {{.Quality}}{{if .StandIn}} · Stand-in: {{.StandIn}}{{end}}</p>{{end}}
{{if .Candidates}}<table>
{{range .Candidates}}<tr><td>{{.Source}}</td><td><a href="{{.PageURL}}">{{.PageURL}}</a></td><td>{{if .Usable}}usable{{if .Verdict}}, quality {{.Verdict.Quality}}{{end}}{{else}}{{.Reason}}{{end}}</td></tr>
{{end}}</table>{{end}}
{{end}}
`))

// renderReport writes the dry-run report as one HTML page.
func renderReport(w io.Writer, rows []reportRow) error {
	return reportTemplate.Execute(w, rows)
}
```

- [ ] **Step 4: Write the command**

Create `apps/server-go/cmd/photo-try/main.go`:

```go
// Command photo-try shows which real photo the featured-image "photo" step
// would choose for published articles, as a dry run. It opens the database
// read-only, uploads nothing and writes nothing but a local report folder:
// report.html, and each chosen photo cropped to 16:9.
//
//	go run ./cmd/photo-try -db ~/copy-of-technews.db -latest 30 -out ./photo-try
//	go run ./cmd/photo-try -db ~/copy-of-technews.db -slugs a-slug,another-slug
//
// The database copy must have migration 17. It reads GEMINI_API_KEY,
// GEMINI_TEXT_MODEL and GEMINI_VISION_MODEL from the environment. A photo
// chosen for one article is not offered to the next, as in production.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/inlineimage"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "photo-try: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string) error {
	flags := flag.NewFlagSet("photo-try", flag.ContinueOnError)
	dbPath := flags.String("db", getenv("DATABASE_PATH"), "SQLite database, opened read-only")
	latest := flags.Int("latest", 30, "how many of the newest published articles to try")
	slugs := flags.String("slugs", "", "comma-separated slugs to try instead of -latest")
	outDir := flags.String("out", "photo-try", "folder for report.html and the chosen photos")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" {
		return errors.New("needs -db (or DATABASE_PATH)")
	}
	db, err := database.OpenExisting(ctx, *dbPath, true)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	store := inlineimage.NewSQLiteStore(db, nil)

	var articles []inlineimage.Article
	if *slugs != "" {
		for _, slug := range strings.Split(*slugs, ",") {
			if slug = strings.TrimSpace(slug); slug == "" {
				continue
			}
			article, err := store.BySlug(ctx, slug)
			if err != nil {
				return fmt.Errorf("%s: %w", slug, err)
			}
			articles = append(articles, article)
		}
	} else if articles, err = store.Latest(ctx, *latest); err != nil {
		return err
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	geminiClient := gemini.New(getenv("GEMINI_API_KEY"), getenv("GEMINI_TEXT_MODEL"), gemini.WithVisionModel(getenv("GEMINI_VISION_MODEL")))
	finder := realphoto.NewFinder(geminiClient, geminiClient, illustration.NewReferenceClient(), logger).WithSearch(geminiClient)

	var rows []reportRow
	var used []string
	for _, article := range articles {
		request := inlineimage.PhotoRequest(article)
		request.Slot = realphoto.SlotFeatured
		request.Exclude = used
		found := finder.Find(ctx, request)
		row := reportRow{Slug: article.Slug, Title: article.Title, Current: article.FeaturedImage, Reason: found.Reason, Candidates: found.Candidates}
		if found.Found && found.Credit != nil && found.Chosen != nil {
			focusX, focusY := 0.5, 0.5
			if verdict := found.Chosen.Verdict; verdict != nil {
				focusX, focusY, row.Quality = verdict.FocusX, verdict.FocusY, verdict.Quality
			}
			cropped, err := imaging.CropToAspect(found.Image, 16.0/9.0, focusX, focusY)
			if err != nil {
				row.Reason = "crop: " + err.Error()
			} else {
				row.Image = article.Slug + ".png"
				if err := os.WriteFile(filepath.Join(*outDir, row.Image), cropped, 0o644); err != nil {
					return err
				}
				row.Found, row.Credit, row.CreditURL = true, found.Credit.Text, found.Credit.URL
				if found.StandIn != nil {
					row.StandIn = found.StandIn.Subject
				}
				used = append(used, found.Chosen.ImageURL)
			}
		}
		outcome := "drawn"
		if row.Found {
			outcome = "photo"
		}
		fmt.Printf("%-6s %-60s %s\n", outcome, article.Slug, row.Reason)
		rows = append(rows, row)
		if ctx.Err() != nil {
			break
		}
	}

	reportPath := filepath.Join(*outDir, "report.html")
	file, err := os.Create(reportPath)
	if err != nil {
		return err
	}
	if err := renderReport(file, rows); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	fmt.Printf("\nreport: %s\n", reportPath)
	return nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go vet ./cmd/photo-try/... && go test -p 2 ./cmd/photo-try/...`
Expected: no vet output and the test PASSES.

- [ ] **Step 6: Commit**

```bash
git add apps/server-go/cmd/photo-try
git commit -m "Add photo-try, a dry run that reports which real photo each recent article would get as its featured image"
```

---

### Task 13: Policy, configuration docs and the final check

**Files:**
- Modify: `NEWS_PUBLISHING_POLICY.md`
- Modify: `apps/server-go/docs/technews.env.example`

- [ ] **Step 1: Update the publishing policy**

In `NEWS_PUBLISHING_POLICY.md`, in the `## Images` section:

Replace rule 2 with:

```markdown
2. `FEATURED_IMAGE_CHAIN` lists the providers tried in order: `photo` (a real, credited photo: a freely licensed Wikimedia Commons photo, or the maker's own official image), `codex` (Codex CLI image generation on the ChatGPT plan), `gemini` (`GEMINI_IMAGE_MODEL`) and `source` (a copy of the source's own image). `FEATURED_IMAGE_SOURCE=generate|source` is the legacy spelling of a one-provider chain. A provider that fails, or whose images all fail review, hands over to the next one. `photo` may not be the only provider.
```

At the start of rule 4, before its first sentence, add:

```markdown
Rules 4 to 6 apply to generated images only.
```

In rule 7, the last sentence stays as it is: "Faces are never re-created by an AI model."

After rule 12, add:

```markdown
13. Real photos. The `photo` provider may use a photo only if it shows the article's subject, or a stand-in for a story with nothing physical to show: the one clear leader of the company the story is about, the headquarters of the company, regulator or court it is about, or the software product on a device. A stand-in is found on Wikimedia Commons only. Every photo passes the vision check for the featured slot: the exact subject, a real photograph, no watermark, not text-heavy, not a marketing banner, not misleading, the subject prominent, not visibly dated, quality 8 or more. A photo of a real person is allowed; a generated likeness is not. The photo is cropped to 16:9, stored in our bucket like any featured image, and credited on the article page with a link to its Commons page or the maker's page and, for Commons, its licence. A photo is never taken from a news source, and the same photo is not used as a featured image twice within 30 days.
```

- [ ] **Step 2: Update the environment example**

In `apps/server-go/docs/technews.env.example`, replace the line:

```
# Example: FEATURED_IMAGE_CHAIN=codex,gemini,source
```

with:

```
# photo looks for a real, credited photo first (Wikimedia Commons, then the
# maker's own image) and hands over to the next provider when none passes;
# it may not be the only provider. Review a cmd/photo-try report before
# adding it.
# Example: FEATURED_IMAGE_CHAIN=photo,codex,gemini
```

Also extend the comment above it that lists the providers so that it names `photo` first.

- [ ] **Step 3: Run everything**

Run from `apps/server-go`: `gofmt -l . && go build ./... && go vet ./... && go test -p 2 ./...`
Expected: `gofmt`, the build and vet print nothing (run `gofmt -w` on any file `gofmt -l` names) and every package PASSES.

Run from `apps/web`: `pnpm test && pnpm exec tsc --noEmit`
Expected: PASS and no type errors.

- [ ] **Step 4: Commit**

```bash
git add NEWS_PUBLISHING_POLICY.md apps/server-go/docs/technews.env.example
git commit -m "Allow real, credited photos as featured images in the publishing policy and document the photo provider"
```

- [ ] **Step 5: Produce the dry-run report for the owner**

This step needs a copy of the production database and the Gemini key, so the owner runs it or provides both. Never run it against the live database file; copy it first.

After deploying the migration (the server applies migration 17 on start), copy the database and run:

```bash
go run ./cmd/photo-try -db ~/copy-of-technews.db -latest 30 -out ./photo-try
```

Expected: one line per article marked `photo` or `drawn`, then `report: photo-try/report.html`. Open the report. It shows, for each of the 30 articles, the current image beside the chosen photo, the credit, and every rejected candidate with its reason.

- [ ] **Step 6: Hand over to the owner**

The step goes live only after the owner has reviewed the report. Enabling it is the owner's change on the server: set `FEATURED_IMAGE_CHAIN=photo,codex,gemini` in the environment file and restart the API. From then on the log line `featured image ready` carries `provider=photo` or the generating provider's name, which gives the share of stories that still fall through to a drawing.

---

## Spec coverage

| Spec section | Task |
|---|---|
| The photo step: new provider, exact subject first, crop, stored on S3, fallback to a drawing, no analyzer call, skipped for drawn-only runs | 7, 8 |
| Stand-in subjects: three kinds, at most three, Commons only | 4 |
| Checks: quality 8, exact match, dated, event branding for a person, focus point | 2, 1 |
| No repeats within 30 days | 3, 5, 7 |
| Rate limits | 3 |
| Credits: new table, written with the article, returned by the API, shown on the article page | 5, 6, 9, 11 |
| In-article image never picks the featured photo | 10 |
| Policy changes | 13 |
| Dry-run command and the review before going live | 12, 13 |
| Testing | each task |
