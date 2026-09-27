package illustration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"math/bits"
	"strings"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

// referenceMaxEdge and referenceQuality size the featured image attached to
// the image model as the inline image's style reference.
const (
	referenceMaxEdge = 1536
	referenceQuality = 90
)

// ErrNotOurImage means the article's featured image is not one of our drawn
// illustrations (a copied source image, a photo or a collage with photo
// pieces), so it must not be used as a style reference.
var ErrNotOurImage = errors.New("featured image is not one of our illustrations")

// InlineRequest asks for a second illustration inside an article's body.
type InlineRequest struct {
	Slug  string
	Title string
	// Section is the text of the paragraphs around the image's place; the
	// scene is drawn from it, not from the headline.
	Section string
	// FeaturedImageURL is the article's featured image, attached as the
	// style reference.
	FeaturedImageURL string
	// SourceImageURL is the news source's own image, when known: a featured
	// image that looks the same is a copy of it and is refused.
	SourceImageURL string
}

// InlineResult is an inline image (PNG) and its alt text, not yet stored.
type InlineResult struct {
	Image  []byte
	Alt    string
	Report Report
}

// InlineIllustration is a stored inline image. Discard deletes it again.
type InlineIllustration struct {
	URL     string
	Alt     string
	Discard func(context.Context)
}

// IllustrateInline produces the inline image, encodes it as WebP like a
// featured image and stores it under "<slug>-inline". It does not touch the
// featured image variety history.
func (p *Pipeline) IllustrateInline(ctx context.Context, request InlineRequest) (InlineIllustration, error) {
	result, err := p.ProduceInline(ctx, request)
	if err != nil {
		return InlineIllustration{}, err
	}
	webp, _, _, err := imaging.EncodeWebPMinWidth(result.Image, webpQuality, minFeatureWidth)
	if err != nil {
		return InlineIllustration{}, publisher.Permanent(fmt.Errorf("encode inline image: %w", err))
	}
	stored, err := storeWebP(ctx, p.deps.Store, InlineSlug(request.Slug), webp, p.deps.Logger)
	if err != nil {
		return InlineIllustration{}, fmt.Errorf("store inline image: %w", err)
	}
	return InlineIllustration{URL: stored.URL, Alt: result.Alt, Discard: stored.Discard}, nil
}

// InlineSlug is the storage name of an article's inline image: the slug,
// shortened so the "-inline" marker survives media.SanitizeSlug's limit.
func InlineSlug(slug string) string {
	safe, err := media.SanitizeSlug(slug)
	if err != nil {
		safe = "article"
	}
	if len(safe) > 110 {
		safe = strings.Trim(safe[:110], "-")
	}
	return safe + "-inline"
}

// ProduceInline runs the inline pipeline without storing anything: check
// that the featured image is ours, write a brief of the section, then try
// the chain's generating providers with the featured image as the style
// reference. Every image passes the compliance review.
func (p *Pipeline) ProduceInline(ctx context.Context, request InlineRequest) (InlineResult, error) {
	if err := p.acquire(ctx); err != nil {
		return InlineResult{}, err
	}
	defer p.release()
	started := p.deps.Now()
	run := &pipelineRun{p: p, request: publisher.IllustrationRequest{Slug: request.Slug, Title: request.Title, Excerpt: request.Section}, report: &Report{}}
	image, alt, err := run.produceInline(ctx, request)
	run.report.Seconds = p.deps.Now().Sub(started).Seconds()
	logger := p.deps.Logger.With("slug", request.Slug, "analyzer", run.report.Analyzer, "seconds", int(run.report.Seconds))
	if err != nil {
		logger.WarnContext(ctx, "inline image pipeline failed", "error", err, "errors", run.report.Errors)
		return InlineResult{Report: *run.report}, err
	}
	logger.InfoContext(ctx, "inline image ready", "provider", run.report.Provider)
	return InlineResult{Image: image, Alt: alt, Report: *run.report}, nil
}

