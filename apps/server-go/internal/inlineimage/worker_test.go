package inlineimage_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/inlineimage"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

type mark struct {
	id       int64
	ready    bool
	url      string
	alt      string
	after    int
	attempts int
	message  string
}

type fakeStore struct {
	articles []inlineimage.Article
	since    time.Time
	marks    []mark
	readyErr error
}

func (f *fakeStore) Pending(_ context.Context, since time.Time, maxAttempts int) ([]inlineimage.Article, error) {
	f.since = since
	return f.articles, nil
}

func (f *fakeStore) MarkReady(_ context.Context, id int64, url, alt string, after, attempts int) error {
	if f.readyErr != nil {
		return f.readyErr
	}
	f.marks = append(f.marks, mark{id: id, ready: true, url: url, alt: alt, after: after, attempts: attempts})
	return nil
}

func (f *fakeStore) MarkFailed(_ context.Context, id int64, message string, attempts int) error {
	f.marks = append(f.marks, mark{id: id, message: message, attempts: attempts})
	return nil
}

type fakeIllustrator struct {
	requests  []illustration.InlineRequest
	err       error
	discarded bool
}

func (f *fakeIllustrator) IllustrateInline(_ context.Context, request illustration.InlineRequest) (illustration.InlineIllustration, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return illustration.InlineIllustration{}, f.err
	}
	return illustration.InlineIllustration{URL: "https://img.test/features/x-inline.webp", Alt: "A scene.", Discard: func(context.Context) { f.discarded = true }}, nil
}

var workerNow = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

const ours = "https://img.test/features/"

func newWorker(store *fakeStore, illustrator *fakeIllustrator) *inlineimage.Worker {
	return inlineimage.NewWorker(store, illustrator, func() time.Time { return workerNow }, nil, ours)
}

func TestWorkerIllustratesTheFirstEligibleArticle(t *testing.T) {
	store := &fakeStore{articles: []inlineimage.Article{
		{ID: 1, Slug: "short", Content: body("p p p p p"), FeaturedImage: ours + "a.webp"},
		{ID: 2, Slug: "long", Title: "Long", Content: body("p p h p p p h p p p"), FeaturedImage: ours + "b.webp", SourceImage: "https://news.example/og.jpg", Attempts: 1},
		{ID: 3, Slug: "also-long", Content: body("p p p p p p p p"), FeaturedImage: ours + "c.webp"},
	}}
	illustrator := &fakeIllustrator{}
	tried, err := newWorker(store, illustrator).RunOnce(context.Background())
	if err != nil || !tried {
		t.Fatalf("tried %t, err %v", tried, err)
	}
	if !store.since.Equal(workerNow.Add(-7 * 24 * time.Hour)) {
		t.Fatalf("since = %s", store.since)
	}
	if len(illustrator.requests) != 1 {
		t.Fatalf("%d generations; want exactly one per run", len(illustrator.requests))
	}
	request := illustrator.requests[0]
	if request.Slug != "long" || request.Title != "Long" || request.FeaturedImageURL != ours+"b.webp" || request.SourceImageURL != "https://news.example/og.jpg" ||
		request.Section != "Paragraph 4.\n\nParagraph 5.\n\nParagraph 6." {
		t.Fatalf("request = %+v", request)
	}
	if len(store.marks) != 1 || store.marks[0] != (mark{id: 2, ready: true, url: "https://img.test/features/x-inline.webp", alt: "A scene.", after: 5, attempts: 2}) {
		t.Fatalf("marks = %+v", store.marks)
	}
}

func TestWorkerMarksForeignFeaturedImagesFailedForGood(t *testing.T) {
	store := &fakeStore{articles: []inlineimage.Article{
		{ID: 1, Content: body("p p p p p p"), FeaturedImage: "/uploads/copied.jpg"},
		{ID: 2, Slug: "ours", Content: body("p p p p p p"), FeaturedImage: ours + "b.webp"},
	}}
	illustrator := &fakeIllustrator{}
	if _, err := newWorker(store, illustrator).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.marks) != 2 || store.marks[0].id != 1 || store.marks[0].ready || store.marks[0].attempts != inlineimage.MaxAttempts || !strings.Contains(store.marks[0].message, "not one of ours") {
		t.Fatalf("marks = %+v", store.marks)
	}
	if len(illustrator.requests) != 1 || illustrator.requests[0].Slug != "ours" {
		t.Fatalf("requests = %+v", illustrator.requests)
	}
}

func TestWorkerRecordsFailures(t *testing.T) {
	article := inlineimage.Article{ID: 9, Slug: "a", Content: body("p p p p p p"), FeaturedImage: ours + "a.webp", Attempts: 1}
	for _, test := range []struct {
		name     string
		err      error
		marked   bool
		attempts int
	}{
		{"transient failure counts one attempt", errors.New("codex timed out"), true, 2},
		{"permanent failure counts one attempt", publisher.Permanent(illustration.ErrNoCompliantImage), true, 2},
		{"not our image uses up the attempts", publisher.Permanent(illustration.ErrNotOurImage), true, inlineimage.MaxAttempts},
		{"system fault keeps the attempts", publisher.SystemFault(errors.New("codex is not logged in")), false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeStore{articles: []inlineimage.Article{article}}
			_, err := newWorker(store, &fakeIllustrator{err: test.err}).RunOnce(context.Background())
			if test.marked {
				if err != nil || len(store.marks) != 1 || store.marks[0].ready || store.marks[0].attempts != test.attempts || store.marks[0].message == "" {
					t.Fatalf("err %v marks %+v", err, store.marks)
				}
				return
			}
			if err == nil || len(store.marks) != 0 {
				t.Fatalf("err %v marks %+v", err, store.marks)
			}
		})
	}
}

func TestWorkerDiscardsTheImageWhenItCannotBeRecorded(t *testing.T) {
	store := &fakeStore{articles: []inlineimage.Article{{ID: 1, Content: body("p p p p p p"), FeaturedImage: ours + "a.webp"}}, readyErr: errors.New("disk full")}
	illustrator := &fakeIllustrator{}
	if _, err := newWorker(store, illustrator).RunOnce(context.Background()); err == nil || !illustrator.discarded {
		t.Fatalf("err %v discarded %t", err, illustrator.discarded)
	}
}

func TestWorkerDoesNothingWithoutEligibleArticles(t *testing.T) {
	store := &fakeStore{articles: []inlineimage.Article{{ID: 1, Content: body("p p p"), FeaturedImage: ours + "a.webp"}}}
	illustrator := &fakeIllustrator{}
	tried, err := newWorker(store, illustrator).RunOnce(context.Background())
	if err != nil || tried || len(illustrator.requests) != 0 || len(store.marks) != 0 {
		t.Fatalf("tried %t err %v", tried, err)
	}
}

func TestOwnImagePrefix(t *testing.T) {
	for _, test := range []struct{ base, prefix, want string }{
		{"https://bucket.s3.amazonaws.com/", "/features/", "https://bucket.s3.amazonaws.com/features/"},
		{"https://cdn.test", "", "https://cdn.test/"},
		{"", "features", ""},
	} {
		if got := inlineimage.OwnImagePrefix(test.base, test.prefix); got != test.want {
			t.Errorf("OwnImagePrefix(%q, %q) = %q, want %q", test.base, test.prefix, got, test.want)
		}
	}
}
