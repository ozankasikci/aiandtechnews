package illustration

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"image"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration/brands"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration/styles"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

const (
	analyzeMaxEdge = 1024
	analyzeQuality = 85
)

// ErrNoCompliantImage means every generation a provider was allowed was
// rejected by the compliance review.
var ErrNoCompliantImage = errors.New("no compliant illustration")

// ErrNoSourceImage reports that the article has no usable original image
// for the "source" step.
var ErrNoSourceImage = errors.New("no usable source image")

// ErrNoBrief means no analyzer produced a usable brief, so no generating
// provider could run.
var ErrNoBrief = errors.New("no analyzer produced a brief")

// Step is one entry of FEATURED_IMAGE_CHAIN: a generating provider, or nil
// Provider for "source" (copy the source image).
type Step struct {
	Name     string
	Provider Provider
}

// SourceStep is the chain step that publishes the source image itself.
func SourceStep() Step { return Step{Name: ProviderSource} }

// ProviderStep wraps a generating provider.
func ProviderStep(provider Provider) Step { return Step{Name: provider.Name(), Provider: provider} }

// PipelineDeps wires a Pipeline.
type PipelineDeps struct {
	Chain     []Step
	Analyzers []Analyzer // tried in order until one returns a valid brief
	Reviewer  Reviewer
	Cutter    Cutter // nil disables the public-figure collage
	Styles    styles.Catalog
	Brands    brands.Catalog // logos the brief may ask for; empty disables logos
	Store     ImageStore     // only Illustrate uses it
	History   History        // only Illustrate uses it; nil disables variety
	// Vision checks that an inline image's style reference is one of our
	// drawn illustrations; nil makes every inline image fail that check.
	Vision VisionModel
	HTTP   *http.Client
	Logger *slog.Logger
	Now    func() time.Time
}

// Pipeline implements publisher.Illustrator: analyze the story, then try
// each provider of the chain in order until one yields a compliant image.
type Pipeline struct {
	deps PipelineDeps
	// busy lets one image (featured or inline) be made at a time, so the
	// background inline worker never runs Codex image generations alongside
	// the publisher's.
	busy chan struct{}
}

func NewPipeline(deps PipelineDeps) *Pipeline {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &Pipeline{deps: deps, busy: make(chan struct{}, 1)}
}

