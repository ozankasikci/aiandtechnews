// Package realphoto looks for a real, legitimately usable photo of an
// article's subject to use as its in-body image, before the pipeline falls
// back to drawing one: first a freely licensed Wikimedia Commons photo, then
// the maker's own official image from its website. Every step can fail; a
// failure only means no photo, never an error for the caller.
package realphoto

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
)

// Kinds of credited photo; generated illustrations have none.
const (
	KindCommons  = "commons"
	KindOfficial = "official"
)

// Size and shape a photo needs for the full-width 16:9 slot.
const (
	MinWidth  = 1200
	MinAspect = 1.5
	MaxAspect = 2.1
	// MaxVerified caps the vision checks per source.
	MaxVerified = 8
	// MinQuality is the lowest vision quality score a photo may have.
	MinQuality = 7
)

// TextModel is the Gemini text call (JSON output).
type TextModel interface {
	GenerateJSON(ctx context.Context, prompt string) (string, error)
}

// VisionModel is the Gemini vision call (JSON output constrained by schema).
type VisionModel interface {
	ReviewImage(ctx context.Context, prompt string, jpeg []byte, schema map[string]any) (string, error)
}

// Request describes the article. SourceURL is the news source's page: it is
// only read to find the maker's link and is never credited or shown.
type Request struct {
	Slug      string
	Title     string
	Text      string
	SourceURL string
}

// Credit is how a chosen photo is credited on the site.
type Credit struct {
	Kind       string `json:"kind"`
	Text       string `json:"text"`
	URL        string `json:"url"`
	License    string `json:"license,omitempty"`
	LicenseURL string `json:"license_url,omitempty"`
}

// Candidate is one photo considered, and what happened to it.
type Candidate struct {
	Source     string   `json:"source"`
	ImageURL   string   `json:"image_url"`
	PageURL    string   `json:"page_url"`
	License    string   `json:"license,omitempty"`
	LicenseURL string   `json:"license_url,omitempty"`
	Artist     string   `json:"artist,omitempty"`
	Width      int      `json:"width,omitempty"`
	Height     int      `json:"height,omitempty"`
	Verdict    *Verdict `json:"verdict,omitempty"`
	Usable     bool     `json:"usable"`
	Reason     string   `json:"reason,omitempty"`

	data []byte
}

// Result is the outcome of Find. Found is false when no photo passed; the
// caller then draws an illustration.
type Result struct {
	Found      bool        `json:"found"`
	Image      []byte      `json:"-"`
	Alt        string      `json:"alt,omitempty"`
	Credit     *Credit     `json:"credit,omitempty"`
	Chosen     *Candidate  `json:"chosen,omitempty"`
	Plan       *Plan       `json:"plan,omitempty"`
	Official   string      `json:"official_page,omitempty"`
	Candidates []Candidate `json:"candidates"`
	// Reason says, in one line, why this path was or was not taken.
	Reason  string   `json:"reason"`
	Notes   []string `json:"notes,omitempty"`
	Seconds float64  `json:"seconds"`
}

// Finder runs the plan, Commons and official-image steps.
type Finder struct {
	text   TextModel
	vision VisionModel
	http   *http.Client
	logger *slog.Logger
	// commonsAPI is the MediaWiki API endpoint (overridable in tests).
	commonsAPI string
	now        func() time.Time
}

// CommonsAPI is Wikimedia Commons' MediaWiki API.
const CommonsAPI = "https://commons.wikimedia.org/w/api.php"

// NewFinder builds a Finder. client must be a safe client for untrusted
// URLs (illustration.NewReferenceClient): no private or loopback addresses.
func NewFinder(text TextModel, vision VisionModel, client *http.Client, logger *slog.Logger) *Finder {
	if logger == nil {
		logger = slog.Default()
	}
	return &Finder{text: text, vision: vision, http: client, logger: logger, commonsAPI: CommonsAPI, now: time.Now}
}

// WithCommonsAPI points the Finder at another MediaWiki API (for tests).
func (f *Finder) WithCommonsAPI(endpoint string) *Finder {
	f.commonsAPI = endpoint
	return f
}

// Find looks for a usable photo: plan, then Commons, then the maker's
// official image. It never fails; Result.Found says whether it found one.
func (f *Finder) Find(ctx context.Context, request Request) Result {
	started := f.now()
	result := f.find(ctx, request)
	result.Seconds = f.now().Sub(started).Seconds()
	logger := f.logger.With("slug", request.Slug, "seconds", int(result.Seconds), "candidates", len(result.Candidates))
	if result.Found {
		logger.InfoContext(ctx, "inline photo found", "kind", result.Credit.Kind, "url", result.Chosen.ImageURL,
			"score", result.Chosen.Verdict.Quality, "reason", result.Reason)
	} else {
		logger.InfoContext(ctx, "no inline photo", "reason", result.Reason)
	}
	return result
}

