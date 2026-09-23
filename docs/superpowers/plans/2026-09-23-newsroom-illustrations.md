# Newsroom Illustrations and Publisher Wiring Implementation Plan (Phase 4b)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the Go publisher a real `Illustrator` — an original Gemini illustration (source image as reference), checked by a vision model, encoded as WebP and stored in S3 — then wire the publisher into the API behind `PUBLISHER_ENABLED` so a queued candidate is published end to end locally.

**Architecture:** `internal/gemini` gains image generation and image review calls. New `internal/imaging` (decode/resize/JPEG/WebP/sniff), `internal/illustration` (prompts, reference fetch, compliance verdict, generation loop — a port of the `s3-feature-images` branch), `internal/media` (S3 feature-image storage with public verification). `internal/illustration.S3Illustrator` implements `publisher.Illustrator`. `internal/config` + `internal/app` wire the publisher loop.

**Phase 4a changes to build on:** `publisher.ClassifyGeminiError`, `SystemFault`/`Permanent` error classes; the article insert marks the candidate published in the same transaction (`NewArticle.CandidateID`); `ResetProcessing(ctx, now, maxAttempts)`; collector `StatusError`/`ErrBodyTooLarge`; `gemini.ErrBlocked`.

**Tech Stack:** Go 1.25; new modules `github.com/aws/aws-sdk-go-v2` (`config`, `service/s3`), `github.com/gen2brain/webp` (pure Go, WASM via wazero — no cgo, keeps `modernc.org/sqlite` build), `golang.org/x/image` (`draw`, `webp` decoding).

**Node reference (branch `s3-feature-images`, read with `git show 's3-feature-images:<path>'`):**
- `apps/server/src/feature-image-publication.ts` — `ILLUSTRATION_STYLE_RULES` (:47-66), `buildIllustrationPrompt` (:71-97)
- `apps/server/src/feature-illustration.ts` — constants and correction lines (:66-90), `fetchReferenceImage` (:134-178), `normalizeReferenceImage` (:188-230), `requestIllustration` (:265-322), `COMPLIANCE_RESPONSE_SCHEMA` + `buildCompliancePrompt` (:338-369), `checkIllustrationCompliance` (:390-470), `correctionForVerdict` + `generateIllustration` (:473-581)
- `apps/server/src/feature-image-storage.ts` — config (:97-123), `sanitizeFeatureImageSlug`, `sniffImageMimeType`, `buildFeatureImageKey`, `encodeFeatureImageAsWebp` (quality 82), `verifyPublicUrl`, `storeFeatureImage`, delete guard

**Rule for prompt text:** every prompt/correction string is a **verbatim** port. After writing each, diff it against the branch source and fix any difference.

**Working directory:** `apps/server-go`. **Commits:** plain sentences, no prefixes, no co-author trailers. **Safety:** unit tests use httptest/fakes only; the live check (Task 7) uses a dev DB and a **dev S3 prefix** and is run only with explicit credentials in the environment.

---

### Task 1: Gemini image generation and image review

**Files:** Modify `internal/gemini/client.go`, `internal/gemini/client_test.go`

- [ ] **Step 1: Write failing tests** (append)

```go
func TestGenerateImageSendsReferenceAndParsesInlineData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/image-model:generateContent" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		contents := body["contents"].([]any)[0].(map[string]any)
		parts := contents["parts"].([]any)
		config := body["generationConfig"].(map[string]any)
		imageConfig := config["imageConfig"].(map[string]any)
		if contents["role"] != "user" || len(parts) != 2 || imageConfig["aspectRatio"] != "16:9" || imageConfig["imageSize"] != "2K" {
			t.Errorf("body = %v", body)
		}
		inline := parts[1].(map[string]any)["inlineData"].(map[string]any)
		if inline["mimeType"] != "image/jpeg" || inline["data"] != base64.StdEncoding.EncodeToString([]byte("ref")) {
			t.Errorf("inline = %v", inline)
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"here"},{"inline_data":{"mime_type":"image/png","data":"` +
			base64.StdEncoding.EncodeToString([]byte("PNGDATA")) + `"}}]}}]}`))
	}))
	defer server.Close()
	client := gemini.New("k", "", gemini.WithBaseURL(server.URL), gemini.WithImageModel("image-model"))
	image, err := client.GenerateImage(context.Background(), "draw", &gemini.InlineImage{MIMEType: "image/jpeg", Data: []byte("ref")})
	if err != nil || string(image) != "PNGDATA" {
		t.Fatalf("image=%q err=%v", image, err)
	}
}

func TestGenerateImageReportsBlockedPrompt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"SAFETY","content":{"parts":[]}}],"promptFeedback":{"blockReason":"OTHER"}}`))
	}))
	defer server.Close()
	_, err := gemini.New("k", "", gemini.WithBaseURL(server.URL)).GenerateImage(context.Background(), "draw", nil)
	if !errors.Is(err, gemini.ErrNoImage) || !strings.Contains(err.Error(), "OTHER") || !strings.Contains(err.Error(), "SAFETY") {
		t.Fatalf("err = %v", err)
	}
}

func TestReviewImageSendsSchemaAndReturnsText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		config := body["generationConfig"].(map[string]any)
		if r.URL.Path != "/models/vision-model:generateContent" || config["temperature"] != 0.0 || config["responseSchema"] == nil {
			t.Errorf("path=%s config=%v", r.URL.Path, config)
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"has_text\":false}"}]}}]}`))
	}))
	defer server.Close()
	client := gemini.New("k", "", gemini.WithBaseURL(server.URL), gemini.WithVisionModel("vision-model"))
	text, err := client.ReviewImage(context.Background(), "review", []byte("jpeg"), map[string]any{"type": "OBJECT"})
	if err != nil || text != `{"has_text":false}` {
		t.Fatalf("text=%q err=%v", text, err)
	}
}
```

(Add imports `encoding/base64`, `strings`.)

- [ ] **Step 2: Verify failure** — `go test ./internal/gemini` → FAIL.

- [ ] **Step 3: Implement** (additions to `client.go`)

```go
const (
	DefaultImageModel  = "gemini-3.1-flash-image-preview"
	DefaultVisionModel = "gemini-3.5-flash"
	imageTimeout       = 120 * time.Second
	reviewTimeout      = 60 * time.Second
)

