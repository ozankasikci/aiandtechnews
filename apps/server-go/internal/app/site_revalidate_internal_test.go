package app

import (
	"context"
	"errors"
	"testing"
)

type recordingSite struct{ slugs [][]string }

func (r *recordingSite) Notify(slugs []string) { r.slugs = append(r.slugs, slugs) }

type failingSubmitter struct{ called bool }

func (f *failingSubmitter) SubmitSlugs(context.Context, []string) error {
	f.called = true
	return errors.New("indexnow down")
}

// A published article refreshes the site before IndexNow runs, and an
// IndexNow failure is still reported to the publisher (which only logs it).
func TestRevalidatingNotifierRefreshesTheSiteThenSubmits(t *testing.T) {
	site, next := &recordingSite{}, &failingSubmitter{}
	err := revalidatingNotifier{site: site, next: next}.SubmitSlugs(context.Background(), []string{"new-story"})
	if err == nil || !next.called {
		t.Fatalf("err = %v, submitted = %t", err, next.called)
	}
	if len(site.slugs) != 1 || site.slugs[0][0] != "new-story" {
		t.Fatalf("site = %v", site.slugs)
	}
}
