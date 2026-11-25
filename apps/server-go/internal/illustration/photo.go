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
