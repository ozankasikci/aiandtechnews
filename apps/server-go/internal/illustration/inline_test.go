package illustration_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration/styles"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

type fakeDrawnCheck struct {
	answer string
	err    error
	calls  int
	prompt string
}

func (f *fakeDrawnCheck) ReviewImage(_ context.Context, prompt string, _ []byte, _ map[string]any) (string, error) {
	f.calls++
	f.prompt = prompt
	return f.answer, f.err
}

type fakeHistory struct{ recorded []illustration.Choice }

func (f *fakeHistory) Recent(context.Context) ([]illustration.Choice, error) { return nil, nil }
func (f *fakeHistory) Record(_ context.Context, choice illustration.Choice) error {
	f.recorded = append(f.recorded, choice)
	return nil
}

const drawnAnswer = `{"drawn_illustration":true,"notes":"a flat vector illustration"}`

const inlineBrief = `{"scene":"Engineers wheel a new rack into a data hall. Cables glow.","foreground":"Two engineers push the rack.","background":"Rows of servers.","mood":"Busy","style":"graphic","style_reason":"Fits.","public_figure":{"name":"Jane Doe","visible_in_source":true},"brands":["openai"],"alt":"Two engineers push a new server rack into a long data hall."}`

// patternPNG draws diagonal bands, so its average hash is not flat.
func patternPNG(t *testing.T, width, height, phase int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			value := uint8(0)
			// Bands in normalized coordinates, so a scaled copy matches.
			band := int((float64(x)/float64(width)+2*float64(y)/float64(height))*4 + float64(phase)/10)
			if band%2 == 0 {
				value = 230
			}
			img.Set(x, y, color.NRGBA{R: value, G: value / 2, B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type inlineFixture struct {
	deps     illustration.PipelineDeps
	vision   *fakeDrawnCheck
	analyzer *fakeAnalyzer
	history  *fakeHistory
	store    *fakeImageStore
	provider *fakeProvider
	request  illustration.InlineRequest
}

func newInlineFixture(t *testing.T) *inlineFixture {
	t.Helper()
	fixture := newFixture(t)
	provider := &fakeProvider{name: "codex", attempts: 2, image: solidPNG(t, 64, 36)}
	vision := &fakeDrawnCheck{answer: drawnAnswer}
	analyzer := &fakeAnalyzer{name: "codex", brief: inlineBrief}
	history := &fakeHistory{}
	store := &fakeImageStore{}
	deps := fixture.deps
	deps.Chain = []illustration.Step{illustration.SourceStep(), illustration.ProviderStep(provider)}
	deps.Analyzers = []illustration.Analyzer{analyzer}
	deps.Vision = vision
	deps.History = history
	deps.Store = store
	return &inlineFixture{
		deps: deps, vision: vision, analyzer: analyzer, history: history, store: store, provider: provider,
		request: illustration.InlineRequest{
			Slug: "big-news", Title: "Big news", Section: "The racks arrive.\n\nEngineers install them.",
			FeaturedImageURL: sourceServer(t, patternPNG(t, 160, 90, 0)),
		},
	}
}

func TestIllustrateInlineDrawsTheSectionInTheFeaturedStyle(t *testing.T) {
	fixture := newInlineFixture(t)
	result, err := illustration.NewPipeline(fixture.deps).IllustrateInline(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if result.URL != "https://img.test/features/big-news-inline.webp" || result.Alt != "Two engineers push a new server rack into a long data hall." || result.Discard == nil {
		t.Fatalf("result = %+v", result)
	}
	if imaging.SniffMIME(fixture.store.stored) != "image/webp" {
		t.Fatal("the inline image is not stored as WebP")
	}
	if len(fixture.history.recorded) != 0 {
		t.Fatalf("inline image recorded in the featured history: %+v", fixture.history.recorded)
	}
	input := fixture.analyzer.input
	if input.Section != fixture.request.Section || input.Title != "Big news" || input.Source == nil || len(input.Brands) != 0 || len(input.Palettes) != 0 {
		t.Fatalf("analyzer input = %+v", input)
	}
	if input.Styles == nil || strings.Contains(strings.Join(input.Styles.Names(), ","), "collage") {
		t.Fatalf("inline styles must exclude collage: %v", input.Styles)
	}
	if len(fixture.provider.requests) != 1 {
		t.Fatalf("%d generations", len(fixture.provider.requests))
	}
	request := fixture.provider.requests[0]
	if request.StyleReference == nil || imaging.SniffMIME(request.StyleReference) != "image/jpeg" || len(request.Logos) != 0 || request.Collage {
		t.Fatalf("generate request = %+v", request)
	}
	if request.Brief.PublicFigure != nil || len(request.Brief.Brands) != 0 {
		t.Fatalf("brief keeps a public figure or brands: %+v", request.Brief)
	}
	prompt := illustration.BuildImagePrompt(request)
	if !strings.Contains(prompt, illustration.ReferenceStyleRules) || strings.Contains(prompt, "Colour palette:") || strings.Contains(prompt, " Style: "+request.Style.Prompt) {
		t.Fatalf("prompt = %s", prompt)
	}
	if fixture.vision.calls != 1 {
		t.Fatalf("drawn check ran %d times", fixture.vision.calls)
	}
}

func TestProduceInlineRefusesAFeaturedImageThatIsNotDrawn(t *testing.T) {
	fixture := newInlineFixture(t)
	fixture.vision.answer = `{"drawn_illustration":false,"notes":"a press photo"}`
	_, err := illustration.NewPipeline(fixture.deps).ProduceInline(context.Background(), fixture.request)
	if !errors.Is(err, illustration.ErrNotOurImage) || !publisher.IsPermanent(err) {
		t.Fatalf("err = %v", err)
	}
	if fixture.analyzer.calls != 0 || len(fixture.provider.requests) != 0 {
		t.Fatal("nothing may be generated from a photo")
	}
}

func TestProduceInlineRefusesACopiedSourceImage(t *testing.T) {
	fixture := newInlineFixture(t)
	// The same picture at another size, as the source step would have stored it.
	fixture.request.SourceImageURL = sourceServer(t, patternPNG(t, 320, 180, 0))
	_, err := illustration.NewPipeline(fixture.deps).ProduceInline(context.Background(), fixture.request)
	if !errors.Is(err, illustration.ErrNotOurImage) || !publisher.IsPermanent(err) {
		t.Fatalf("err = %v", err)
	}
	if fixture.vision.calls != 0 || fixture.analyzer.calls != 0 {
		t.Fatal("a copied source image must be refused before any model call")
	}
}

func TestProduceInlineAcceptsADifferentSourceImage(t *testing.T) {
	fixture := newInlineFixture(t)
	fixture.request.SourceImageURL = sourceServer(t, patternPNG(t, 160, 120, 7))
	if _, err := illustration.NewPipeline(fixture.deps).ProduceInline(context.Background(), fixture.request); err != nil {
		t.Fatal(err)
	}
}

func TestProduceInlineFailsClosedWithoutAVisionAnswer(t *testing.T) {
	fixture := newInlineFixture(t)
	fixture.vision.answer = "not json"
	_, err := illustration.NewPipeline(fixture.deps).ProduceInline(context.Background(), fixture.request)
	if err == nil || fixture.analyzer.calls != 0 {
		t.Fatalf("err = %v, analyzer calls %d", err, fixture.analyzer.calls)
	}
	fixture.deps.Vision = nil
	if _, err := illustration.NewPipeline(fixture.deps).ProduceInline(context.Background(), fixture.request); !errors.Is(err, illustration.ErrNotOurImage) {
		t.Fatalf("no vision model: err = %v", err)
	}
}

func TestProduceInlineFallsBackToTheSceneForAlt(t *testing.T) {
	fixture := newInlineFixture(t)
	fixture.analyzer.brief = strings.Replace(inlineBrief, `,"alt":"Two engineers push a new server rack into a long data hall."`, "", 1)
	result, err := illustration.NewPipeline(fixture.deps).ProduceInline(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Alt != "Engineers wheel a new rack into a data hall." {
		t.Fatalf("alt = %q", result.Alt)
	}
}

func TestProduceInlineRejectionsAreRetriedWithCorrections(t *testing.T) {
	fixture := newInlineFixture(t)
	reviewer := &fakeReviewer{verdicts: []illustration.Verdict{rejectText}}
	fixture.deps.Reviewer = reviewer
	if _, err := illustration.NewPipeline(fixture.deps).ProduceInline(context.Background(), fixture.request); err != nil {
		t.Fatal(err)
	}
	if len(reviewer.reviewed) != 2 || fixture.provider.requests[1].Correction != illustration.TextViolationCorrection || fixture.provider.requests[1].StyleReference == nil {
		t.Fatalf("reviewed %d, requests %+v", len(reviewer.reviewed), fixture.provider.requests)
	}
}

func TestInlineSlugKeepsTheMarker(t *testing.T) {
	long := strings.Repeat("word-", 40)
	if got := illustration.InlineSlug(long); !strings.HasSuffix(got, "-inline") || len(got) > 120 {
		t.Fatalf("InlineSlug = %q", got)
	}
	if got := illustration.InlineSlug("Big News!"); got != "big-news-inline" {
		t.Fatalf("InlineSlug = %q", got)
	}
}

func TestBuildInlineAnalyzePrompt(t *testing.T) {
	catalog := styles.MustLoad()
	prompt := illustration.BuildInlineAnalyzePrompt("Headline X", "Passage Y.", catalog, true, illustration.Compositions, illustration.Shots)
	for _, want := range []string{"SECOND, ORIGINAL illustration", "Headline (context only): Headline X", "Passage:\nPassage Y.", "featured illustration", `"alt"`, `"shot"`, "Return only a JSON object"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	for _, unwanted := range []string{`"palette"`, `"brands"`, "news source's own image"} {
		if strings.Contains(prompt, unwanted) {
			t.Errorf("prompt has %q", unwanted)
		}
	}
	featured := illustration.BuildAnalyzePrompt("H", "S", "", catalog, false, nil, nil, "", nil, nil)
	if strings.Contains(featured, `"alt"`) || !strings.HasPrefix(featured, "You are the art director of an AI and technology news site. Write the brief for an ORIGINAL featured illustration of this story.\n\nHeadline: H\nSummary: S\n\nNo source image is available; work from the headline and summary.\n\nReturn only a JSON object") {
		t.Fatalf("featured prompt changed: %s", featured[:300])
	}
}

func TestParseDrawnCheck(t *testing.T) {
	for raw, want := range map[string][3]any{
		drawnAnswer: {true, "a flat vector illustration", true},
		"```json\n{\"drawn_illustration\":false,\"notes\":\"photo\"}\n```": {false, "photo", true},
		`{"notes":"x"}`: {false, "", false},
		"nope":          {false, "", false},
	} {
		drawn, notes, ok := illustration.ParseDrawnCheck(raw)
		if drawn != want[0] || notes != want[1] || ok != want[2] {
			t.Errorf("ParseDrawnCheck(%q) = %t %q %t", raw, drawn, notes, ok)
		}
	}
}

func TestSameImage(t *testing.T) {
	decode := func(data []byte) image.Image {
		img, err := imaging.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	a := decode(patternPNG(t, 160, 90, 0))
	if !illustration.SameImage(a, decode(patternPNG(t, 320, 180, 0))) {
		t.Fatal("a scaled copy is the same image")
	}
	if illustration.SameImage(a, decode(patternPNG(t, 160, 120, 0))) {
		t.Fatal("another aspect ratio is not the same image")
	}
	if illustration.SameImage(a, decode(patternPNG(t, 160, 90, 11))) {
		t.Fatal("a shifted pattern is not the same image")
	}
}

func TestCodexProviderAttachesOnlyTheStyleReference(t *testing.T) {
	harness := newFakeCodex(t, "write")
	request := sampleRequest(t)
	request.StyleReference = []byte("jpeg bytes")
	if _, err := illustration.NewCodexProvider(harness.runner).Generate(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	args := harness.args(t)
	var images []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "--image=") {
			images = append(images, arg)
		}
	}
	if len(images) != 1 || !strings.HasSuffix(images[0], "style-reference-featured.jpg") {
		t.Fatalf("images = %q", images)
	}
	prompt := args[len(args)-1]
	if !strings.Contains(prompt, illustration.ReferenceStyleRules) || strings.Contains(prompt, "style references only") || strings.Contains(prompt, "Style: Bold flat vector") {
		t.Fatalf("prompt = %s", prompt)
	}
}

type fakeImageModel struct{ reference *gemini.InlineImage }

func (f *fakeImageModel) GenerateImage(_ context.Context, _ string, reference *gemini.InlineImage) ([]byte, error) {
	f.reference = reference
	return []byte("png"), nil
}

func TestGeminiProviderSendsTheStyleReference(t *testing.T) {
	model := &fakeImageModel{}
	request := sampleRequest(t)
	if _, err := illustration.NewGeminiProvider(model).Generate(context.Background(), request); err != nil || model.reference != nil {
		t.Fatalf("featured: err %v reference %v", err, model.reference)
	}
	request.StyleReference = []byte("jpeg bytes")
	if _, err := illustration.NewGeminiProvider(model).Generate(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if model.reference == nil || model.reference.MIMEType != "image/jpeg" || string(model.reference.Data) != "jpeg bytes" {
		t.Fatalf("reference = %+v", model.reference)
	}
}