var ErrNoImage = errors.New("gemini returned no image")

// InlineImage is an image part sent with a prompt.
type InlineImage struct {
	MIMEType string
	Data     []byte
}

func WithImageModel(model string) Option  { return func(c *Client) { if model != "" { c.imageModel = model } } }
func WithVisionModel(model string) Option { return func(c *Client) { if model != "" { c.visionModel = model } } }
```

Add `imageModel`, `visionModel` fields to `Client`, defaulting to `DefaultImageModel` / `DefaultVisionModel` in `New`. Then:

```go
type inlinePart struct {
	Text       string          `json:"text,omitempty"`
	InlineData *inlineDataJSON `json:"inlineData,omitempty"`
}

type inlineDataJSON struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

type imageResponse struct {
	Candidates []struct {
		FinishReason string `json:"finishReason"`
		Content      struct {
			Parts []struct {
				Text        string `json:"text"`
				InlineData  *struct{ Data string `json:"data"` } `json:"inlineData"`
				InlineData2 *struct{ Data string `json:"data"` } `json:"inline_data"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
}

// GenerateImage ports requestIllustration: 16:9, 2K, text+image modalities,
// optional inline reference. Returns the first inline image's bytes.
func (c *Client) GenerateImage(ctx context.Context, prompt string, reference *InlineImage) ([]byte, error) {
	parts := []inlinePart{{Text: prompt}}
	if reference != nil {
		parts = append(parts, inlinePart{InlineData: &inlineDataJSON{MIMEType: reference.MIMEType, Data: base64.StdEncoding.EncodeToString(reference.Data)}})
	}
	payload := map[string]any{
		"contents": []map[string]any{{"role": "user", "parts": parts}},
		"generationConfig": map[string]any{
			"responseModalities": []string{"TEXT", "IMAGE"},
			"imageConfig":        map[string]any{"aspectRatio": "16:9", "imageSize": "2K"},
		},
	}
	ctx, cancel := context.WithTimeout(ctx, imageTimeout)
	defer cancel()
	var response imageResponse
	if err := c.generate(ctx, c.imageModel, payload, &response); err != nil {
		return nil, err
	}
	finish := ""
	for _, candidate := range response.Candidates {
		if finish == "" {
			finish = candidate.FinishReason
		}
		for _, part := range candidate.Content.Parts {
			encoded := ""
			if part.InlineData != nil {
				encoded = part.InlineData.Data
			} else if part.InlineData2 != nil {
				encoded = part.InlineData2.Data
			}
			if encoded == "" {
				continue
			}
			if data, err := base64.StdEncoding.DecodeString(encoded); err == nil && len(data) > 0 {
				return data, nil
			}
		}
	}
	return nil, fmt.Errorf("%w (blocked: %q, finishReason: %q)", ErrNoImage, response.PromptFeedback.BlockReason, finish)
}

// ReviewImage sends a JPEG with a prompt to the vision model (temperature 0,
// JSON output constrained by schema) and returns the joined text.
func (c *Client) ReviewImage(ctx context.Context, prompt string, jpeg []byte, schema map[string]any) (string, error) {
	payload := map[string]any{
		"contents": []map[string]any{{"role": "user", "parts": []inlinePart{
			{Text: prompt},
			{InlineData: &inlineDataJSON{MIMEType: "image/jpeg", Data: base64.StdEncoding.EncodeToString(jpeg)}},
		}}},
		"generationConfig": map[string]any{"temperature": 0, "responseMimeType": "application/json", "responseSchema": schema},
	}
	ctx, cancel := context.WithTimeout(ctx, reviewTimeout)
	defer cancel()
	var response generateResponse
	if err := c.generate(ctx, c.visionModel, payload, &response); err != nil {
		return "", err
	}
	if len(response.Candidates) == 0 {
		return "", ErrEmptyResponse
	}
	var text strings.Builder
	for _, p := range response.Candidates[0].Content.Parts {
		text.WriteString(p.Text)
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", ErrEmptyResponse
	}
	return strings.TrimSpace(text.String()), nil
}
```

(Add import `encoding/base64`.)

- [ ] **Step 4: Run** — `go test -race ./internal/gemini -v` → PASS.
- [ ] **Step 5: Commit** — `git add internal/gemini && git commit -m "Add Gemini image generation and image review calls"`

---

### Task 2: Image processing helpers

**Files:** Create `internal/imaging/imaging.go`, `internal/imaging/imaging_test.go`; `go.mod`/`go.sum`

- [ ] **Step 1: Add dependencies**

```bash
go get github.com/gen2brain/webp@latest golang.org/x/image@latest
```

Open the `gen2brain/webp` package docs (`go doc github.com/gen2brain/webp`) and confirm the encoder signature (`webp.Encode(w io.Writer, m image.Image, o ...webp.Options) error` with `Options{Quality int, ...}`). If it differs, adapt `EncodeWebP` below and note it in your report.

- [ ] **Step 2: Write failing tests** — `internal/imaging/imaging_test.go`

```go
package imaging_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
)

func pngImage(t *testing.T, width, height int, transparent bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			alpha := uint8(255)
			if transparent {
				alpha = 0
			}
			img.Set(x, y, color.NRGBA{R: 200, G: 30, B: 30, A: alpha})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestSniffMIME(t *testing.T) {
	if got := imaging.SniffMIME(pngImage(t, 2, 2, false)); got != "image/png" {
		t.Fatalf("png = %q", got)
	}
	if got := imaging.SniffMIME([]byte("RIFF\x00\x00\x00\x00WEBPVP8 ")); got != "image/webp" {
		t.Fatalf("webp = %q", got)
	}
	if got := imaging.SniffMIME([]byte{0xff, 0xd8, 0xff, 0xe0}); got != "image/jpeg" {
		t.Fatalf("jpeg = %q", got)
	}
	if got := imaging.SniffMIME([]byte("<svg")); got != "" {
		t.Fatalf("svg = %q", got)
	}
}

func TestFitJPEGDownscalesAndFlattens(t *testing.T) {
	out, err := imaging.FitJPEG(pngImage(t, 2048, 1024, true), 1024, 85)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 1024 || b.Dy() != 512 {
		t.Fatalf("bounds = %v", b)
	}
	r, g, b, _ := img.At(10, 10).RGBA()
	if r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
		t.Fatalf("transparent pixels should flatten to white, got %d %d %d", r>>8, g>>8, b>>8)
	}

	small, err := imaging.FitJPEG(pngImage(t, 300, 200, false), 1024, 85)
	if err != nil {
		t.Fatal(err)
	}
	if img, _ := jpeg.Decode(bytes.NewReader(small)); img.Bounds().Dx() != 300 {
		t.Fatalf("must not enlarge: %v", img.Bounds())
	}
}

func TestEncodeWebP(t *testing.T) {
	out, width, height, err := imaging.EncodeWebP(pngImage(t, 640, 360, false), 82)
	if err != nil {
		t.Fatal(err)
	}
	if imaging.SniffMIME(out) != "image/webp" || width != 640 || height != 360 {
		t.Fatalf("mime=%q %dx%d", imaging.SniffMIME(out), width, height)
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	if _, err := imaging.FitJPEG([]byte("not an image"), 1024, 85); err == nil {
		t.Fatal("garbage should fail")
	}
}
```

- [ ] **Step 3: Verify failure**, then **implement** — `internal/imaging/imaging.go`

```go
// Package imaging decodes, resizes and re-encodes images for the publisher.
package imaging

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"

	"github.com/gen2brain/webp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// SniffMIME ports sniffImageMimeType for the formats the publisher handles.
func SniffMIME(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg"
	case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}):
		return "image/png"
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif"
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

func decode(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	return img, nil
}

// FitJPEG resizes to fit inside maxEdge×maxEdge without enlarging, flattens
// transparency onto white and encodes JPEG (sharp resize fit:inside +
// withoutEnlargement + flatten white + jpeg).
func FitJPEG(data []byte, maxEdge, quality int) ([]byte, error) {
	src, err := decode(data)
	if err != nil {
		return nil, err
	}
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width > maxEdge || height > maxEdge {
		if width >= height {
			height = max(1, height*maxEdge/width)
			width = maxEdge
		} else {
			width = max(1, width*maxEdge/height)
			height = maxEdge
		}
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(canvas, canvas.Bounds(), src, bounds, draw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, canvas, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return out.Bytes(), nil
}

// EncodeWebP re-encodes an image as lossy WebP at the given quality
// (encodeFeatureImageAsWebp uses 82) without resizing.
func EncodeWebP(data []byte, quality int) ([]byte, int, int, error) {
	src, err := decode(data)
	if err != nil {
		return nil, 0, 0, err
	}
	var out bytes.Buffer
	if err := webp.Encode(&out, src, webp.Options{Quality: quality}); err != nil {
		return nil, 0, 0, fmt.Errorf("encode webp: %w", err)
	}
	return out.Bytes(), src.Bounds().Dx(), src.Bounds().Dy(), nil
}
```

Note: sharp's `.rotate()` applies EXIF orientation; Go's decoders ignore EXIF. Generated images carry no EXIF, and the reference is only a visual hint, so this difference is accepted.

- [ ] **Step 4: Run** — `go test -race ./internal/imaging -v` → PASS. Also check a real-size encode time: `go test ./internal/imaging -run EncodeWebP -v` should finish in well under a few seconds.
- [ ] **Step 5: Commit** — `git add go.mod go.sum internal/imaging && git commit -m "Add image resizing and WebP encoding helpers"`

---

### Task 3: Illustration generation (port of the branch)

**Files:** Create `internal/illustration/prompts.go`, `internal/illustration/reference.go`, `internal/illustration/compliance.go`, `internal/illustration/generate.go`, and tests `internal/illustration/illustration_test.go`

- [ ] **Step 1: Port prompts verbatim** — `prompts.go`

Port `ILLUSTRATION_STYLE_RULES`, both reference branches and the prompt shape of `buildIllustrationPrompt` from `feature-image-publication.ts:47-97`, the four correction constants from `feature-illustration.ts:79-86`, and `buildCompliancePrompt` + `COMPLIANCE_RESPONSE_SCHEMA` from `feature-illustration.ts:338-369`, as:

```go
package illustration

import (
	"fmt"
	"strings"
)

const styleRules = `...verbatim ILLUSTRATION_STYLE_RULES...`

const (
	TextViolationCorrection   = "The previous attempt contained writing. Remove all writing, speech bubbles and signs."
	LogoViolationCorrection   = "The previous attempt contained a logo or watermark. Remove every logo, brand mark and watermark."
	InjuryViolationCorrection = "The previous attempt depicted injury or violence the article does not state. Depict only what the article supports."
	UnverifiedCorrection      = "The previous attempt could not be verified. Follow every no writing and accuracy rule exactly."
)

type Article struct {
	Title   string
	Excerpt string
}

// BuildPrompt ports buildIllustrationPrompt.
func BuildPrompt(article Article, hasReference bool, correction string) string {
	referenceRules := noReferenceRules
	if hasReference {
		referenceRules = withReferenceRules
	}
	correctionRules := ""
	if trimmed := strings.TrimSpace(correction); trimmed != "" {
		correctionRules = "\n\nCorrection for this attempt:\n- " + trimmed
	}
	return fmt.Sprintf("Create an original editorial illustration for this artificial intelligence news article.\n\nHeadline: %s\nSummary: %s\n\n%s\n\n%s%s",
		article.Title, article.Excerpt, referenceRules, styleRules, correctionRules)
}

const withReferenceRules = `...verbatim hasReference branch...`
const noReferenceRules = `...verbatim no-reference branch...`

// ComplianceSchema is COMPLIANCE_RESPONSE_SCHEMA.
var ComplianceSchema = map[string]any{
	"type": "OBJECT",
	"properties": map[string]any{
		"has_text":                               map[string]any{"type": "BOOLEAN"},
		"has_logo_or_watermark":                  map[string]any{"type": "BOOLEAN"},
		"depicts_unsupported_injury_or_violence": map[string]any{"type": "BOOLEAN"},
		"notes":                                  map[string]any{"type": "STRING"},
	},
	"required": []string{"has_text", "has_logo_or_watermark", "depicts_unsupported_injury_or_violence", "notes"},
}

// BuildCompliancePrompt ports buildCompliancePrompt.
func BuildCompliancePrompt(article Article) string {
	return fmt.Sprintf(`...verbatim prompt with %s for headline and summary...`, article.Title, article.Excerpt)
}
```

Replace every `...verbatim...` with the exact branch text (backticks in Go raw strings: the Node texts contain none; `%` characters: none — verify). Then diff: print `BuildPrompt(Article{"H","S"}, true, "")`, `BuildPrompt(..., false, "Fix")` and `BuildCompliancePrompt` from a scratch test and compare to the branch output for the same inputs (you can run the branch module with `node` only if it is already installed and its deps resolve; otherwise compare line by line manually). Report the result.

- [ ] **Step 2: Reference fetch** — `reference.go`

```go
package illustration

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	MaxReferenceBytes = 8 << 20
	referenceTimeout  = 15 * time.Second
	userAgent         = "TechNews-Editorial-Importer/2.0"
)

// FetchReference ports fetchReferenceImage: returns (nil, nil) whenever the
// image cannot be used, so the illustration falls back to text-only.
func FetchReference(ctx context.Context, client *http.Client, imageURL string) ([]byte, error) {
	if imageURL == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, referenceTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, nil
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, nil
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	mediaType = strings.ToLower(mediaType)
	if !strings.HasPrefix(mediaType, "image/") || mediaType == "image/svg+xml" {
		return nil, nil
	}
	if declared, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); err == nil && declared > MaxReferenceBytes {
		return nil, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxReferenceBytes+1))
	if err != nil || len(data) == 0 || len(data) > MaxReferenceBytes {
		return nil, nil
	}
	return data, fmt.Errorf("%w", nil) // replaced below
}
```

(Replace the final line with `return data, nil`.) Reference images are fetched from URLs that came from approved source pages or feeds; they are held in memory only and never stored.

- [ ] **Step 3: Compliance verdict** — `compliance.go`

```go
package illustration

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Verdict ports IllustrationComplianceVerdict.
type Verdict struct {
	Compliant  bool
	HasText    bool
	HasLogo    bool
	HasInjury  bool
	Notes      string
	Unverified bool
}

var (
	leadingFence  = regexp.MustCompile("(?i)^```(?:json)?\\s*")
	trailingFence = regexp.MustCompile("\\s*```$")
	jsonObject    = regexp.MustCompile(`(?s)\{.*\}`)
)

func unverified(notes string) Verdict { return Verdict{Notes: notes, Unverified: true} }

// ParseVerdict ports the response handling in checkIllustrationCompliance:
// fail closed on anything unparsable or incomplete.
func ParseVerdict(raw string) Verdict {
	cleaned := strings.TrimSpace(trailingFence.ReplaceAllString(leadingFence.ReplaceAllString(raw, ""), ""))
	candidate := cleaned
	if match := jsonObject.FindString(cleaned); match != "" {
		candidate = match
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(candidate), &parsed); err != nil {
		return unverified("compliance check returned unparsable JSON")
	}
	flags := [3]bool{}
	for i, key := range []string{"has_text", "has_logo_or_watermark", "depicts_unsupported_injury_or_violence"} {
		value, ok := parsed[key].(bool)
		if !ok {
			return unverified("compliance check returned an incomplete verdict")
		}
		flags[i] = value
	}
	notes, _ := parsed["notes"].(string)
	return Verdict{Compliant: !flags[0] && !flags[1] && !flags[2], HasText: flags[0], HasLogo: flags[1], HasInjury: flags[2], Notes: notes}
}

// Correction ports correctionForVerdict.
func Correction(verdict Verdict) string {
	switch {
	case verdict.HasText:
		return TextViolationCorrection
	case verdict.HasLogo:
		return LogoViolationCorrection
	case verdict.HasInjury:
		return InjuryViolationCorrection
	}
	return UnverifiedCorrection
}
```

- [ ] **Step 4: Generation loop** — `generate.go`

```go
package illustration

import (
	"context"
	"errors"
	"log/slog"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
)

const (
	MaxGenerationAttempts = 3
	referenceMaxEdge      = 1024
	referenceQuality      = 85
	reviewMaxEdge         = 1024
	reviewQuality         = 80
)

// ErrNoCompliantImage means every attempt was rejected or failed.
var ErrNoCompliantImage = errors.New("no compliant illustration after 3 generation attempts")

type ImageModel interface {
	GenerateImage(ctx context.Context, prompt string, reference *gemini.InlineImage) ([]byte, error)
	ReviewImage(ctx context.Context, prompt string, jpeg []byte, schema map[string]any) (string, error)
}

type Generator struct {
	model  ImageModel
	logger *slog.Logger
}

func NewGenerator(model ImageModel, logger *slog.Logger) *Generator { return &Generator{model: model, logger: logger} }

// Generate ports generateIllustration: at most 3 generation calls; a failed
// referenced call retries once text-only; rejected images regenerate with a
// correction; text/logo rejections drop the reference.
func (g *Generator) Generate(ctx context.Context, article Article, rawReference []byte) ([]byte, error) {
	var reference *gemini.InlineImage
	if len(rawReference) > 0 {
		if normalized, err := imaging.FitJPEG(rawReference, referenceMaxEdge, referenceQuality); err == nil {
			reference = &gemini.InlineImage{MIMEType: "image/jpeg", Data: normalized}
		} else {
			g.logger.InfoContext(ctx, "reference image could not be decoded; generating from text", "error", err)
		}
	}
	correction := ""
	var lastErr error
	for attempt := 1; attempt <= MaxGenerationAttempts; attempt++ {
		image, err := g.model.GenerateImage(ctx, BuildPrompt(article, reference != nil, correction), reference)
		if err != nil {
			lastErr = err
			if reference != nil {
				g.logger.InfoContext(ctx, "referenced illustration failed; retrying from text", "error", err)
				reference = nil
				continue
			}
			return nil, err
		}
		verdict := g.review(ctx, article, image)
		if verdict.Compliant {
			return image, nil
		}
		g.logger.InfoContext(ctx, "illustration rejected by compliance check", "attempt", attempt,
			"text", verdict.HasText, "logo", verdict.HasLogo, "injury", verdict.HasInjury, "unverified", verdict.Unverified, "notes", verdict.Notes)
		correction = Correction(verdict)
		if reference != nil && (verdict.HasText || verdict.HasLogo) {
			reference = nil
		}
		lastErr = ErrNoCompliantImage
	}
	if lastErr == nil {
		lastErr = ErrNoCompliantImage
	}
	if !errors.Is(lastErr, ErrNoCompliantImage) {
		return nil, lastErr
	}
	return nil, ErrNoCompliantImage
}

func (g *Generator) review(ctx context.Context, article Article, image []byte) Verdict {
	jpeg, err := imaging.FitJPEG(image, reviewMaxEdge, reviewQuality)
	if err != nil {
		return unverified("compliance check could not decode the generated image")
	}
	raw, err := g.model.ReviewImage(ctx, BuildCompliancePrompt(article), jpeg, ComplianceSchema)
	if err != nil {
		return unverified("compliance check request failed: " + err.Error())
	}
	return ParseVerdict(raw)
}
```

- [ ] **Step 5: Tests** — `illustration_test.go` (package `illustration_test`), with a fake `ImageModel` that returns scripted images/verdicts. Use a real tiny PNG (encode with `image/png`) as the fake generated image so `FitJPEG` succeeds. Cover:
  1. `BuildPrompt` contains `"Headline: H\nSummary: S"`, the reference/no-reference first lines, and `"\n\nCorrection for this attempt:\n- Fix"` only when a correction is given.
  2. `ParseVerdict`: clean JSON → compliant; fenced JSON → parsed; missing flag → unverified, not compliant; garbage → unverified.
  3. `Correction` priority text > logo > injury > unverified.
  4. `Generate`: first image compliant → returned after 1 generation; text violation → second prompt contains `TextViolationCorrection` and is sent **without** reference; three rejections → `ErrNoCompliantImage` after exactly 3 generations; referenced generation error → one text-only retry (second call has nil reference); text-only generation error → returned immediately (1 call).
  5. `FetchReference` with httptest: `image/png` → bytes; `image/svg+xml` → nil; `text/html` → nil; 404 → nil; body over `MaxReferenceBytes` → nil.

- [ ] **Step 6: Run** — `go test -race ./internal/illustration -v` → PASS.
- [ ] **Step 7: Commit** — `git add internal/illustration && git commit -m "Port illustration prompts, compliance check and generation loop"`

---

### Task 4: S3 feature image storage

**Files:** Create `internal/media/storage.go`, `internal/media/storage_test.go`; `go.mod`/`go.sum`

- [ ] **Step 1: Dependencies** — `go get github.com/aws/aws-sdk-go-v2/config@latest github.com/aws/aws-sdk-go-v2/service/s3@latest`

- [ ] **Step 2: Implement** — `internal/media/storage.go`

```go
// Package media stores feature images in S3 (port of feature-image-storage.ts).
package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
)

const (
	MaxImageBytes = 10 << 20
	cacheControl  = "public, max-age=31536000, immutable"
	verifyTimeout = 20 * time.Second
)

type Config struct {
	Region        string
	Bucket        string
	Prefix        string
	PublicBaseURL string
}

// Validate ports getFeatureImageStorageConfig's checks.
func (c Config) Validate() error {
	var missing []string
	if c.Region == "" {
		missing = append(missing, "AWS_REGION")
	}
	if c.Bucket == "" {
		missing = append(missing, "S3_FEATURE_IMAGE_BUCKET")
	}
	if c.PublicBaseURL == "" {
		missing = append(missing, "S3_FEATURE_IMAGE_PUBLIC_URL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("feature image storage is not configured; missing %s", strings.Join(missing, ", "))
	}
	if c.Prefix == "" {
		return errors.New("S3_FEATURE_IMAGE_PREFIX must not be empty")
	}
	if !strings.HasPrefix(c.PublicBaseURL, "https://") {
		return errors.New("S3_FEATURE_IMAGE_PUBLIC_URL must be an https URL")
	}
	return nil
}

// ObjectAPI is the subset of the S3 client the store uses.
type ObjectAPI interface {
	PutObject(ctx context.Context, input *s3.PutObjectInput, options ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, input *s3.DeleteObjectInput, options ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type Stored struct {
	Key string
	URL string
}

type Store struct {
	config Config
	api    ObjectAPI
	http   *http.Client
	now    func() time.Time
}

func NewStore(config Config, api ObjectAPI, httpClient *http.Client, now func() time.Time) *Store {
	config.Prefix = strings.Trim(config.Prefix, "/")
	config.PublicBaseURL = strings.TrimRight(config.PublicBaseURL, "/")
	return &Store{config: config, api: api, http: httpClient, now: now}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// SanitizeSlug ports sanitizeFeatureImageSlug.
func SanitizeSlug(slug string) (string, error) {
	safe := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(slug), "-"), "-")
	if len(safe) > 120 {
		safe = strings.Trim(safe[:120], "-")
	}
	if safe == "" {
		return "", fmt.Errorf("feature image slug is empty after sanitizing: %q", slug)
	}
	return safe, nil
}

// Key ports buildFeatureImageKey: <prefix>/YYYY/MM/<slug>-<sha16>.<ext> (UTC).
func Key(prefix, slug, sha string, extension string, now time.Time) string {
	now = now.UTC()
	return fmt.Sprintf("%s/%04d/%02d/%s-%s.%s", prefix, now.Year(), int(now.Month()), slug, sha[:16], extension)
}

// StoreWebP uploads WebP bytes, verifies the public URL serves exactly those
// bytes, and deletes the object again if verification fails.
func (s *Store) StoreWebP(ctx context.Context, slug string, data []byte) (Stored, error) {
	if len(data) == 0 || len(data) > MaxImageBytes {
		return Stored{}, fmt.Errorf("feature image size %d is outside 1..%d bytes", len(data), MaxImageBytes)
	}
	if imaging.SniffMIME(data) != "image/webp" {
		return Stored{}, errors.New("feature image bytes are not WebP")
	}
	safeSlug, err := SanitizeSlug(slug)
	if err != nil {
		return Stored{}, err
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	key := Key(s.config.Prefix, safeSlug, sha, "webp", s.now())
	url := s.config.PublicBaseURL + "/" + key

	if _, err := s.api.PutObject(ctx, &s3.PutObjectInput{
		Bucket:         aws.String(s.config.Bucket),
		Key:            aws.String(key),
		Body:           bytes.NewReader(data),
		ContentType:    aws.String("image/webp"),
		CacheControl:   aws.String(cacheControl),
		ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(sum[:])),
	}); err != nil {
		return Stored{}, fmt.Errorf("upload feature image: %w", err)
	}
	if err := s.verify(ctx, url, sha, len(data)); err != nil {
		_ = s.Delete(context.WithoutCancel(ctx), key)
		return Stored{}, fmt.Errorf("uploaded feature image failed public verification: %w", err)
	}
	return Stored{Key: key, URL: url}, nil
}

func (s *Store) verify(ctx context.Context, url, sha string, size int) error {
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Cache-Control", "no-store")
	client := *s.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("redirects are not allowed") }
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("public URL returned HTTP %d", resp.StatusCode)
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaType != "image/webp" {
		return fmt.Errorf("public URL returned Content-Type %q, expected image/webp", mediaType)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxImageBytes+1))
	if err != nil {
		return err
	}
	if len(body) != size {
		return fmt.Errorf("public URL returned %d bytes, expected %d", len(body), size)
	}
	got := sha256.Sum256(body)
	if hex.EncodeToString(got[:]) != sha {
		return errors.New("public URL content hash does not match the uploaded bytes")
	}
	return nil
}

// Delete removes an uploaded object; keys outside the prefix are refused.
func (s *Store) Delete(ctx context.Context, key string) error {
	if !strings.HasPrefix(key, s.config.Prefix+"/") || strings.Contains(key, "..") {
		return fmt.Errorf("refusing to delete a key outside %s/: %s", s.config.Prefix, key)
	}
	_, err := s.api.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.config.Bucket), Key: aws.String(key)})
	return err
}
```

- [ ] **Step 3: Tests** — `storage_test.go` with a fake `ObjectAPI` that records Put/Delete inputs and serves the uploaded bytes from an httptest server acting as the public base URL (use `httptest.NewTLSServer` and pass `server.Client()` as `httpClient`, since `PublicBaseURL` must be https). Cover: key layout `dev/2026/09/<slug>-<sha16>.webp` for `now=2026-09-23`; Put carries content type, cache control and a base64 SHA-256 checksum; verification success returns the URL; wrong bytes/content type/404 → error **and** the object is deleted; non-WebP input rejected before upload; `Delete` refuses `other/key` and `dev/../x`; `Config.Validate` messages for missing vars and http URL; `SanitizeSlug` of `"Hello, World!"` → `"hello-world"` and of `"!!!"` → error.

- [ ] **Step 4: Run** — `go test -race ./internal/media -v` → PASS.
- [ ] **Step 5: Commit** — `git add go.mod go.sum internal/media && git commit -m "Store feature images in S3 with public verification"`

---

### Task 5: S3Illustrator

**Files:** Create `internal/illustration/illustrator.go`, `internal/illustration/illustrator_test.go`

- [ ] **Step 1: Implement**

```go
package illustration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

