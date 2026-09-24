package illustration_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration/styles"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

type fakeAnalyzer struct {
	name  string
	brief string
	err   error
	calls int
	input illustration.AnalyzeInput
}

func (f *fakeAnalyzer) Name() string { return f.name }
func (f *fakeAnalyzer) Analyze(_ context.Context, input illustration.AnalyzeInput) (illustration.Brief, error) {
	f.calls++
	f.input = input
	if f.err != nil {
		return illustration.Brief{}, f.err
	}
	return illustration.ParseBrief(f.brief, styleNames)
}

type fakeProvider struct {
	name     string
	attempts int
	image    []byte
	errs     []error
	requests []illustration.GenerateRequest
}

func (f *fakeProvider) Name() string  { return f.name }
func (f *fakeProvider) Attempts() int { return f.attempts }
func (f *fakeProvider) Generate(_ context.Context, request illustration.GenerateRequest) ([]byte, error) {
	f.requests = append(f.requests, request)
	if i := len(f.requests) - 1; i < len(f.errs) && f.errs[i] != nil {
		return nil, f.errs[i]
	}
	return f.image, nil
}

// fakeReviewer answers from a script; after it runs out, every image is clean.
type fakeReviewer struct {
	mu       sync.Mutex
	verdicts []illustration.Verdict
	err      error
	reviewed [][]byte
}

func (f *fakeReviewer) Review(_ context.Context, _ illustration.Article, image []byte) (illustration.Verdict, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reviewed = append(f.reviewed, image)
	if f.err != nil {
		return illustration.Verdict{}, f.err
	}
	if len(f.verdicts) == 0 {
		return illustration.Verdict{Compliant: true}, nil
	}
	verdict := f.verdicts[0]
	f.verdicts = f.verdicts[1:]
	return verdict, nil
}

type fakeCutter struct {
	cutout []byte
	err    error
	calls  int
}

func (f *fakeCutter) Cutout(context.Context, []byte) ([]byte, error) {
	f.calls++
	return f.cutout, f.err
}

type fakeImageStore struct {
	stored  []byte
	deleted []string
	err     error
}

func (f *fakeImageStore) StoreWebP(_ context.Context, slug string, data []byte) (media.Stored, error) {
	if f.err != nil {
		return media.Stored{}, f.err
	}
	f.stored = data
	return media.Stored{Key: "features/" + slug + ".webp", URL: "https://img.test/features/" + slug + ".webp"}, nil
}

func (f *fakeImageStore) Delete(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	return nil
}

var rejectText = illustration.Verdict{HasText: true, Notes: "text"}

func sourceServer(t *testing.T, image []byte) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(image)
	}))
	t.Cleanup(server.Close)
	return server.URL + "/source.png"
}

type pipelineFixture struct {
	deps     illustration.PipelineDeps
	reviewer *fakeReviewer
	analyzer *fakeAnalyzer
	request  publisher.IllustrationRequest
}

func newFixture(t *testing.T, chain ...illustration.Step) *pipelineFixture {
	t.Helper()
	reviewer := &fakeReviewer{}
	analyzer := &fakeAnalyzer{name: "codex", brief: validBrief}
	return &pipelineFixture{
		deps: illustration.PipelineDeps{
			Chain:     chain,
			Analyzers: []illustration.Analyzer{analyzer},
			Reviewer:  reviewer,
			Styles:    styles.MustLoad(),
			HTTP:      illustration.NewReferenceClientAllowingAnyAddress(),
			Logger:    discardLogger(),
		},
		reviewer: reviewer,
		analyzer: analyzer,
		request:  publisher.IllustrationRequest{Slug: "slug", Title: "T", Excerpt: "E"},
	}
}

func (f *pipelineFixture) produce(t *testing.T) (illustration.Result, error) {
	t.Helper()
	return illustration.NewPipeline(f.deps).Produce(context.Background(), f.request)
}

