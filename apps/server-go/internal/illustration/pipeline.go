package illustration

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"net/http"
	"time"

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
	Store     ImageStore // only Illustrate uses it
	HTTP      *http.Client
	Logger    *slog.Logger
	Now       func() time.Time
}

// Pipeline implements publisher.Illustrator: analyze the story, then try
// each provider of the chain in order until one yields a compliant image.
type Pipeline struct{ deps PipelineDeps }

func NewPipeline(deps PipelineDeps) *Pipeline {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &Pipeline{deps: deps}
}

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
	result, err := p.Produce(ctx, request)
	if err != nil {
		return publisher.Illustration{}, err
	}
	webp, _, _, err := imaging.EncodeWebP(result.Image, webpQuality)
	if err != nil {
		return publisher.Illustration{}, publisher.Permanent(fmt.Errorf("encode featured image: %w", err))
	}
	illustration, err := storeWebP(ctx, p.deps.Store, request.Slug, webp, p.deps.Logger)
	if err != nil {
		return publisher.Illustration{}, fmt.Errorf("store featured image: %w", err)
	}
	return illustration, nil
}

// Produce runs the pipeline without storing anything.
func (p *Pipeline) Produce(ctx context.Context, request publisher.IllustrationRequest) (Result, error) {
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
	if brief != nil {
		person = r.prepareCollage(ctx, *brief)
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
		if image, ok := r.generate(ctx, step.Provider, *brief, person); ok {
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
	input := AnalyzeInput{Title: r.request.Title, Excerpt: r.request.Excerpt}
	if r.source != nil {
		input.ImageURL = r.request.ReferenceImageURL
		if normalized, err := imaging.FitJPEG(r.source, analyzeMaxEdge, analyzeQuality); err == nil {
			input.Source = normalized
		}
	}
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
func (r *pipelineRun) generate(ctx context.Context, provider Provider, brief Brief, person *image.NRGBA) ([]byte, bool) {
	deps := r.p.deps
	style, _ := deps.Styles.Get(brief.Style)
	article := Article{Title: r.request.Title, Excerpt: r.request.Excerpt}
	correction := ""
	rejected := false
	for attempt := 1; attempt <= provider.Attempts(); attempt++ {
		started := deps.Now()
		raw, err := provider.Generate(ctx, GenerateRequest{Brief: brief, Style: style, Collage: person != nil, Correction: correction})
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
			r.report.Attempts = append(r.report.Attempts, Attempt{Provider: provider.Name(), Attempt: attempt, Outcome: "rejected: " + verdict.Notes, Seconds: deps.Now().Sub(started).Seconds()})
			deps.Logger.InfoContext(ctx, "featured image rejected by compliance check", "provider", provider.Name(), "attempt", attempt,
				"text", verdict.HasText, "logo", verdict.HasLogo, "flag", verdict.HasFlag, "person", verdict.HasPerson, "injury", verdict.HasInjury,
				"unverified", verdict.Unverified, "notes", verdict.Notes)
			continue
		}
		final := normalized
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
