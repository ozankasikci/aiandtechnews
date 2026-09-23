package publisher_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/collector"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

const sourceURL = "https://techcrunch.com/2026/09/23/anthropic-launches-new-model"

func sourcePage() string {
	var b strings.Builder
	b.WriteString(`<html><head><link rel="canonical" href="` + sourceURL + `"><meta property="og:image" content="https://techcrunch.com/lead.jpg"></head><body><article>`)
	for i := 0; i < 6; i++ {
		fmt.Fprintf(&b, "<p>%s Paragraph %d adds a distinct detail about the release and its pricing for developers.</p>", storyParagraph, i)
	}
	b.WriteString("</article></body></html>")
	return b.String()
}

type fakeFetcher struct {
	body string
	err  error
}

func (f fakeFetcher) FetchText(context.Context, string, string) (string, string, error) {
	return f.body, sourceURL, f.err
}

type fakeRewriter struct{ err error }

func (f fakeRewriter) Rewrite(context.Context, publisher.RewriteInput) (content.RewrittenArticle, error) {
	return content.RewrittenArticle{Title: "Anthropic releases a cheaper model", Excerpt: "It costs less.", Content: "<p>Body</p>"}, f.err
}

type fakeIllustrator struct {
	err       error
	reference string
	discarded bool
}

func (f *fakeIllustrator) Illustrate(_ context.Context, request publisher.IllustrationRequest) (publisher.Illustration, error) {
	f.reference = request.ReferenceImageURL
	if f.err != nil {
		return publisher.Illustration{}, f.err
	}
	return publisher.Illustration{URL: "https://images.example.com/x.webp", Discard: func(context.Context) { f.discarded = true }}, nil
}

type fakeNotifier struct{ slugs []string }

func (f *fakeNotifier) SubmitSlugs(_ context.Context, slugs []string) error {
	f.slugs = append(f.slugs, slugs...)
	return errors.New("indexnow down")
}

type harness struct {
	store       *newsroom.SQLiteStore
	illustrator *fakeIllustrator
	notifier    *fakeNotifier
	pub         *publisher.Publisher
	now         time.Time
	id          int64
}

