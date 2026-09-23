package illustration_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBuildPromptContainsExpectedClauses(t *testing.T) {
	article := illustration.Article{Title: "H", Excerpt: "S"}

	withRef := illustration.BuildPrompt(article, true, "")
	if !strings.Contains(withRef, "Headline: H\nSummary: S") {
		t.Fatalf("missing headline/summary: %q", withRef)
	}
	if !strings.HasPrefix(withRef[strings.Index(withRef, "Summary: S")+len("Summary: S\n\n"):], "A reference image from the source report is attached for visual guidance only.") {
		t.Fatalf("missing reference-rules first line: %q", withRef)
	}
	if strings.Contains(withRef, "Correction for this attempt") {
		t.Fatalf("unexpected correction: %q", withRef)
	}

	noRef := illustration.BuildPrompt(article, false, "")
	if !strings.HasPrefix(noRef[strings.Index(noRef, "Summary: S")+len("Summary: S\n\n"):], "No reference image is available, so build the illustration from the article text alone.") {
		t.Fatalf("missing no-reference first line: %q", noRef)
	}

	withCorrection := illustration.BuildPrompt(article, false, "Fix")
	if !strings.Contains(withCorrection, "\n\nCorrection for this attempt:\n- Fix") {
		t.Fatalf("missing correction: %q", withCorrection)
	}
}

func TestParseVerdict(t *testing.T) {
	clean := illustration.ParseVerdict(`{"has_text":false,"has_logo_or_watermark":false,"depicts_unsupported_injury_or_violence":false,"notes":"clean"}`)
	if !clean.Compliant || clean.Unverified {
		t.Fatalf("clean verdict = %+v", clean)
	}

	fenced := illustration.ParseVerdict("```json\n{\"has_text\":true,\"has_logo_or_watermark\":false,\"depicts_unsupported_injury_or_violence\":false,\"notes\":\"text found\"}\n```")
	if fenced.Compliant || !fenced.HasText || fenced.Unverified {
		t.Fatalf("fenced verdict = %+v", fenced)
	}

	missing := illustration.ParseVerdict(`{"has_text":false}`)
	if missing.Compliant || !missing.Unverified {
		t.Fatalf("missing-flag verdict = %+v", missing)
	}

	garbage := illustration.ParseVerdict("not json at all")
	if garbage.Compliant || !garbage.Unverified {
		t.Fatalf("garbage verdict = %+v", garbage)
	}
}

func TestCorrectionPriority(t *testing.T) {
	if got := illustration.Correction(illustration.Verdict{HasText: true, HasLogo: true, HasInjury: true}); got != illustration.TextViolationCorrection {
		t.Fatalf("text priority: %q", got)
	}
	if got := illustration.Correction(illustration.Verdict{HasLogo: true, HasInjury: true}); got != illustration.LogoViolationCorrection {
		t.Fatalf("logo priority: %q", got)
	}
	if got := illustration.Correction(illustration.Verdict{HasInjury: true}); got != illustration.InjuryViolationCorrection {
		t.Fatalf("injury priority: %q", got)
	}
	if got := illustration.Correction(illustration.Verdict{}); got != illustration.UnverifiedCorrection {
		t.Fatalf("unverified fallback: %q", got)
	}
}

type scriptedReview struct {
	json string
	err  error
}

type fakeModel struct {
	t              *testing.T
	generateCalls  int
	generateErrs   []error
	reviews        []scriptedReview
	referenceSeen  []bool
	generatedImage []byte
}

func (f *fakeModel) GenerateImage(_ context.Context, _ string, reference *gemini.InlineImage) ([]byte, error) {
	f.referenceSeen = append(f.referenceSeen, reference != nil)
	idx := f.generateCalls
	f.generateCalls++
	if idx < len(f.generateErrs) && f.generateErrs[idx] != nil {
		return nil, f.generateErrs[idx]
	}
	return f.generatedImage, nil
}

func (f *fakeModel) ReviewImage(_ context.Context, _ string, _ []byte, _ map[string]any) (string, error) {
	if len(f.reviews) == 0 {
		f.t.Fatal("unexpected ReviewImage call")
	}
	r := f.reviews[0]
	f.reviews = f.reviews[1:]
	if r.err != nil {
		return "", r.err
	}
	return r.json, nil
}

const compliantJSON = `{"has_text":false,"has_logo_or_watermark":false,"depicts_unsupported_injury_or_violence":false,"notes":"clean"}`
const textViolationJSON = `{"has_text":true,"has_logo_or_watermark":false,"depicts_unsupported_injury_or_violence":false,"notes":"text"}`

func TestGenerateReturnsFirstCompliantImage(t *testing.T) {
	img := tinyPNG(t)
	model := &fakeModel{t: t, generatedImage: img, reviews: []scriptedReview{{json: compliantJSON}}}
	generator := illustration.NewGenerator(model, discardLogger())
	out, err := generator.Generate(context.Background(), illustration.Article{Title: "T", Excerpt: "E"}, nil)
	if err != nil || !bytes.Equal(out, img) {
		t.Fatalf("out=%v err=%v", out, err)
	}
	if model.generateCalls != 1 {
		t.Fatalf("generateCalls = %d", model.generateCalls)
	}
}

