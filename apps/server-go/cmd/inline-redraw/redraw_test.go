package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
)

type fakeArticles struct {
	rows     map[string]Current
	updated  []string
	undoSeen bool
	undoPath string
}

func (f *fakeArticles) Current(_ context.Context, slug string) (Current, error) {
	return f.rows[slug], nil
}
func (f *fakeArticles) Replace(_ context.Context, c Current, url, alt string) error {
	if _, err := os.Stat(f.undoPath); err == nil {
		f.undoSeen = true
	}
	f.updated = append(f.updated, c.Slug+"="+url)
	return nil
}

type fakeDrawer struct {
	drawn []string
	fail  map[string]bool
}

func (f *fakeDrawer) Draw(_ context.Context, c Current) (illustration.InlineIllustration, error) {
	f.drawn = append(f.drawn, c.Slug)
	if f.fail[c.Slug] {
		return illustration.InlineIllustration{}, errors.New("model down")
	}
	return illustration.InlineIllustration{URL: "https://img.test/" + c.Slug + "-new.webp", Alt: "new alt"}, nil
}

type fakeRefresher struct{ slugs []string }

func (f *fakeRefresher) Revalidate(_ context.Context, slugs []string) error {
	f.slugs = slugs
	return nil
}

func TestRedrawSkipsPhotosAndWritesUndoFirst(t *testing.T) {
	undo := filepath.Join(t.TempDir(), "undo.json")
	articles := &fakeArticles{undoPath: undo, rows: map[string]Current{
		"drawn":  {ID: 1, Slug: "drawn", URL: "https://img.test/drawn-old.webp", Alt: "old", AfterParagraph: 3},
		"photo":  {ID: 2, Slug: "photo", URL: "https://img.test/photo.webp", Credited: true},
		"no-row": {ID: 3, Slug: "no-row"},
	}}
	drawer, refresher := &fakeDrawer{}, &fakeRefresher{}
	report, err := Redraw(context.Background(), Deps{Articles: articles, Drawer: drawer, Refresher: refresher}, []string{"drawn", "photo", "no-row"}, undo)
	if err != nil {
		t.Fatal(err)
	}
	if len(drawer.drawn) != 1 || drawer.drawn[0] != "drawn" {
		t.Fatalf("drawn = %v", drawer.drawn)
	}
	if !articles.undoSeen || len(articles.updated) != 1 || articles.updated[0] != "drawn=https://img.test/drawn-new.webp" {
		t.Fatalf("updated = %v, undo written first = %v", articles.updated, articles.undoSeen)
	}
	var saved []Current
	raw, _ := os.ReadFile(undo)
	if err := json.Unmarshal(raw, &saved); err != nil || len(saved) != 1 || saved[0].URL != "https://img.test/drawn-old.webp" {
		t.Fatalf("undo = %s %v", raw, err)
	}
	if len(refresher.slugs) != 1 || refresher.slugs[0] != "drawn" {
		t.Fatalf("refreshed %v", refresher.slugs)
	}
	if len(report.Skipped) != 2 {
		t.Fatalf("skipped = %v", report.Skipped)
	}
}

func TestRedrawRefusesAnExistingUndoFile(t *testing.T) {
	undo := filepath.Join(t.TempDir(), "undo.json")
	if err := os.WriteFile(undo, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	articles := &fakeArticles{undoPath: undo, rows: map[string]Current{
		"drawn": {ID: 1, Slug: "drawn", URL: "https://img.test/drawn-old.webp"},
	}}
	drawer := &fakeDrawer{}
	_, err := Redraw(context.Background(), Deps{Articles: articles, Drawer: drawer}, []string{"drawn"}, undo)
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(drawer.drawn) != 0 || len(articles.updated) != 0 {
		t.Fatalf("drawn %v updated %v", drawer.drawn, articles.updated)
	}
	if raw, _ := os.ReadFile(undo); string(raw) != "keep me" {
		t.Fatalf("undo file overwritten: %s", raw)
	}
}

func TestRedrawContinuesAfterADrawFailureAndStillRefreshes(t *testing.T) {
	undo := filepath.Join(t.TempDir(), "undo.json")
	articles := &fakeArticles{undoPath: undo, rows: map[string]Current{
		"a": {ID: 1, Slug: "a", URL: "https://img.test/a.webp"},
		"b": {ID: 2, Slug: "b", URL: "https://img.test/b.webp"},
		"c": {ID: 3, Slug: "c", URL: "https://img.test/c.webp"},
	}}
	drawer, refresher := &fakeDrawer{fail: map[string]bool{"b": true}}, &fakeRefresher{}
	report, err := Redraw(context.Background(), Deps{Articles: articles, Drawer: drawer, Refresher: refresher}, []string{"a", "b", "c"}, undo)
	if err == nil {
		t.Fatal("expected an error for the failed draw")
	}
	if len(articles.updated) != 2 || articles.updated[0] != "a=https://img.test/a-new.webp" || articles.updated[1] != "c=https://img.test/c-new.webp" {
		t.Fatalf("updated = %v", articles.updated)
	}
	if len(report.Failed) != 1 {
		t.Fatalf("failed = %v", report.Failed)
	}
	if len(refresher.slugs) != 2 || refresher.slugs[0] != "a" || refresher.slugs[1] != "c" {
		t.Fatalf("refreshed %v", refresher.slugs)
	}
}

func TestRedrawDedupesSlugs(t *testing.T) {
	undo := filepath.Join(t.TempDir(), "undo.json")
	articles := &fakeArticles{undoPath: undo, rows: map[string]Current{
		"drawn": {ID: 1, Slug: "drawn", URL: "https://img.test/drawn-old.webp"},
	}}
	drawer := &fakeDrawer{}
	if _, err := Redraw(context.Background(), Deps{Articles: articles, Drawer: drawer}, []string{"drawn", "drawn"}, undo); err != nil {
		t.Fatal(err)
	}
	if len(drawer.drawn) != 1 {
		t.Fatalf("drawn = %v", drawer.drawn)
	}
}