func (r *pipelineRun) produceInline(ctx context.Context, request InlineRequest) ([]byte, string, error) {
	deps := r.p.deps
	if strings.TrimSpace(request.Section) == "" {
		return nil, "", publisher.Permanent(errors.New("inline image has no section text"))
	}
	featured, err := FetchReference(ctx, deps.HTTP, request.FeaturedImageURL)
	if err != nil {
		return nil, "", fmt.Errorf("featured image: %w", err)
	}
	if featured == nil {
		return nil, "", publisher.Permanent(fmt.Errorf("%w: the article has no featured image", ErrNotOurImage))
	}
	decoded, err := imaging.Decode(featured)
	if err != nil {
		return nil, "", publisher.Permanent(fmt.Errorf("%w: undecodable featured image: %v", ErrNotOurImage, err))
	}
	if err := r.checkNotSource(ctx, decoded, request.SourceImageURL); err != nil {
		return nil, "", err
	}
	if err := r.checkDrawn(ctx, featured); err != nil {
		return nil, "", err
	}
	if r.styleReference, err = imaging.FitJPEG(featured, referenceMaxEdge, referenceQuality); err != nil {
		return nil, "", publisher.Permanent(fmt.Errorf("featured image as reference: %w", err))
	}

	var collages []string
	for _, style := range deps.Styles.All() {
		if style.Collage {
			collages = append(collages, style.Name)
		}
	}
	allowedStyles := deps.Styles.WithoutStyles(collages)
	r.allowedStyles = allowedStyles
	input := AnalyzeInput{Title: request.Title, Section: request.Section, Styles: &allowedStyles, Compositions: allowedCompositions(nil), Shots: shotsExcept(nil)}
	if small, err := imaging.FitJPEG(featured, analyzeMaxEdge, analyzeQuality); err == nil {
		input.Source = small
	}
	brief := r.runAnalyzers(ctx, input)
	if brief == nil {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		return nil, "", classifyChainFailure(r.errs)
	}
	// No logos, no collage: the inline image is a plain drawing.
	r.brands, brief.Brands, r.report.Brands = nil, nil, nil
	brief.PublicFigure = nil
	alt := brief.Alt
	if alt == "" {
		alt = firstSentence(brief.Scene)
	}

	for _, step := range deps.Chain {
		if step.Provider == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		if image, ok := r.generate(ctx, step.Provider, *brief, nil, nil); ok {
			return image, alt, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if len(r.errs) == 0 {
		return nil, "", publisher.Permanent(errors.New("inline image: the chain has no generating provider"))
	}
	return nil, "", classifyChainFailure(r.errs)
}

// checkNotSource refuses a featured image that is the news source's own
// image, re-encoded: those were copied, not drawn by us.
func (r *pipelineRun) checkNotSource(ctx context.Context, featured image.Image, sourceURL string) error {
	if sourceURL == "" {
		return nil
	}
	source, err := FetchReference(ctx, r.p.deps.HTTP, sourceURL)
	if err != nil || source == nil {
		// The source image is gone or unusable; the drawn check still runs.
		return nil
	}
	decoded, err := imaging.Decode(source)
	if err != nil {
		return nil
	}
	if SameImage(featured, decoded) {
		return publisher.Permanent(fmt.Errorf("%w: it is the source's own image", ErrNotOurImage))
	}
	return nil
}

// checkDrawn asks the vision model whether the featured image is entirely a
// drawn illustration, so photos, copied source images and collages with
// photo pieces are never used as a reference. It fails closed.
func (r *pipelineRun) checkDrawn(ctx context.Context, featured []byte) error {
	deps := r.p.deps
	if deps.Vision == nil {
		return publisher.Permanent(fmt.Errorf("%w: no vision model to check it", ErrNotOurImage))
	}
	jpeg, err := imaging.FitJPEG(featured, reviewMaxEdge, reviewQuality)
	if err != nil {
		return publisher.Permanent(fmt.Errorf("%w: %v", ErrNotOurImage, err))
	}
	started := deps.Now()
	raw, err := deps.Vision.ReviewImage(ctx, DrawnCheckPrompt, jpeg, drawnCheckSchema)
	if err != nil {
		if errors.Is(err, gemini.ErrEmptyResponse) {
			return errors.New("featured image check returned no content")
		}
		return fmt.Errorf("featured image check: %w", publisher.ClassifyGeminiError(err))
	}
	drawn, notes, ok := ParseDrawnCheck(raw)
	outcome := "ok"
	if !ok {
		outcome = "unparsable answer"
	} else if !drawn {
		outcome = "not drawn: " + notes
	}
	r.report.Attempts = append(r.report.Attempts, Attempt{Provider: "featured-check", Outcome: outcome, Seconds: deps.Now().Sub(started).Seconds()})
	switch {
	case !ok:
		return errors.New("featured image check returned an unusable answer")
	case !drawn:
		return publisher.Permanent(fmt.Errorf("%w: %s", ErrNotOurImage, notes))
	}
	return nil
}

// DrawnCheckPrompt asks whether an image is purely a drawn illustration.
const DrawnCheckPrompt = `You are checking an image before it is used as the style reference for a new drawn illustration.

drawn_illustration: true only if the whole image is one drawn, painted or digitally rendered illustration. False if any part of it is a photograph, a photo cut-out, a halftone or torn print of a photo, a screenshot, a real product shot, or a collage that mixes photos with drawing.
notes: one short sentence saying what the image is.

Return only JSON with exactly this shape:
{"drawn_illustration":true,"notes":"..."}`

var drawnCheckSchema = map[string]any{
	"type": "OBJECT",
	"properties": map[string]any{
		"drawn_illustration": map[string]any{"type": "BOOLEAN"},
		"notes":              map[string]any{"type": "STRING"},
	},
	"required": []string{"drawn_illustration", "notes"},
}

// ParseDrawnCheck reads the drawn check's answer; ok is false when it is
// unparsable or incomplete.
func ParseDrawnCheck(raw string) (drawn bool, notes string, ok bool) {
	cleaned := strings.TrimSpace(trailingFence.ReplaceAllString(leadingFence.ReplaceAllString(strings.TrimSpace(raw), ""), ""))
	if match := jsonObject.FindString(cleaned); match != "" {
		cleaned = match
	}
	var answer struct {
		Drawn *bool  `json:"drawn_illustration"`
		Notes string `json:"notes"`
	}
	if err := json.Unmarshal([]byte(cleaned), &answer); err != nil || answer.Drawn == nil {
		return false, "", false
	}
	return *answer.Drawn, strings.TrimSpace(answer.Notes), true
}

// firstSentence is the text up to and including its first full stop.
func firstSentence(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if end := strings.Index(text, ". "); end >= 0 {
		return text[:end+1]
	}
	return text
}

// SameImage reports whether two images show the same picture, allowing for
// scaling and re-encoding: same aspect ratio (within 3%) and near-identical
// 16x16 average hashes.
func SameImage(a, b image.Image) bool {
	ab, bb := a.Bounds(), b.Bounds()
	if ab.Dx() == 0 || ab.Dy() == 0 || bb.Dx() == 0 || bb.Dy() == 0 {
		return false
	}
	ratioA := float64(ab.Dx()) / float64(ab.Dy())
	ratioB := float64(bb.Dx()) / float64(bb.Dy())
	if ratioA/ratioB > 1.03 || ratioB/ratioA > 1.03 {
		return false
	}
	hashA, hashB := averageHash(a), averageHash(b)
	distance := 0
	for i := range hashA {
		distance += bits.OnesCount64(hashA[i] ^ hashB[i])
	}
	return distance <= 24 // of 256 bits
}

// averageHash is a 16x16 grid of luminance cells, each bit set when the
// cell is brighter than the image's mean.
func averageHash(img image.Image) [4]uint64 {
	const grid = 16
	bounds := img.Bounds()
	var cells [grid * grid]float64
	var total float64
	for gy := 0; gy < grid; gy++ {
		for gx := 0; gx < grid; gx++ {
			x0 := bounds.Min.X + gx*bounds.Dx()/grid
			x1 := max(bounds.Min.X+(gx+1)*bounds.Dx()/grid, x0+1)
			y0 := bounds.Min.Y + gy*bounds.Dy()/grid
			y1 := max(bounds.Min.Y+(gy+1)*bounds.Dy()/grid, y0+1)
			stepX, stepY := max((x1-x0)/8, 1), max((y1-y0)/8, 1)
			var sum float64
			count := 0
			for y := y0; y < y1; y += stepY {
				for x := x0; x < x1; x += stepX {
					red, green, blue, _ := img.At(x, y).RGBA()
					sum += 0.299*float64(red) + 0.587*float64(green) + 0.114*float64(blue)
					count++
				}
			}
			cells[gy*grid+gx] = sum / float64(count)
			total += cells[gy*grid+gx]
		}
	}
	mean := total / float64(len(cells))
	var hash [4]uint64
	for i, cell := range cells {
		if cell > mean {
			hash[i/64] |= 1 << (i % 64)
		}
	}
	return hash
}