const webpQuality = 82

type ImageStore interface {
	StoreWebP(ctx context.Context, slug string, data []byte) (media.Stored, error)
	Delete(ctx context.Context, key string) error
}

// S3Illustrator implements publisher.Illustrator with a generated, verified,
// S3-hosted WebP. The source image is only an in-memory reference.
type S3Illustrator struct {
	generator *Generator
	store     ImageStore
	http      *http.Client
	logger    *slog.Logger
}

func NewS3Illustrator(generator *Generator, store ImageStore, httpClient *http.Client, logger *slog.Logger) *S3Illustrator {
	return &S3Illustrator{generator: generator, store: store, http: httpClient, logger: logger}
}

func (s *S3Illustrator) Illustrate(ctx context.Context, request publisher.IllustrationRequest) (publisher.Illustration, error) {
	reference, _ := FetchReference(ctx, s.http, request.ReferenceImageURL)
	image, err := s.generator.Generate(ctx, Article{Title: request.Title, Excerpt: request.Excerpt}, reference)
	if err != nil {
		if errors.Is(err, ErrNoCompliantImage) {
			// Retrying later repeats three paid generations with the same inputs;
			// leave it to an editor's Retry in the app.
			return publisher.Illustration{}, publisher.Permanent(err)
		}
		return publisher.Illustration{}, publisher.ClassifyGeminiError(fmt.Errorf("generate illustration: %w", err))
	}
	webp, _, _, err := imaging.EncodeWebP(image, webpQuality)
	if err != nil {
		return publisher.Illustration{}, publisher.Permanent(fmt.Errorf("encode illustration: %w", err))
	}
	stored, err := s.store.StoreWebP(ctx, request.Slug, webp)
	if err != nil {
		return publisher.Illustration{}, fmt.Errorf("store illustration: %w", err)
	}
	return publisher.Illustration{
		URL: stored.URL,
		Discard: func(ctx context.Context) {
			if err := s.store.Delete(ctx, stored.Key); err != nil {
				s.logger.WarnContext(ctx, "could not delete unused illustration", "key", stored.Key, "error", err)
			}
		},
	}, nil
}
```

Classify Gemini errors with the shared helper from phase 4a: in the `generate illustration` branch return `publisher.ClassifyGeminiError(fmt.Errorf("generate illustration: %w", err))` so missing keys and 401/403/404 become system faults (requeued without using an attempt), safety blocks and other 4xx become permanent, and 429/5xx/network errors stay transient.

- [ ] **Step 2: Tests** — fake `ImageModel` (compliant PNG) + fake `ImageStore` recording keys: success returns the stored URL and `Discard` deletes the key; `ErrNoCompliantImage` is permanent; store failure is transient (not permanent).
- [ ] **Step 3: Run** — `go test -race ./internal/illustration -v` → PASS.
- [ ] **Step 4: Commit** — `git add internal/illustration && git commit -m "Provide S3-hosted illustrations to the publisher"`

---

### Task 6: Configuration and app wiring

**Files:** Modify `internal/config/config.go` (+ test), `internal/app/app.go`, `Makefile`, `.env`-style docs in `README.md`

- [ ] **Step 1: Config fields** (`Config`), loaded in `Load`:

| Field | Env | Default |
|---|---|---|
| `PublisherEnabled bool` | `PUBLISHER_ENABLED` (same on/off parsing as `COLLECTOR_ENABLED`) | false |
| `PublisherInterval time.Duration` | `PUBLISHER_INTERVAL` | `1m` |
| `GeminiAPIKey string` | `GEMINI_API_KEY` | "" |
| `GeminiTextModel`, `GeminiImageModel`, `GeminiVisionModel string` | `GEMINI_TEXT_MODEL`, `GEMINI_IMAGE_MODEL`, `GEMINI_VISION_MODEL` | "" (client defaults) |
| `FeatureImages media.Config` — or plain fields `AWSRegion`, `S3Bucket`, `S3Prefix`, `S3PublicURL` | `AWS_REGION`, `S3_FEATURE_IMAGE_BUCKET`, `S3_FEATURE_IMAGE_PREFIX` (default `features`), `S3_FEATURE_IMAGE_PUBLIC_URL` | |

`Validate`: when `PublisherEnabled`, require `GeminiAPIKey`, the S3 fields (use the same checks as `media.Config.Validate`, implemented in config to avoid importing media), and `PublisherInterval >= 10s`. `String()` must redact `GeminiAPIKey` like `JWTSecret`. Tests: defaults off; enabled without key → error naming `GEMINI_API_KEY`; enabled with all fields → ok; key never appears in `String()`.

- [ ] **Step 2: Wire the app** — in `NewWithDatabaseAt`, when `cfg.PublisherEnabled`:

```go
	awsConfig, err := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion(cfg.AWSRegion))
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", err)
	}
	geminiClient := gemini.New(cfg.GeminiAPIKey, cfg.GeminiTextModel,
		gemini.WithImageModel(cfg.GeminiImageModel), gemini.WithVisionModel(cfg.GeminiVisionModel))
	httpClient := &http.Client{}
	imageStore := media.NewStore(media.Config{Region: cfg.AWSRegion, Bucket: cfg.S3Bucket, Prefix: cfg.S3Prefix, PublicBaseURL: cfg.S3PublicURL},
		s3.NewFromConfig(awsConfig), httpClient, now)
	newsPublisher := publisher.New(publisher.Deps{
		Store:       newsroomStore,
		Fetcher:     collector.NewFetcher(),
		Rewriter:    publisher.NewRewriter(geminiClient),
		Illustrator: illustration.NewS3Illustrator(illustration.NewGenerator(geminiClient, logger), imageStore, httpClient, logger),
		Articles:    publisher.NewSQLiteArticles(db, now),
		Notifier:    indexnow.New(),
		Now:         now,
		Logger:      logger,
	})
	application.background = append(application.background, func(ctx context.Context) {
		newsPublisher.Loop(ctx, cfg.PublisherInterval)
	})
