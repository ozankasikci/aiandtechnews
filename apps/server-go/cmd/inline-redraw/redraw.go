package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
)

// Current is an article and its inline image as it is now.
type Current struct {
	ID             int64  `json:"id"`
	Slug           string `json:"slug"`
	Title          string `json:"title"`
	Content        string `json:"-"`
	FeaturedImage  string `json:"featured_image"`
	SourceImage    string `json:"-"`
	URL            string `json:"url"`
	Alt            string `json:"alt"`
	AfterParagraph int    `json:"after_paragraph"`
	// Credited: the inline image is a real photo with a credit row.
	Credited bool `json:"-"`
}

// Articles reads and replaces inline images.
type Articles interface {
	Current(ctx context.Context, slug string) (Current, error)
	Replace(ctx context.Context, current Current, url, alt string) error
}

// Drawer draws and stores a new inline illustration.
type Drawer interface {
	Draw(ctx context.Context, current Current) (illustration.InlineIllustration, error)
}

// Refresher asks the site to refresh article pages.
type Refresher interface {
	Revalidate(ctx context.Context, slugs []string) error
}

type Deps struct {
	Articles  Articles
	Drawer    Drawer
	Refresher Refresher // nil skips the refresh
}

type Report struct {
	Redrawn []string
	Skipped []string
	// Failed holds "slug: error" for articles whose drawing failed.
	Failed []string
}

// dedupe drops repeated slugs, keeping the first occurrence.
func dedupe(slugs []string) []string {
	seen := make(map[string]bool, len(slugs))
	var out []string
	for _, slug := range slugs {
		if !seen[slug] {
			seen[slug] = true
			out = append(out, slug)
		}
	}
	return out
}

// Redraw replaces the inline illustration of each slug. It refuses an
// existing undoPath, then writes every redrawable article's current url and
// alt there before changing anything. Real photos and articles with no
// inline image are skipped; a failed draw is reported and the next article
// goes on; a failed database write stops the run. Articles already replaced
// are always revalidated before returning.
func Redraw(ctx context.Context, deps Deps, slugs []string, undoPath string) (Report, error) {
	var report Report
	var todo []Current
	for _, slug := range dedupe(slugs) {
		current, err := deps.Articles.Current(ctx, slug)
		if err != nil {
			return report, err
		}
		if current.URL == "" || current.Credited {
			report.Skipped = append(report.Skipped, slug)
			continue
		}
		todo = append(todo, current)
	}
	raw, err := json.MarshalIndent(todo, "", " ")
	if err != nil {
		return report, err
	}
	file, err := os.OpenFile(undoPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return report, fmt.Errorf("undo file %s already exists; pick another -undo path", undoPath)
		}
		return report, fmt.Errorf("write undo file: %w", err)
	}
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return report, fmt.Errorf("write undo file: %w", err)
	}
	if err := file.Close(); err != nil {
		return report, fmt.Errorf("write undo file: %w", err)
	}
	var runErr error
	for _, current := range todo {
		drawn, err := deps.Drawer.Draw(ctx, current)
		if err != nil {
			report.Failed = append(report.Failed, fmt.Sprintf("%s: %v", current.Slug, err))
			continue
		}
		if err := deps.Articles.Replace(ctx, current, drawn.URL, drawn.Alt); err != nil {
			if drawn.Discard != nil {
				drawn.Discard(ctx)
			}
			runErr = fmt.Errorf("replace %s: %w", current.Slug, err)
			break
		}
		report.Redrawn = append(report.Redrawn, current.Slug)
	}
	if deps.Refresher != nil && len(report.Redrawn) > 0 {
		if err := deps.Refresher.Revalidate(ctx, report.Redrawn); err != nil {
			return report, errors.Join(runErr, fmt.Errorf("refresh site: %w", err))
		}
	}
	if runErr != nil {
		return report, runErr
	}
	if len(report.Failed) > 0 {
		return report, fmt.Errorf("%d of %d draws failed: %v", len(report.Failed), len(todo), report.Failed)
	}
	return report, nil
}