func TestPipelineFirstProviderWins(t *testing.T) {
	codex := &fakeProvider{name: "codex", attempts: 2, image: solidPNG(t, 64, 36)}
	gem := &fakeProvider{name: "gemini", attempts: 3, image: solidPNG(t, 64, 36)}
	fixture := newFixture(t, illustration.ProviderStep(codex), illustration.ProviderStep(gem), illustration.SourceStep())
	result, err := fixture.produce(t)
	if err != nil {
		t.Fatal(err)
	}
	report := result.Report
	if report.Provider != "codex" || report.Analyzer != "codex" || report.Style != "gouache@1" || report.Collage || len(gem.requests) != 0 {
		t.Fatalf("report = %+v gemini calls = %d", report, len(gem.requests))
	}
	if len(fixture.reviewer.reviewed) != 1 {
		t.Fatalf("reviewed %d images", len(fixture.reviewer.reviewed))
	}
	if codex.requests[0].Style.Name != "gouache" || codex.requests[0].Collage {
		t.Fatalf("request = %+v", codex.requests[0])
	}
}

func TestPipelineFallsThroughInChainOrder(t *testing.T) {
	codex := &fakeProvider{name: "codex", attempts: 2, errs: []error{errors.New("timeout"), errors.New("no image")}}
	gem := &fakeProvider{name: "gemini", attempts: 3, image: solidPNG(t, 64, 36)}
	fixture := newFixture(t, illustration.ProviderStep(codex), illustration.ProviderStep(gem), illustration.SourceStep())
	fixture.reviewer.verdicts = []illustration.Verdict{rejectText}
	result, err := fixture.produce(t)
	if err != nil {
		t.Fatal(err)
	}
	if len(codex.requests) != 2 || len(gem.requests) != 2 || result.Report.Provider != "gemini" {
		t.Fatalf("codex %d gemini %d report %+v", len(codex.requests), len(gem.requests), result.Report)
	}
	if gem.requests[1].Correction != illustration.TextViolationCorrection {
		t.Fatalf("second attempt correction = %q", gem.requests[1].Correction)
	}
	if !strings.Contains(illustration.BuildImagePrompt(gem.requests[1]), "Correction: "+illustration.TextViolationCorrection) {
		t.Fatal("correction missing from prompt")
	}
}

func TestPipelineFallsBackToSourceAfterRejections(t *testing.T) {
	gem := &fakeProvider{name: "gemini", attempts: 3, image: solidPNG(t, 64, 36)}
	fixture := newFixture(t, illustration.ProviderStep(gem), illustration.SourceStep())
	fixture.reviewer.verdicts = []illustration.Verdict{rejectText, rejectText, rejectText}
	source := solidPNG(t, 40, 30)
	fixture.request.ReferenceImageURL = sourceServer(t, source)
	result, err := fixture.produce(t)
	if err != nil {
		t.Fatal(err)
	}
	if result.Report.Provider != "source" || !bytes.Equal(result.Image, source) || len(gem.requests) != 3 {
		t.Fatalf("report %+v gemini calls %d", result.Report, len(gem.requests))
	}
	if fixture.analyzer.input.Source == nil {
		t.Fatal("the analyzer should see the source image")
	}
}

func TestPipelineRejectionsOnlyIsPermanent(t *testing.T) {
	gem := &fakeProvider{name: "gemini", attempts: 3, image: solidPNG(t, 64, 36)}
	fixture := newFixture(t, illustration.ProviderStep(gem), illustration.SourceStep())
	fixture.reviewer.verdicts = []illustration.Verdict{rejectText, rejectText, rejectText}
	_, err := fixture.produce(t)
	if !publisher.IsPermanent(err) || !errors.Is(err, illustration.ErrNoCompliantImage) || !errors.Is(err, illustration.ErrNoSourceImage) {
		t.Fatalf("err = %v, want permanent", err)
	}
}

