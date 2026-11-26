// Package realphoto looks for a real, legitimately usable photo of an
// article's subject to use as its in-body image, before the pipeline falls
// back to drawing one: first a freely licensed Wikimedia Commons photo, then
// the maker's own official image from its website. Every step can fail; a
// miss can be transient, such as when the search is rate limited.
package realphoto

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
)

// Kinds of credited photo; generated illustrations have none.
const (
	KindCommons  = "commons"
	KindOfficial = "official"
)

// Size and aspect limits for photo candidates.
const (
	MinWidth          = 1200
	MinAspect         = 1.5 // inline or unspecified slot
	MaxAspect         = 2.1
	minFeaturedAspect = 4.0 / 3.0
	aspectTolerance   = 0.01
	// MaxVerified caps the vision checks per source.
	MaxVerified = 8
	// MinQuality is the lowest vision quality score a photo may have.
	MinQuality = 7
)

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

// TextModel is the Gemini text call (JSON output).
type TextModel interface {
	GenerateJSON(ctx context.Context, prompt string) (string, error)
}

// Searcher answers a prompt using a web search (Gemini's Google Search
// grounding). It finds the maker's real product page when the source does
// not link to it.
type Searcher interface {
	SearchText(ctx context.Context, prompt string) (string, error)
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
	// Slot is where the photo will be shown: SlotInline (the default) or
	// SlotFeatured, which has stricter checks and may use a stand-in subject.
	Slot string
	// Exclude lists photo URLs (Candidate.ImageURL) that must not be chosen.
	Exclude []string
	// RecentFeaturedSubjects lists featured subjects that can rule out a stand-in.
	RecentFeaturedSubjects []FeaturedSubjectUse
}