// acquire waits for the pipeline to be free; release frees it.
func (p *Pipeline) acquire(ctx context.Context) error {
	select {
	case p.busy <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Pipeline) release() { <-p.busy }

// Attempt records one generation or step outcome for logs and reports.
type Attempt struct {
	Provider string  `json:"provider"`
	Attempt  int     `json:"attempt,omitempty"`
	Outcome  string  `json:"outcome"`
	Seconds  float64 `json:"seconds"`
}

// Report says how an image was made.
type Report struct {
	Provider    string    `json:"provider,omitempty"`
	Style       string    `json:"style,omitempty"`
	Palette     string    `json:"palette,omitempty"`
	Composition string    `json:"composition,omitempty"`
	Shot        string    `json:"shot,omitempty"`
	Brands      []string  `json:"brands,omitempty"`
	LogoBadge   bool      `json:"logo_badge,omitempty"`
	StyleReason string    `json:"style_reason,omitempty"`
	Analyzer    string    `json:"analyzer,omitempty"`
	Brief       *Brief    `json:"brief,omitempty"`
	Collage     bool      `json:"collage"`
	CollageNote string    `json:"collage_note,omitempty"`
	SourceImage bool      `json:"source_image"`
	Attempts    []Attempt `json:"attempts"`
	Errors      []string  `json:"errors,omitempty"`
	Seconds     float64   `json:"seconds"`
	Width       int       `json:"width,omitempty"`
	Height      int       `json:"height,omitempty"`
}

// Result is the final image (PNG, or the source's own bytes) and its report.
type Result struct {
	Image  []byte
	Report Report
}

// Illustrate produces the image, encodes it as WebP and stores it.
func (p *Pipeline) Illustrate(ctx context.Context, request publisher.IllustrationRequest) (publisher.Illustration, error) {
	p.applyVariety(ctx, &request)
	result, err := p.Produce(ctx, request)
	if err != nil {
		return publisher.Illustration{}, err
	}
	webp, _, _, err := imaging.EncodeWebPMinWidth(result.Image, webpQuality, minFeatureWidth)
	if err != nil {
		return publisher.Illustration{}, publisher.Permanent(fmt.Errorf("encode featured image: %w", err))
	}
	illustration, err := storeWebP(ctx, p.deps.Store, request.Slug, webp, p.deps.Logger)
	if err != nil {
		return publisher.Illustration{}, fmt.Errorf("store featured image: %w", err)
	}
	p.recordChoice(ctx, result.Report)
	return illustration, nil
}

// applyVariety fills the request's avoid and prefer fields from the latest
// images, unless the caller set them. History errors only cost variety.
func (p *Pipeline) applyVariety(ctx context.Context, request *publisher.IllustrationRequest) {
	if p.deps.History == nil || len(request.AvoidStyles) > 0 || len(request.AvoidPalettes) > 0 || request.PreferComposition != "" || len(request.AvoidShots) > 0 {
		return
	}
	recent, err := p.deps.History.Recent(ctx)
	if err != nil {
		p.deps.Logger.WarnContext(ctx, "featured image history unavailable", "error", err)
		return
	}
	variety := VarietyFrom(recent)
	request.AvoidStyles, request.AvoidPalettes, request.PreferComposition, request.AvoidShots = variety.AvoidStyles, variety.AvoidPalettes, variety.PreferComposition, variety.AvoidShots
}

// recordChoice remembers a generated image's look; the source photo has none.
func (p *Pipeline) recordChoice(ctx context.Context, report Report) {
	if p.deps.History == nil || report.SourceImage || report.Brief == nil {
		return
	}
	style := report.Brief.Style
	if err := p.deps.History.Record(ctx, Choice{Style: style, Palette: report.Palette, Composition: report.Composition, Shot: report.Shot}); err != nil {
		p.deps.Logger.WarnContext(ctx, "could not record featured image history", "error", err)
	}
}

// Produce runs the pipeline without storing anything.
func (p *Pipeline) Produce(ctx context.Context, request publisher.IllustrationRequest) (Result, error) {
	if err := p.acquire(ctx); err != nil {
		return Result{}, err
	}
	defer p.release()
	started := p.deps.Now()
	run := &pipelineRun{p: p, request: request, report: &Report{}}
	image, err := run.produce(ctx)
	run.report.Seconds = p.deps.Now().Sub(started).Seconds()
	logger := p.deps.Logger.With("slug", request.Slug, "analyzer", run.report.Analyzer, "style", run.report.Style,
		"collage", run.report.Collage, "seconds", int(run.report.Seconds))
	if err != nil {
		logger.WarnContext(ctx, "featured image pipeline failed", "errors", run.report.Errors)
		return Result{Report: *run.report}, err
	}
	generations := 0
	for _, attempt := range run.report.Attempts {
		if attempt.Provider == run.report.Provider {
			generations++
		}
	}
	logger.InfoContext(ctx, "featured image ready", "provider", run.report.Provider, "generations", generations)
	return Result{Image: image, Report: *run.report}, nil
}

// AnalyzeOnly fetches the source image and runs the analyzers, without
// generating anything. It is for tuning briefs (cmd/imagegen-try -analyze-only).
func (p *Pipeline) AnalyzeOnly(ctx context.Context, request publisher.IllustrationRequest) (Report, error) {
	started := p.deps.Now()
	run := &pipelineRun{p: p, request: request, report: &Report{}}
	run.fetchSource(ctx)
	brief := run.analyze(ctx)
	run.report.Seconds = p.deps.Now().Sub(started).Seconds()
	if brief == nil {
		return *run.report, classifyChainFailure(run.errs)
	}
	return *run.report, nil
}

type pipelineRun struct {
	p       *Pipeline
	request publisher.IllustrationRequest
	report  *Report
	errs    []error
	source  []byte
	brands  []brands.Brand
	// allowedStyles are the styles offered to the analyzer (for fallbacks).
	allowedStyles styles.Catalog
	// styleReference is the featured image an inline image is drawn like.
	styleReference []byte
}

func (r *pipelineRun) fail(provider string, attempt int, started time.Time, err error) {
	r.errs = append(r.errs, fmt.Errorf("%s: %w", provider, err))
	r.report.Errors = append(r.report.Errors, fmt.Sprintf("%s: %v", provider, err))
	r.report.Attempts = append(r.report.Attempts, Attempt{Provider: provider, Attempt: attempt, Outcome: err.Error(), Seconds: r.p.deps.Now().Sub(started).Seconds()})
}

func (r *pipelineRun) produce(ctx context.Context) ([]byte, error) {
	deps := r.p.deps
	r.fetchSource(ctx)

	var brief *Brief
	for _, step := range deps.Chain {
		if step.Provider != nil {
			brief = r.analyze(ctx)
			break
		}
	}
	var person *image.NRGBA
	var parts *CollageParts
	if brief != nil {
		if style, _ := deps.Styles.Get(brief.Style); style.Collage {
			parts = r.prepareMixed(ctx, brief)
		} else {
			person = r.prepareCollage(ctx, *brief)
		}
	}

	for _, step := range deps.Chain {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if step.Provider == nil {
			if r.source == nil {
				r.fail(ProviderSource, 0, deps.Now(), ErrNoSourceImage)
				continue
			}
			r.report.Provider = ProviderSource
			r.report.SourceImage = true
			r.report.Collage = false
			r.report.Attempts = append(r.report.Attempts, Attempt{Provider: ProviderSource, Outcome: "ok"})
			return r.source, nil
		}
		if brief == nil {
			r.fail(step.Name, 0, deps.Now(), ErrNoBrief)
			continue
		}
		if image, ok := r.generate(ctx, step.Provider, *brief, person, parts); ok {
			return image, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, classifyChainFailure(r.errs)
}

func (r *pipelineRun) fetchSource(ctx context.Context) {
	deps := r.p.deps
	source, err := FetchReference(ctx, deps.HTTP, r.request.ReferenceImageURL)
	if err != nil {
		deps.Logger.InfoContext(ctx, "source image unusable", "url", r.request.ReferenceImageURL, "reason", err)
	}
	if source != nil {
		if _, err := imaging.Decode(source); err == nil {
			r.source = source
		} else {
			deps.Logger.InfoContext(ctx, "source image could not be decoded", "url", r.request.ReferenceImageURL, "error", err)
		}
	}
}

func (r *pipelineRun) analyze(ctx context.Context) *Brief {
	deps := r.p.deps
	avoid := r.request.AvoidStyles
	if r.source == nil {
		// A collage pastes pieces of the source photo; without one, never offer it.
		for _, style := range deps.Styles.All() {
			if style.Collage {
				avoid = append(slices.Clone(avoid), style.Name)
			}
		}
	}
	allowedStyles := deps.Styles.WithoutStyles(avoid)
	r.allowedStyles = allowedStyles
	input := AnalyzeInput{Title: r.request.Title, Excerpt: r.request.Excerpt, Palettes: deps.Styles.PalettesExcept(r.request.AvoidPalettes), Styles: &allowedStyles, Compositions: allowedCompositions(r.request.Compositions), PreferComposition: r.request.PreferComposition, Shots: shotsExcept(r.request.AvoidShots), Brands: deps.Brands.All()}
	if r.source != nil {
		input.ImageURL = r.request.ReferenceImageURL
		if normalized, err := imaging.FitJPEG(r.source, analyzeMaxEdge, analyzeQuality); err == nil {
			input.Source = normalized
		}
	}
	return r.runAnalyzers(ctx, input)
}

// runAnalyzers tries each analyzer in order and returns the first valid
// brief, with its palette, composition, framing and brands settled.
func (r *pipelineRun) runAnalyzers(ctx context.Context, input AnalyzeInput) *Brief {
	deps := r.p.deps
	for _, analyzer := range deps.Analyzers {
		started := deps.Now()
		brief, err := analyzer.Analyze(ctx, input)
		if err != nil {
			r.logCodexAuth(ctx, err)
			r.fail("analyze/"+analyzer.Name(), 0, started, classifyProviderError(analyzer.Name(), err))
			deps.Logger.WarnContext(ctx, "featured image analyzer failed", "analyzer", analyzer.Name(), "error", err)
			continue
		}
		style, _ := deps.Styles.Get(brief.Style)
		brief.Palette = choosePalette(brief.Palette, input.Palettes, r.request.Slug)
		r.report.Palette = brief.Palette
		brief.Composition = chooseComposition(brief.Composition, input.Compositions)
		r.report.Composition = brief.Composition
		brief.Shot = chooseShot(brief.Shot, input.Shots, r.request.Slug)
		r.report.Shot = brief.Shot
		r.brands = deps.Brands.Resolve(brief.Brands, MaxBrands)
		brief.Brands = nil
		for _, brand := range r.brands {
			brief.Brands = append(brief.Brands, brand.ID)
			r.report.Brands = append(r.report.Brands, brand.Name)
		}
		r.report.Analyzer = analyzer.Name()
		r.report.Brief = &brief
		r.report.Style = style.ID()
		r.report.StyleReason = brief.StyleReason
		r.report.Attempts = append(r.report.Attempts, Attempt{Provider: "analyze/" + analyzer.Name(), Outcome: "ok", Seconds: deps.Now().Sub(started).Seconds()})
		return &brief
	}
	return nil
}

func (r *pipelineRun) prepareCollage(ctx context.Context, brief Brief) *image.NRGBA {
	deps := r.p.deps
	switch {
	case !brief.WantsCollage():
		return nil
	case brief.Composition == CompositionSimple:
		r.report.CollageNote = "simple composition has no people"
		return nil
	case deps.Cutter == nil:
		r.report.CollageNote = "CUTOUT_BIN is not configured"
		return nil
	case r.source == nil:
		r.report.CollageNote = "no source image"
		return nil
	}
	started := deps.Now()
	cutout, err := deps.Cutter.Cutout(ctx, r.source)
	if err == nil {
		var person *image.NRGBA
		if person, err = PrepareCutout(cutout); err == nil {
			r.report.Attempts = append(r.report.Attempts, Attempt{Provider: "cutout", Outcome: "ok", Seconds: deps.Now().Sub(started).Seconds()})
			return person
		}
	}
	r.report.CollageNote = err.Error()
	r.report.Attempts = append(r.report.Attempts, Attempt{Provider: "cutout", Outcome: err.Error(), Seconds: deps.Now().Sub(started).Seconds()})
	deps.Logger.InfoContext(ctx, "public-figure collage skipped; using a plain illustration", "reason", err)
	return nil
}

// generate spends the provider's attempts; every image is reviewed before
// use. A collage's background is reviewed before the photo is pasted on.
func (r *pipelineRun) generate(ctx context.Context, provider Provider, brief Brief, person *image.NRGBA, parts *CollageParts) ([]byte, bool) {
	deps := r.p.deps
	style, _ := deps.Styles.Get(brief.Style)
	article := Article{Title: r.request.Title, Excerpt: r.request.Excerpt}
	var logos []brands.Brand
	placer, places := provider.(interface{ PlacesLogos() bool })
	// A collage pastes the real logos itself; its backdrop carries none.
	placesLogos := places && placer.PlacesLogos() || parts != nil
	if placesLogos && parts == nil {
		logos = r.brands
		for _, brand := range logos {
			article.Brands = append(article.Brands, brand.Name)
			if brand.Note != "" {
				article.BrandNotes = append(article.BrandNotes, brand.Note)
			}
		}
	}
	correction := ""
	rejected := false
	for attempt := 1; attempt <= provider.Attempts(); attempt++ {
		started := deps.Now()
		palette, _ := deps.Styles.Palette(brief.Palette)
		raw, err := provider.Generate(ctx, GenerateRequest{Brief: brief, Style: style, Palette: palette, Logos: logos, Collage: person != nil || parts != nil, Correction: correction, StyleReference: r.styleReference})
		if err != nil {
			if ctx.Err() != nil {
				return nil, false
			}
			r.logCodexAuth(ctx, err)
			classified := classifyProviderError(provider.Name(), err)
			r.fail(provider.Name(), attempt, started, classified)
			deps.Logger.WarnContext(ctx, "featured image generation failed", "provider", provider.Name(), "attempt", attempt, "error", err)
			if publisher.IsSystemFault(classified) || publisher.IsPermanent(classified) {
				return nil, false
			}
			continue
		}
		decoded, err := imaging.Decode(raw)
		if err != nil {
			r.fail(provider.Name(), attempt, started, fmt.Errorf("undecodable image: %w", err))
			continue
		}
		framed := CropTo16x9(decoded)
		normalized, err := encodePNG(framed)
		if err != nil {
			r.fail(provider.Name(), attempt, started, err)
			continue
		}
		verdict, err := deps.Reviewer.Review(ctx, article, normalized)
		if err != nil {
			// The reviewer is down or misconfigured: more generations cannot be checked.
			r.fail(provider.Name(), attempt, started, err)
			return nil, false
		}
		if !verdict.Compliant {
			rejected = true
			correction = Correction(verdict)
			if verdict.HasLogo && len(logos) > 0 && !verdict.HasText {
				correction = BrandLogoCorrection
			}
			r.report.Attempts = append(r.report.Attempts, Attempt{Provider: provider.Name(), Attempt: attempt, Outcome: "rejected: " + verdict.Notes, Seconds: deps.Now().Sub(started).Seconds()})
			deps.Logger.InfoContext(ctx, "featured image rejected by compliance check", "provider", provider.Name(), "attempt", attempt,
				"text", verdict.HasText, "logo", verdict.HasLogo, "flag", verdict.HasFlag, "person", verdict.HasPerson, "injury", verdict.HasInjury,
				"unverified", verdict.Unverified, "notes", verdict.Notes)
			continue
		}
		final := normalized
		if !placesLogos && len(r.brands) > 0 {
			framed = AddLogoBadges(framed, r.brands)
			if final, err = encodePNG(framed); err != nil {
				r.fail(provider.Name(), attempt, started, err)
				continue
			}
			r.report.LogoBadge = true
		}
		if parts != nil {
			palette, _ := deps.Styles.Palette(brief.Palette)
			if final, err = encodePNG(MixedCollage(framed, *parts, palette, r.request.Slug)); err != nil {
				r.fail(provider.Name(), attempt, started, err)
				continue
			}
			r.report.Collage = true
		}
		if person != nil {
			composite := Composite(framed, person)
			if final, err = encodePNG(composite); err != nil {
				r.fail(provider.Name(), attempt, started, err)
				continue
			}
			r.report.Collage = true
		}
		bounds := framed.Bounds()
		r.report.Width, r.report.Height = bounds.Dx(), bounds.Dy()
		r.report.Provider = provider.Name()
		r.report.Attempts = append(r.report.Attempts, Attempt{Provider: provider.Name(), Attempt: attempt, Outcome: "ok", Seconds: deps.Now().Sub(started).Seconds()})
		return final, true
	}
	if rejected {
		r.errs = append(r.errs, fmt.Errorf("%s: %w", provider.Name(), publisher.Permanent(ErrNoCompliantImage)))
		r.report.Errors = append(r.report.Errors, provider.Name()+": "+ErrNoCompliantImage.Error())
	}
	return nil, false
}

func (r *pipelineRun) logCodexAuth(ctx context.Context, err error) {
	if errors.Is(err, ErrCodexAuth) {
		r.p.deps.Logger.ErrorContext(ctx, CodexReloginHint, "error", err)
	}
}

// classifyProviderError sorts a generation failure: a Codex login problem is
// a system fault; Gemini errors use the publisher's Gemini classification;
// anything else (timeouts, codex exiting without an image) is transient.
func classifyProviderError(provider string, err error) error {
	switch {
	case errors.Is(err, ErrCodexAuth):
		return publisher.SystemFault(err)
	case provider == ProviderGemini:
		return publisher.ClassifyGeminiError(err)
	}
	return err
}

// classifyChainFailure picks the publisher error class for a chain in which
// every step failed: any system fault keeps the candidate's attempts; only
// all-permanent failures mark it failed; the rest is retried later.
func classifyChainFailure(errs []error) error {
	if len(errs) == 0 {
		return publisher.Permanent(errors.New("featured image chain is empty"))
	}
	joined := fmt.Errorf("featured image: %w", errors.Join(errs...))
	permanent := true
	for _, err := range errs {
		if publisher.IsSystemFault(err) || errors.Is(err, ErrCodexAuth) {
			return publisher.SystemFault(joined)
		}
		if !publisher.IsPermanent(err) && !errors.Is(err, ErrNoSourceImage) && !errors.Is(err, ErrNoBrief) {
			permanent = false
		}
	}
	if permanent {
		return publisher.Permanent(joined)
	}
	// Flatten so a permanent step error inside does not make the whole
	// failure look permanent to publisher.IsPermanent.
	return errors.New(joined.Error())
}

// choosePalette keeps the analyzer's palette when it is one of the allowed
// ones; otherwise it picks an allowed palette from the slug, so a missing or
// avoided choice still varies between articles. No palettes: "".
func choosePalette(chosen string, allowed []styles.Palette, slug string) string {
	if len(allowed) == 0 {
		return ""
	}
	for _, palette := range allowed {
		if palette.Name == chosen {
			return chosen
		}
	}
	sum := fnv.New32a()
	_, _ = sum.Write([]byte(slug))
	return allowed[int(sum.Sum32()%uint32(len(allowed)))].Name
}

// allowedCompositions keeps the requested compositions that exist; none
// requested (or none valid) allows every composition.
func allowedCompositions(requested []string) []string {
	var allowed []string
	for _, name := range Compositions {
		if slices.Contains(requested, name) {
			allowed = append(allowed, name)
		}
	}
	if len(allowed) == 0 {
		return slices.Clone(Compositions)
	}
	return allowed
}

// chooseComposition keeps the analyzer's composition when it is allowed,
// otherwise the first allowed one.
func chooseComposition(chosen string, allowed []string) string {
	if slices.Contains(allowed, chosen) {
		return chosen
	}
	return allowed[0]
}

// shotsExcept offers every framing not used recently; if all were, all.
func shotsExcept(avoid []string) []string {
	var allowed []string
	for _, name := range Shots {
		if !slices.Contains(avoid, name) {
			allowed = append(allowed, name)
		}
	}
	if len(allowed) == 0 {
		return slices.Clone(Shots)
	}
	return allowed
}

// chooseShot keeps the analyzer's framing when it is allowed, otherwise
// picks one of the allowed framings from the slug.
func chooseShot(chosen string, allowed []string, slug string) string {
	if slices.Contains(allowed, chosen) {
		return chosen
	}
	sum := fnv.New32a()
	_, _ = sum.Write([]byte(slug))
	return allowed[int(sum.Sum32()%uint32(len(allowed)))]
}

// prepareMixed gathers the real pieces for a collage-style image. With no
// cut-out and no logo there is nothing to paste, so the brief falls back to
// another offered style and nil is returned.
func (r *pipelineRun) prepareMixed(ctx context.Context, brief *Brief) *CollageParts {
	deps := r.p.deps
	parts := &CollageParts{Logos: r.brands}
	if r.source != nil {
		if decoded, err := imaging.Decode(r.source); err == nil {
			parts.Scrap = decoded
		}
		// Cut out only a well-known person the source shows: products and
		// robots cut badly (a dark visor vanishes), so they stay whole in the print.
		if deps.Cutter != nil && brief.WantsCollage() {
			started := deps.Now()
			cutout, err := deps.Cutter.Cutout(ctx, r.source)
			if err == nil {
				parts.Subject, err = PrepareSubject(cutout)
			}
			outcome := "ok"
			if err != nil {
				outcome = err.Error()
			}
			r.report.Attempts = append(r.report.Attempts, Attempt{Provider: "cutout", Outcome: outcome, Seconds: deps.Now().Sub(started).Seconds()})
		}
	}
	if parts.Subject != nil || len(parts.Logos) > 0 {
		return parts
	}
	for _, style := range r.allowedStyles.All() {
		if !style.Collage {
			brief.Style = style.Name
			r.report.Style = style.ID()
			r.report.CollageNote = "collage needs a cut-out or a logo; used " + style.Name
			deps.Logger.InfoContext(ctx, "collage style has nothing to paste; falling back", "style", style.Name)
			return nil
		}
	}
	fallback := deps.Styles.Default()
	brief.Style, r.report.Style = fallback.Name, fallback.ID()
	return nil
}