func TestPipelineTransientFailureIsRetriedLater(t *testing.T) {
	codex := &fakeProvider{name: "codex", attempts: 2, errs: []error{errors.New("timeout"), errors.New("timeout")}}
	gem := &fakeProvider{name: "gemini", attempts: 3, image: solidPNG(t, 64, 36)}
	fixture := newFixture(t, illustration.ProviderStep(codex), illustration.ProviderStep(gem))
	fixture.reviewer.verdicts = []illustration.Verdict{rejectText, rejectText, rejectText}
	_, err := fixture.produce(t)
	if err == nil || publisher.IsPermanent(err) || publisher.IsSystemFault(err) {
		t.Fatalf("err = %v, want transient", err)
	}
}

func TestPipelineReviewerOutageStopsTheProvider(t *testing.T) {
	gem := &fakeProvider{name: "gemini", attempts: 3, image: solidPNG(t, 64, 36)}
	fixture := newFixture(t, illustration.ProviderStep(gem))
	fixture.reviewer.err = publisher.SystemFault(&gemini.Error{Status: 403})
	_, err := fixture.produce(t)
	if !publisher.IsSystemFault(err) || len(gem.requests) != 1 {
		t.Fatalf("err = %v gemini calls %d", err, len(gem.requests))
	}
}

func TestPipelineGeminiBlockIsPermanentForThatProvider(t *testing.T) {
	gem := &fakeProvider{name: "gemini", attempts: 3, errs: []error{gemini.ErrBlocked}}
	fixture := newFixture(t, illustration.ProviderStep(gem))
	_, err := fixture.produce(t)
	if !publisher.IsPermanent(err) || len(gem.requests) != 1 {
		t.Fatalf("err = %v gemini calls %d", err, len(gem.requests))
	}
}

func TestPipelineAnalyzerFallback(t *testing.T) {
	gem := &fakeProvider{name: "gemini", attempts: 3, image: solidPNG(t, 64, 36)}
	fixture := newFixture(t, illustration.ProviderStep(gem))
	first := &fakeAnalyzer{name: "codex", err: illustration.ErrCodexAuth}
	second := &fakeAnalyzer{name: "gemini", brief: strings.Replace(validBrief, "gouache", "anime", 1)}
	fixture.deps.Analyzers = []illustration.Analyzer{first, second}
	result, err := fixture.produce(t)
	if err != nil {
		t.Fatal(err)
	}
	if result.Report.Analyzer != "gemini" || result.Report.Style != "anime@1" || first.calls != 1 {
		t.Fatalf("report = %+v", result.Report)
	}
}

func TestPipelineWithoutBriefUsesOnlySource(t *testing.T) {
	gem := &fakeProvider{name: "gemini", attempts: 3, image: solidPNG(t, 64, 36)}
	fixture := newFixture(t, illustration.ProviderStep(gem), illustration.SourceStep())
	fixture.analyzer.err = errors.New("analyzer down")
	fixture.request.ReferenceImageURL = sourceServer(t, solidPNG(t, 40, 30))
	result, err := fixture.produce(t)
	if err != nil || result.Report.Provider != "source" || len(gem.requests) != 0 {
		t.Fatalf("err = %v report = %+v", err, result.Report)
	}
	fixture.request.ReferenceImageURL = ""
	if _, err := fixture.produce(t); err == nil || publisher.IsPermanent(err) {
		t.Fatalf("no brief and no source: err = %v, want transient", err)
	}
}

func TestPipelineSourceOnlyChainSkipsAnalysis(t *testing.T) {
	fixture := newFixture(t, illustration.SourceStep())
	fixture.request.ReferenceImageURL = sourceServer(t, solidPNG(t, 40, 30))
	result, err := fixture.produce(t)
	if err != nil || result.Report.Provider != "source" || fixture.analyzer.calls != 0 {
		t.Fatalf("err = %v report = %+v analyzer calls %d", err, result.Report, fixture.analyzer.calls)
	}
	fixture.request.ReferenceImageURL = ""
	if _, err := fixture.produce(t); !publisher.IsPermanent(err) || !errors.Is(err, illustration.ErrNoSourceImage) {
		t.Fatalf("no source: err = %v, want permanent", err)
	}
}

const figureBrief = `{"scene":"A helix.","foreground":"A glowing rung.","background":"Stars.","mood":"Curious","style":"anime","style_reason":"Epic.","public_figure":{"name":"Dario Amodei","visible_in_source":true}}`