func TestGenerateDropsReferenceOnTextViolation(t *testing.T) {
	img := tinyPNG(t)
	model := &fakeModel{t: t, generatedImage: img, reviews: []scriptedReview{{json: textViolationJSON}, {json: compliantJSON}}}
	generator := illustration.NewGenerator(model, discardLogger())
	_, err := generator.Generate(context.Background(), illustration.Article{Title: "T", Excerpt: "E"}, tinyPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	if model.generateCalls != 2 {
		t.Fatalf("generateCalls = %d", model.generateCalls)
	}
	if model.referenceSeen[0] != true || model.referenceSeen[1] != false {
		t.Fatalf("referenceSeen = %v", model.referenceSeen)
	}
}

func TestGenerateFailsAfterThreeRejections(t *testing.T) {
	img := tinyPNG(t)
	model := &fakeModel{t: t, generatedImage: img, reviews: []scriptedReview{{json: textViolationJSON}, {json: textViolationJSON}, {json: textViolationJSON}}}
	generator := illustration.NewGenerator(model, discardLogger())
	_, err := generator.Generate(context.Background(), illustration.Article{Title: "T", Excerpt: "E"}, nil)
	if !errors.Is(err, illustration.ErrNoCompliantImage) {
		t.Fatalf("err = %v", err)
	}
	if model.generateCalls != 3 {
		t.Fatalf("generateCalls = %d", model.generateCalls)
	}
}

func TestGenerateRetriesTextOnlyAfterReferencedFailure(t *testing.T) {
	img := tinyPNG(t)
	genErr := errors.New("boom")
	model := &fakeModel{t: t, generatedImage: img, generateErrs: []error{genErr, nil}, reviews: []scriptedReview{{json: compliantJSON}}}
	generator := illustration.NewGenerator(model, discardLogger())
	out, err := generator.Generate(context.Background(), illustration.Article{Title: "T", Excerpt: "E"}, tinyPNG(t))
	if err != nil || !bytes.Equal(out, img) {
		t.Fatalf("out=%v err=%v", out, err)
	}
	if model.generateCalls != 2 {
		t.Fatalf("generateCalls = %d", model.generateCalls)
	}
	if model.referenceSeen[0] != true || model.referenceSeen[1] != false {
		t.Fatalf("referenceSeen = %v", model.referenceSeen)
	}
}

func TestGenerateReturnsImmediatelyOnTextOnlyFailure(t *testing.T) {
	genErr := errors.New("boom")
	model := &fakeModel{t: t, generateErrs: []error{genErr}}
	generator := illustration.NewGenerator(model, discardLogger())
	_, err := generator.Generate(context.Background(), illustration.Article{Title: "T", Excerpt: "E"}, nil)
	if !errors.Is(err, genErr) {
		t.Fatalf("err = %v", err)
	}
	if model.generateCalls != 1 {
		t.Fatalf("generateCalls = %d", model.generateCalls)
	}
}

const injuryViolationJSON = `{"has_text":false,"has_logo_or_watermark":false,"depicts_unsupported_injury_or_violence":true,"notes":"injury"}`

func TestGenerateReviewSystemFaultStopsWithoutRegenerating(t *testing.T) {
	reviewErr := &gemini.Error{Status: 404}
	model := &fakeModel{t: t, generatedImage: tinyPNG(t), reviews: []scriptedReview{{err: reviewErr}}}
	_, err := illustration.NewGenerator(model, discardLogger()).Generate(context.Background(), illustration.Article{Title: "T", Excerpt: "E"}, nil)
	if !errors.Is(err, reviewErr) || !publisher.IsSystemFault(err) {
		t.Fatalf("err = %v, want system fault wrapping the review error", err)
	}
	if model.generateCalls != 1 {
		t.Fatalf("generateCalls = %d, want 1", model.generateCalls)
	}
}

func TestGenerateReviewTransientErrorStopsWithoutRegenerating(t *testing.T) {
	for name, reviewErr := range map[string]error{
		"503":      &gemini.Error{Status: 503},
		"429":      &gemini.Error{Status: 429},
		"network":  errors.New("connection reset"),
		"deadline": context.DeadlineExceeded,
	} {
		t.Run(name, func(t *testing.T) {
			model := &fakeModel{t: t, generatedImage: tinyPNG(t), reviews: []scriptedReview{{err: reviewErr}}}
			_, err := illustration.NewGenerator(model, discardLogger()).Generate(context.Background(), illustration.Article{Title: "T", Excerpt: "E"}, nil)
			if !errors.Is(err, reviewErr) || publisher.IsSystemFault(err) || publisher.IsPermanent(err) {
				t.Fatalf("err = %v, want transient review error", err)
			}
			if model.generateCalls != 1 {
				t.Fatalf("generateCalls = %d, want 1", model.generateCalls)
			}
		})
	}
}

func TestGenerateReviewPermanentErrorRegeneratesAsUnverified(t *testing.T) {
	reviewErr := &gemini.Error{Status: 400}
	model := &fakeModel{t: t, generatedImage: tinyPNG(t), reviews: []scriptedReview{{err: reviewErr}, {err: reviewErr}, {err: reviewErr}}}
	_, err := illustration.NewGenerator(model, discardLogger()).Generate(context.Background(), illustration.Article{Title: "T", Excerpt: "E"}, nil)
	if !errors.Is(err, illustration.ErrNoCompliantImage) {
		t.Fatalf("err = %v, want ErrNoCompliantImage", err)
	}
	if model.generateCalls != 3 {
		t.Fatalf("generateCalls = %d, want 3", model.generateCalls)
	}
}

func TestGenerateReviewEmptyResponseRegeneratesAsUnverified(t *testing.T) {
	model := &fakeModel{t: t, generatedImage: tinyPNG(t), reviews: []scriptedReview{{err: gemini.ErrEmptyResponse}, {json: compliantJSON}}}
	_, err := illustration.NewGenerator(model, discardLogger()).Generate(context.Background(), illustration.Article{Title: "T", Excerpt: "E"}, nil)
	if err != nil || model.generateCalls != 2 {
		t.Fatalf("err = %v generateCalls = %d, want success on attempt 2", err, model.generateCalls)
	}
}

func TestGenerateKeepsReferenceOnInjuryRejection(t *testing.T) {
	model := &fakeModel{t: t, generatedImage: tinyPNG(t), reviews: []scriptedReview{{json: injuryViolationJSON}, {json: compliantJSON}}}
	_, err := illustration.NewGenerator(model, discardLogger()).Generate(context.Background(), illustration.Article{Title: "T", Excerpt: "E"}, tinyPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(model.referenceSeen) != 2 || !model.referenceSeen[0] || !model.referenceSeen[1] {
		t.Fatalf("referenceSeen = %v, want reference kept", model.referenceSeen)
	}
}

func TestGenerateUndecodableReferenceGeneratesFromText(t *testing.T) {
	model := &fakeModel{t: t, generatedImage: tinyPNG(t), reviews: []scriptedReview{{json: compliantJSON}}}
	_, err := illustration.NewGenerator(model, discardLogger()).Generate(context.Background(), illustration.Article{Title: "T", Excerpt: "E"}, []byte("not an image"))
	if err != nil {
		t.Fatal(err)
	}
	if len(model.referenceSeen) != 1 || model.referenceSeen[0] {
		t.Fatalf("referenceSeen = %v, want text-only", model.referenceSeen)
	}
}

func TestGenerateReturnsLastGenerationErrorOnFinalAttempt(t *testing.T) {
	genErr := errors.New("final boom")
	model := &fakeModel{t: t, generatedImage: tinyPNG(t), generateErrs: []error{nil, nil, genErr},
		reviews: []scriptedReview{{json: textViolationJSON}, {json: textViolationJSON}}}
	_, err := illustration.NewGenerator(model, discardLogger()).Generate(context.Background(), illustration.Article{Title: "T", Excerpt: "E"}, nil)
	if !errors.Is(err, genErr) {
		t.Fatalf("err = %v, want final generation error", err)
	}
}

func TestErrNoCompliantImageMentionsAttemptLimit(t *testing.T) {
	if !strings.Contains(illustration.ErrNoCompliantImage.Error(), "3 generation attempts") {
		t.Fatalf("ErrNoCompliantImage = %q", illustration.ErrNoCompliantImage)
	}
}

func TestFetchReference(t *testing.T) {
	png := tinyPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
		case "/svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte("<svg></svg>"))
		case "/html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html></html>"))
		case "/notfound":
			w.WriteHeader(http.StatusNotFound)
		case "/big":
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Length", "9000000")
			_, _ = w.Write(png)
		}
	}))
	defer server.Close()
	client := server.Client()

	if data, err := illustration.FetchReference(context.Background(), client, server.URL+"/png"); err != nil || !bytes.Equal(data, png) {
		t.Fatalf("png: data=%v err=%v", data, err)
	}
	if data, err := illustration.FetchReference(context.Background(), client, server.URL+"/svg"); err != nil || data != nil {
		t.Fatalf("svg: data=%v err=%v", data, err)
	}
	if data, err := illustration.FetchReference(context.Background(), client, server.URL+"/html"); err != nil || data != nil {
		t.Fatalf("html: data=%v err=%v", data, err)
	}
	if data, err := illustration.FetchReference(context.Background(), client, server.URL+"/notfound"); err != nil || data != nil {
		t.Fatalf("404: data=%v err=%v", data, err)
	}
	if data, err := illustration.FetchReference(context.Background(), client, server.URL+"/big"); err != nil || data != nil {
		t.Fatalf("big: data=%v err=%v", data, err)
	}
	if data, err := illustration.FetchReference(context.Background(), client, ""); err != nil || data != nil {
		t.Fatalf("empty: data=%v err=%v", data, err)
	}
}