func (f *Finder) find(ctx context.Context, request Request) Result {
	result := Result{Candidates: []Candidate{}}
	if f.text == nil || f.vision == nil || f.http == nil {
		result.Reason = "photo finder is not configured"
		return result
	}
	plan, err := f.plan(ctx, request)
	if err != nil {
		result.Reason = "plan failed: " + err.Error()
		return result
	}
	result.Plan = &plan
	if !plan.Photographable {
		result.Reason = "no photographable subject: " + plan.Reason
		return result
	}

	commons, err := f.commons(ctx, request, plan)
	result.Candidates = append(result.Candidates, commons...)
	if err != nil {
		result.Notes = append(result.Notes, "commons: "+err.Error())
	}
	if best := bestCandidate(commons); best != nil {
		result.choose(best, commonsCredit(*best))
		result.Reason = fmt.Sprintf("Wikimedia Commons photo of %s, score %d", plan.Subject, best.Verdict.Quality)
		return result
	}
	if ctx.Err() != nil {
		result.Reason = "cancelled"
		return result
	}

	official, page, maker, err := f.official(ctx, request, plan)
	result.Official = page
	result.Candidates = append(result.Candidates, official...)
	if err != nil {
		result.Notes = append(result.Notes, "official: "+err.Error())
	}
	if best := bestCandidate(official); best != nil {
		result.choose(best, Credit{Kind: KindOfficial, Text: "Image: " + maker, URL: best.PageURL})
		result.Reason = fmt.Sprintf("official image from %s, score %d", maker, best.Verdict.Quality)
		return result
	}
	result.Reason = "no usable Commons or official photo of " + plan.Subject
	return result
}

func (r *Result) choose(best *Candidate, credit Credit) {
	chosen := *best
	r.Found = true
	r.Image = best.data
	r.Alt = strings.TrimSpace(best.Verdict.Alt)
	if r.Alt == "" && r.Plan != nil {
		r.Alt = "Photo of " + r.Plan.Subject + "."
	}
	r.Credit = &credit
	r.Chosen = &chosen
}

// bestCandidate is the usable candidate with the highest quality score; the
// earlier (higher-ranked) one wins a tie.
func bestCandidate(candidates []Candidate) *Candidate {
	var best *Candidate
	for i := range candidates {
		candidate := &candidates[i]
		if !candidate.Usable || candidate.Verdict == nil {
			continue
		}
		if best == nil || candidate.Verdict.Quality > best.Verdict.Quality {
			best = candidate
		}
	}
	return best
}

// fitsSlot reports whether a photo is big and wide enough for the slot.
func fitsSlot(width, height int) (bool, string) {
	if width < MinWidth {
		return false, fmt.Sprintf("too narrow (%d px)", width)
	}
	if height <= 0 {
		return false, "unknown height"
	}
	aspect := float64(width) / float64(height)
	if aspect < MinAspect || aspect > MaxAspect {
		return false, fmt.Sprintf("aspect %.2f outside %.2f-%.2f", aspect, MinAspect, MaxAspect)
	}
	return true, ""
}

// measure reads an image's size without decoding its pixels.
func measure(data []byte) (int, int, error) {
	if imaging.SniffMIME(data) == "" {
		return 0, 0, fmt.Errorf("not a JPEG, PNG, GIF or WebP")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return config.Width, config.Height, nil
}

// verifyAll downloads (when needed) and vision-checks candidates in order,
// at most MaxVerified of them, marking each usable or saying why not.
func (f *Finder) verifyAll(ctx context.Context, plan Plan, request Request, mode string, candidates []Candidate) {
	verified := 0
	for i := range candidates {
		candidate := &candidates[i]
		if candidate.Reason != "" {
			continue
		}
		if ctx.Err() != nil {
			candidate.Reason = "cancelled"
			continue
		}
		if verified >= MaxVerified {
			candidate.Reason = "not checked (cap reached)"
			continue
		}
		if candidate.data == nil {
			data, err := f.getImage(ctx, candidate.ImageURL)
			if err != nil {
				candidate.Reason = "download: " + err.Error()
				continue
			}
			candidate.data = data
		}
		width, height, err := measure(candidate.data)
		if err != nil {
			candidate.Reason = "unreadable image: " + err.Error()
			candidate.data = nil
			continue
		}
		if mode == KindOfficial {
			// The file itself decides for official images; Commons sizes come
			// from the API (the download is a 1600 px rendition).
			candidate.Width, candidate.Height = width, height
			if ok, why := fitsSlot(width, height); !ok {
				candidate.Reason = why
				candidate.data = nil
				continue
			}
		}
		verified++
		verdict, err := f.verify(ctx, plan, request, mode, candidate.data)
		if err != nil {
			candidate.Reason = "vision check: " + err.Error()
			candidate.data = nil
			continue
		}
		candidate.Verdict = &verdict
		if ok, why := verdict.usable(mode); ok {
			candidate.Usable = true
		} else {
			candidate.Reason = why
			candidate.data = nil
		}
	}
}

// sortByRank keeps candidates in their search order.
func sortByRank[T any](items []T, rank func(T) int) {
	sort.SliceStable(items, func(i, j int) bool { return rank(items[i]) < rank(items[j]) })
}