func TestPipelineCollage(t *testing.T) {
	gem := &fakeProvider{name: "gemini", attempts: 3, image: solidPNG(t, 320, 180)}
	fixture := newFixture(t, illustration.ProviderStep(gem))
	fixture.analyzer.brief = figureBrief
	fixture.request.ReferenceImageURL = sourceServer(t, solidPNG(t, 200, 200))
	cutter := &fakeCutter{cutout: personCutout(t, 200, 200)}
	fixture.deps.Cutter = cutter
	result, err := fixture.produce(t)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Report.Collage || cutter.calls != 1 || !gem.requests[0].Collage {
		t.Fatalf("report = %+v", result.Report)
	}
	if !strings.Contains(illustration.BuildImagePrompt(gem.requests[0]), "LEFT 45%") {
		t.Fatal("collage prompt must keep the left side free")
	}
	// The reviewer saw the background, not the composite.
	if bytes.Equal(fixture.reviewer.reviewed[0], result.Image) {
		t.Fatal("the composite must not be what was reviewed")
	}
	final, err := png.Decode(bytes.NewReader(result.Image))
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, _ := final.At(77, 170).RGBA(); r>>8 != 200 || g>>8 != 40 || b>>8 != 40 {
		t.Fatalf("person pixel = %d,%d,%d", r>>8, g>>8, b>>8)
	}
}

func TestPipelineCollageFallsBackOnPoorCutout(t *testing.T) {
	for name, cutter := range map[string]*fakeCutter{
		"tiny mask": {cutout: tinyMaskCutout(t)},
		"error":     {err: errors.New("no subject found")},
	} {
		gem := &fakeProvider{name: "gemini", attempts: 3, image: solidPNG(t, 320, 180)}
		fixture := newFixture(t, illustration.ProviderStep(gem))
		fixture.analyzer.brief = figureBrief
		fixture.request.ReferenceImageURL = sourceServer(t, solidPNG(t, 200, 200))
		fixture.deps.Cutter = cutter
		result, err := fixture.produce(t)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if result.Report.Collage || gem.requests[0].Collage || result.Report.CollageNote == "" {
			t.Fatalf("%s: report = %+v", name, result.Report)
		}
	}
}

func TestPipelineIllustrateStoresWebPAndDiscards(t *testing.T) {
	gem := &fakeProvider{name: "gemini", attempts: 3, image: solidPNG(t, 64, 36)}
	fixture := newFixture(t, illustration.ProviderStep(gem))
	store := &fakeImageStore{}
	fixture.deps.Store = store
	illustrationResult, err := illustration.NewPipeline(fixture.deps).Illustrate(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if illustrationResult.URL != "https://img.test/features/slug.webp" || !bytes.HasPrefix(store.stored, []byte("RIFF")) {
		t.Fatalf("url %q stored %d bytes", illustrationResult.URL, len(store.stored))
	}
	illustrationResult.Discard(context.Background())
	if len(store.deleted) != 1 {
		t.Fatalf("deleted = %v", store.deleted)
	}
}

func TestPipelineCropsTo16x9(t *testing.T) {
	gem := &fakeProvider{name: "gemini", attempts: 3, image: solidPNG(t, 300, 200)}
	result, err := newFixture(t, illustration.ProviderStep(gem)).produce(t)
	if err != nil {
		t.Fatal(err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(result.Image))
	if err != nil || config.Width != 300 || config.Height != 168 {
		t.Fatalf("final %dx%d err %v", config.Width, config.Height, err)
	}
}

// personCutout is a transparent image with an opaque red "person": a head
// and shoulders block touching the bottom edge.
func personCutout(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	red := color.NRGBA{R: 200, G: 40, B: 40, A: 255}
	for y := height / 5; y < height; y++ {
		for x := width / 4; x < width*3/4; x++ {
			img.Set(x, y, red)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func tinyMaskCutout(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 200, 200))
	for y := 90; y < 100; y++ {
		for x := 90; x < 100; x++ {
			img.Set(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