// FeaturedSubjectUse identifies a stand-in or exact person used as a featured photo.
type FeaturedSubjectUse struct {
	Subject string
	Kind    string
	Maker   string
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
	Source      string   `json:"source"`
	ImageURL    string   `json:"image_url"`
	PageURL     string   `json:"page_url"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	License     string   `json:"license,omitempty"`
	LicenseURL  string   `json:"license_url,omitempty"`
	Artist      string   `json:"artist,omitempty"`
	Width       int      `json:"width,omitempty"`
	Height      int      `json:"height,omitempty"`
	CaptureYear int      `json:"capture_year,omitempty"` // Commons only; zero when unknown
	Verdict     *Verdict `json:"verdict,omitempty"`
	Usable      bool     `json:"usable"`
	Reason      string   `json:"reason,omitempty"`

	fileTitle string
	data      []byte
}

const (
	excludedPhotoReason    = "already used by a recent featured image"
	rateLimitedPhotoReason = "not checked (rate limited)"
)

// Result is the outcome of Find. Found is false when no photo passed; the
// caller then draws an illustration.
type Result struct {
	Found bool `json:"found"`
	// Transient marks a miss caused by a retryable plan error, cancellation or rate limit.
	Transient bool       `json:"transient,omitempty"`
	Image     []byte     `json:"-"`
	Alt       string     `json:"alt,omitempty"`
	Credit    *Credit    `json:"credit,omitempty"`
	Chosen    *Candidate `json:"chosen,omitempty"`
	Plan      *Plan      `json:"plan,omitempty"`
	// StandIn is set when the chosen photo shows a stand-in subject.
	StandIn    *StandIn    `json:"stand_in,omitempty"`
	Official   string      `json:"official_page,omitempty"`
	Candidates []Candidate `json:"candidates"`
	// Reason says, in one line, why this path was or was not taken.
	Reason  string   `json:"reason"`
	Notes   []string `json:"notes,omitempty"`
	Seconds float64  `json:"seconds"`
}

// FeaturedSubjectUse returns the identity to record for a chosen featured photo.
func (r Result) FeaturedSubjectUse() *FeaturedSubjectUse {
	if !r.Found || r.Plan == nil {
		return nil
	}
	if r.StandIn != nil {
		return &FeaturedSubjectUse{Subject: r.StandIn.Subject, Kind: r.StandIn.Kind, Maker: r.Plan.Maker}
	}
	if r.Plan.Photographable && r.Plan.Kind == KindPerson {
		return &FeaturedSubjectUse{Subject: r.Plan.Subject, Kind: KindPerson, Maker: r.Plan.Maker}
	}
	return nil
}

// Finder runs the plan, Commons and official-image steps.
type Finder struct {
	text   TextModel
	vision VisionModel
	http   *http.Client
	logger *slog.Logger
	search Searcher // nil: no product-page lookup
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

// WithSearch lets the official step look up the maker's product page.
func (f *Finder) WithSearch(searcher Searcher) *Finder {
	f.search = searcher
	return f
}

// WithCommonsAPI points the Finder at another MediaWiki API (for tests).
func (f *Finder) WithCommonsAPI(endpoint string) *Finder {
	f.commonsAPI = endpoint
	return f
}

// WithClock sets the clock used for capture age and elapsed time.
func (f *Finder) WithClock(now func() time.Time) *Finder {
	f.now = now
	return f
}

type rateLimitKey struct{}

// recordRateLimit is called for requests that contribute to photo selection.
func recordRateLimit(ctx context.Context, err error) {
	var response *gemini.Error
	if errors.Is(err, ErrRateLimited) || (errors.As(err, &response) && response.Status == http.StatusTooManyRequests) {
		if limited, ok := ctx.Value(rateLimitKey{}).(*atomic.Bool); ok {
			limited.Store(true)
		}
	}
}

func transientPlanError(err error) bool {
	var response *gemini.Error
	if errors.As(err, &response) {
		return response.Transient()
	}
	var networkErr net.Error
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.As(err, &networkErr) || errors.Is(err, io.ErrUnexpectedEOF)
}

// Find looks for a usable photo: plan, then Commons, then the maker's
// official image. Result tells the caller whether it found one or should retry.
func (f *Finder) Find(ctx context.Context, request Request) Result {
	started := f.now()
	var rateLimited atomic.Bool
	ctx = context.WithValue(ctx, rateLimitKey{}, &rateLimited)
	result := f.find(ctx, request)
	result.Seconds = f.now().Sub(started).Seconds()
	logger := f.logger.With("slug", request.Slug, "seconds", int(result.Seconds), "candidates", len(result.Candidates))
	if result.Found {
		logger.InfoContext(ctx, "inline photo found", "kind", result.Credit.Kind, "url", result.Chosen.ImageURL,
			"score", result.Chosen.Verdict.Quality, "reason", result.Reason)
	} else {
		switch {
		case ctx.Err() != nil:
			result.Transient = true
			result.Reason = "cancelled: " + ctx.Err().Error()
		case rateLimited.Load():
			result.Transient = true
			result.Reason = "rate limited: " + result.Reason
		}
		logger.InfoContext(ctx, "no inline photo", "reason", result.Reason)
	}
	return result
}

func (f *Finder) find(ctx context.Context, request Request) Result {
	result := Result{Candidates: []Candidate{}}
	if ctx.Err() != nil {
		return result
	}
	if f.text == nil || f.vision == nil || f.http == nil {
		result.Reason = "photo finder is not configured"
		return result
	}
	plan, err := f.plan(ctx, request)
	if err != nil {
		result.Transient = transientPlanError(err)
		result.Reason = "plan failed: " + err.Error()
		return result
	}
	result.Plan = &plan
	if !plan.Photographable {
		if request.Slot != SlotFeatured || len(plan.StandIns) == 0 {
			result.Reason = "no photographable subject: " + plan.Reason
			return result
		}
		return f.findStandIn(ctx, request, plan, result)
	}

	commons, err := f.commons(ctx, request, plan, 0)
	result.Candidates = append(result.Candidates, commons...)
	if err != nil {
		result.Notes = append(result.Notes, "commons: "+err.Error())
	}
	if best := bestCandidate(commons); best != nil {
		result.choose(best, commonsCredit(*best), plan.Subject)
		result.Reason = fmt.Sprintf("Wikimedia Commons photo of %s, score %d", plan.Subject, best.Verdict.Quality)
		return result
	}
	if ctx.Err() != nil {
		result.Reason = "cancelled"
		return result
	}
	if plan.Kind == KindPerson {
		result.Reason = "no usable Commons photo of " + plan.Subject
		return result
	}

	official, page, maker, err := f.official(ctx, request, plan)
	result.Official = page
	result.Candidates = append(result.Candidates, official...)
	if err != nil {
		result.Notes = append(result.Notes, "official: "+err.Error())
	}
	if best := bestCandidate(official); best != nil {
		result.choose(best, Credit{Kind: KindOfficial, Text: "Image: " + maker, URL: best.PageURL}, plan.Subject)
		result.Reason = fmt.Sprintf("official image from %s, score %d", maker, best.Verdict.Quality)
		return result
	}
	result.Reason = "no usable Commons or official photo of " + plan.Subject
	return result
}

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
	var skipped []string
	for _, standIn := range plan.StandIns {
		if recentlyUsedStandIn(standIn, plan.Maker, request.RecentFeaturedSubjects) {
			note := fmt.Sprintf("stand-in %s skipped as recently used", standIn.Subject)
			skipped = append(skipped, note)
			result.Notes = append(result.Notes, note)
			continue
		}
		subPlan := Plan{Photographable: true, Subject: standIn.Subject, Kind: standIn.Kind, Queries: standIn.Queries, Maker: plan.Maker}
		candidates, err := f.commons(ctx, request, subPlan, standIn.headquartersSince)
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
	if len(skipped) > 0 {
		result.Reason += "; " + strings.Join(skipped, "; ")
	}
	return result
}

func recentlyUsedStandIn(standIn StandIn, maker string, recent []FeaturedSubjectUse) bool {
	makerWords := strings.Fields(maker)
	subject := strings.Join(strings.Fields(standIn.Subject), " ")
	for _, used := range recent {
		if standIn.Kind != used.Kind {
			continue
		}
		if strings.EqualFold(subject, strings.Join(strings.Fields(used.Subject), " ")) {
			return true
		}
		usedMakerWords := strings.Fields(used.Maker)
		prefixLen := min(len(makerWords), len(usedMakerWords))
		if hasWordPrefixFold(makerWords, usedMakerWords[:prefixLen]) {
			return true
		}
	}
	return false
}

// hasWordPrefixFold compares a non-empty prefix of whitespace-split words,
// ignoring case.
func hasWordPrefixFold(words, prefix []string) bool {
	return len(prefix) > 0 && len(words) >= len(prefix) &&
		slices.EqualFunc(words[:len(prefix)], prefix, strings.EqualFold)
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
func fitsSlot(width, height int, slot string) (bool, string) {
	if width < MinWidth {
		return false, fmt.Sprintf("too narrow (%d px)", width)
	}
	if height <= 0 {
		return false, "unknown height"
	}
	minAspect := MinAspect
	if slot == SlotFeatured {
		minAspect = minFeaturedAspect
	}
	aspect := float64(width) / float64(height)
	if aspect < minAspect-aspectTolerance || aspect > MaxAspect+aspectTolerance {
		return false, fmt.Sprintf("aspect %.4f outside %.2f-%.2f (tolerance %.2f)", aspect, minAspect, MaxAspect, aspectTolerance)
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
// at most MaxVerified of them, marking each usable or saying why not. A
// rate-limited download stops further downloads and is returned to the caller.
func (f *Finder) verifyAll(ctx context.Context, plan Plan, request Request, mode string, candidates []Candidate) error {
	verified := 0
	var rateLimitErr error
	for i := range candidates {
		candidate := &candidates[i]
		if candidate.Reason != "" {
			continue
		}
		if slices.Contains(request.Exclude, candidate.ImageURL) {
			candidate.Reason = excludedPhotoReason
			continue
		}
		if rateLimitErr != nil && candidate.data == nil {
			candidate.Reason = rateLimitedPhotoReason
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
				if errors.Is(err, ErrRateLimited) {
					rateLimitErr = err
				}
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
			if ok, why := fitsSlot(width, height, request.Slot); !ok {
				candidate.Reason = why
				candidate.data = nil
				continue
			}
		}
		verified++
		verdict, err := f.verify(ctx, plan, request, candidate)
		if err != nil {
			candidate.Reason = "vision check: " + err.Error()
			candidate.data = nil
			continue
		}
		candidate.Verdict = &verdict
		if ok, why := verdict.usableFor(request.Slot, plan.Kind); ok {
			candidate.Usable = true
		} else {
			candidate.Reason = why
			candidate.data = nil
		}
	}
	return rateLimitErr
}

// sortByRank keeps candidates in their search order.
func sortByRank[T any](items []T, rank func(T) int) {
	sort.SliceStable(items, func(i, j int) bool { return rank(items[i]) < rank(items[j]) })
}