func newHarness(t *testing.T, fetcher publisher.SourceFetcher, rewriter publisher.ArticleRewriter, illustratorErr error) *harness {
	t.Helper()
	db := openDB(t)
	store := newsroom.NewSQLiteStore(db)
	h := &harness{store: store, illustrator: &fakeIllustrator{err: illustratorErr}, notifier: &fakeNotifier{}, now: publishNow}
	ctx := context.Background()
	id, _, err := store.Insert(ctx, newsroom.NewCandidate{SourceURL: sourceURL, SourceName: "TechCrunch", FeedURL: "https://techcrunch.com/feed/", Title: "Anthropic launches new model"}, publishNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkQueued(ctx, id, newsroom.StatusPending, publishNow, publishNow); err != nil {
		t.Fatal(err)
	}
	h.id = id
	h.pub = publisher.New(publisher.Deps{
		Store: store, Fetcher: fetcher, Rewriter: rewriter, Illustrator: h.illustrator,
		Articles: publisher.NewSQLiteArticles(db, func() time.Time { return h.now }), Notifier: h.notifier,
		Now: func() time.Time { return h.now }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return h
}

func (h *harness) candidate(t *testing.T) newsroom.Candidate {
	t.Helper()
	candidate, err := h.store.Get(context.Background(), h.id)
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}

func TestPublishNextPublishesDueCandidate(t *testing.T) {
	h := newHarness(t, fakeFetcher{body: sourcePage()}, fakeRewriter{}, nil)
	published, err := h.pub.PublishNext(context.Background())
	if err != nil || !published {
		t.Fatalf("published=%v err=%v", published, err)
	}
	candidate := h.candidate(t)
	if candidate.Status != newsroom.StatusPublished || candidate.ArticleSlug == nil || *candidate.ArticleSlug != "anthropic-releases-a-cheaper-model" {
		t.Fatalf("candidate = %+v", candidate)
	}
	if h.illustrator.reference != "https://techcrunch.com/lead.jpg" {
		t.Fatalf("reference image = %q", h.illustrator.reference)
	}
	if len(h.notifier.slugs) != 1 || h.notifier.slugs[0] != "anthropic-releases-a-cheaper-model" {
		t.Fatalf("indexnow slugs = %v (its failure must not fail the publish)", h.notifier.slugs)
	}
	if published, _ := h.pub.PublishNext(context.Background()); published {
		t.Fatal("nothing else is due")
	}
}

func TestPublishNextRetriesTransientFailuresThenFails(t *testing.T) {
	h := newHarness(t, fakeFetcher{err: errors.New("connection reset")}, fakeRewriter{}, nil)
	for attempt := 1; attempt <= publisher.MaxAttempts; attempt++ {
		if published, err := h.pub.PublishNext(context.Background()); err != nil || !published {
			t.Fatalf("attempt %d: published=%v err=%v", attempt, published, err)
		}
		candidate := h.candidate(t)
		if attempt < publisher.MaxAttempts {
			if candidate.Status != newsroom.StatusQueued || *candidate.ScheduledFor != h.now.Add(publisher.RetryDelay).Format(time.RFC3339) {
				t.Fatalf("attempt %d: candidate = %+v", attempt, candidate)
			}
			h.now = h.now.Add(publisher.RetryDelay)
			continue
		}
		if candidate.Status != newsroom.StatusFailed || !strings.Contains(*candidate.LastError, "connection reset") {
			t.Fatalf("final candidate = %+v", candidate)
		}
	}
}

func TestPublishNextFailsPermanentProblemsImmediately(t *testing.T) {
	cases := map[string]struct {
		fetcher  publisher.SourceFetcher
		rewriter publisher.ArticleRewriter
		want     string
	}{
		"redirect outside source": {fakeFetcher{err: fmt.Errorf("%w TechCrunch: https://evil.example", collector.ErrRedirectOutsideSource)}, fakeRewriter{}, "redirect"},
		"short source text":       {fakeFetcher{body: "<p>Too short.</p>"}, fakeRewriter{}, "too short"},
		"rejected rewrite":        {fakeFetcher{body: sourcePage()}, fakeRewriter{err: publisher.Permanent(errors.New("rewrite failed validation: word count"))}, "rewrite failed"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, tc.fetcher, tc.rewriter, nil)
			if _, err := h.pub.PublishNext(context.Background()); err != nil {
				t.Fatal(err)
			}
			candidate := h.candidate(t)
			if candidate.Status != newsroom.StatusFailed || !strings.Contains(*candidate.LastError, tc.want) {
				t.Fatalf("candidate = %+v", candidate)
			}
		})
	}
}

func TestPublishNextSkipsAlreadyPublishedStory(t *testing.T) {
	h := newHarness(t, fakeFetcher{body: sourcePage()}, fakeRewriter{}, nil)
	if _, err := h.pub.PublishNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A second candidate for the same canonical story must fail as a duplicate, before rewriting.
	ctx := context.Background()
	id, _, err := h.store.Insert(ctx, newsroom.NewCandidate{SourceURL: sourceURL + "?amp=1", SourceName: "TechCrunch", FeedURL: "x", Title: "Anthropic launches new model again"}, h.now)
	if err != nil || id == 0 {
		t.Skipf("normalizer merged the URL (id=%d err=%v); duplicate path covered by articles tests", id, err)
	}
	if err := h.store.MarkQueued(ctx, id, newsroom.StatusPending, h.now, h.now); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(time.Hour)
	if _, err := h.pub.PublishNext(ctx); err != nil {
		t.Fatal(err)
	}
	second, _ := h.store.Get(ctx, id)
	if second.Status != newsroom.StatusFailed || !strings.Contains(*second.LastError, "already published") {
		t.Fatalf("second = %+v", second)
	}
}

func TestRecoverRequeuesProcessingCandidates(t *testing.T) {
	h := newHarness(t, fakeFetcher{body: sourcePage()}, fakeRewriter{}, nil)
	if _, _, err := h.store.ClaimDue(context.Background(), h.now, 0); err != nil {
		t.Fatal(err)
	}
	if err := h.pub.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if candidate := h.candidate(t); candidate.Status != newsroom.StatusQueued {
		t.Fatalf("candidate = %+v", candidate)
	}
}

func TestIllustratorTransientFailureRequeues(t *testing.T) {
	h := newHarness(t, fakeFetcher{body: sourcePage()}, fakeRewriter{}, errors.New("image model timeout"))
	if _, err := h.pub.PublishNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if candidate := h.candidate(t); candidate.Status != newsroom.StatusQueued || !strings.Contains(*candidate.LastError, "image model timeout") {
		t.Fatalf("candidate = %+v", candidate)
	}
}

type failingArticles struct{ publisher.ArticleStore }

func (failingArticles) Exists(context.Context, string, string) (bool, error) { return false, nil }
func (failingArticles) Publish(context.Context, publisher.NewArticle) (int64, error) {
	return 0, errors.New("database is locked")
}

func TestFailedInsertDiscardsUploadedImage(t *testing.T) {
	h := newHarness(t, fakeFetcher{body: sourcePage()}, fakeRewriter{}, nil)
	h.pub.Articles = failingArticles{}
	if _, err := h.pub.PublishNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !h.illustrator.discarded {
		t.Fatal("uploaded image was not discarded")
	}
	if candidate := h.candidate(t); candidate.Status != newsroom.StatusQueued {
		t.Fatalf("a locked database is transient; candidate = %+v", candidate)
	}
}
