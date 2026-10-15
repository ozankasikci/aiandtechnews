package main

import (
	"context"
	"encoding/json"
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
}

// Redraw replaces the inline illustration of each slug. It writes every
// redrawable article's current url and alt to undoPath before changing
// anything, skips real photos and articles with no inline image, and stops
// at the first failure (articles already replaced stay replaced).
func Redraw(ctx context.Context, deps Deps, slugs []string, undoPath string) (Report, error) {
	var report Report
	var todo []Current
	for _, slug := range slugs {
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
	if err := os.WriteFile(undoPath, raw, 0o644); err != nil {
		return report, fmt.Errorf("write undo file: %w", err)
	}
	for _, current := range todo {
		drawn, err := deps.Drawer.Draw(ctx, current)
		if err != nil {
			return report, fmt.Errorf("draw %s: %w", current.Slug, err)
		}
		if err := deps.Articles.Replace(ctx, current, drawn.URL, drawn.Alt); err != nil {
			if drawn.Discard != nil {
				drawn.Discard(ctx)
			}
			return report, fmt.Errorf("replace %s: %w", current.Slug, err)
		}
		report.Redrawn = append(report.Redrawn, current.Slug)
	}
	if deps.Refresher != nil && len(report.Redrawn) > 0 {
		if err := deps.Refresher.Revalidate(ctx, report.Redrawn); err != nil {
			return report, fmt.Errorf("refresh site: %w", err)
		}
	}
	return report, nil
}