```

AWS credentials come from the default chain (`AWS_PROFILE` locally, instance/env credentials in production) — never from config files in the repo.

- [ ] **Step 3: Makefile**

```make
# Publishes queued candidates for real (Gemini + S3). Requires GEMINI_API_KEY and
# AWS credentials; uses a dev S3 prefix so test images never mix with production.
dev-publish:
	JWT_SECRET=$${JWT_SECRET:-$(DEV_JWT_SECRET)} COLLECTOR_ENABLED=$${COLLECTOR_ENABLED:-1} \
	PUBLISHER_ENABLED=1 S3_FEATURE_IMAGE_PREFIX=$${S3_FEATURE_IMAGE_PREFIX:-dev-features} go run ./cmd/api
```

Add `dev-publish` to `.PHONY`.

- [ ] **Step 4: Run** — `make check && make test-race && make contracts-check` → PASS (tests build configs without the publisher, so no AWS/Gemini access happens).
- [ ] **Step 5: Commit** — `git add go.mod go.sum internal/config internal/app Makefile && git commit -m "Wire the publisher behind PUBLISHER_ENABLED"`

---

### Task 7: Local end-to-end publish, docs and policy

- [ ] **Step 1: Preconditions (ask the user before running):** `GEMINI_API_KEY` exported; AWS credentials for the feature-image bucket available (`AWS_PROFILE=aiandtech` per `apps/server/.env.example`), `AWS_REGION=eu-west-1`, `S3_FEATURE_IMAGE_BUCKET`, `S3_FEATURE_IMAGE_PUBLIC_URL` from `.env.example`. The run uses the **dev DB** (`data/technews.db`) and prefix `dev-features`.

- [ ] **Step 2: Run**

```bash
make dev-publish
```

In the iOS app (Debug): select one real candidate → Publish. For a fast check, first set the delay to 1–2 minutes in Settings. Watch the API log for `published candidate`, then:

```bash
sqlite3 ../../data/technews.db "SELECT id, slug, featured_image, published_at FROM articles ORDER BY id DESC LIMIT 1;"
curl -s -o /dev/null -w "%{http_code} %{content_type}\n" "<featured_image URL from above>"
```

Expected: a new article row in Node's datetime format, an https S3 URL under `dev-features/` returning `200 image/webp`; the candidate shows under **Recently published** in the Queue tab with **View live** (the live link 404s locally because the article is only in the dev DB — expected).

- [ ] **Step 3: Docs** — README (server-go): replace "The publisher (queued → published) is not implemented yet." with a paragraph describing `PUBLISHER_ENABLED`, the env vars table above, the minimum-gap rule, retries (3 attempts, 5 minutes), and `make dev-publish`.

- [ ] **Step 4: Publishing policy** — update `NEWS_PUBLISHING_POLICY.md`: (a) collection no longer publishes; publishing requires an editor selecting a candidate in the Omni Control app; (b) published items are spaced by the configured random delay with a minimum gap (replacing "1 article per run / 4 slots a day"); (c) the Images rules from the `s3-feature-images` branch (`git show 's3-feature-images:NEWS_PUBLISHING_POLICY.md'`, section "Images") now apply to the Go publisher. Keep the approved-feed list, AI-only gate and rewrite rules unchanged. Note at the top that the Node importer remains the active publisher until the Go publisher is enabled in production (phase 5).

- [ ] **Step 5: Commit** — `git add README.md ../../NEWS_PUBLISHING_POLICY.md && git commit -m "Document the Go publisher and the editor-selected publishing policy"`
