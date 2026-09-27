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
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

type mark struct {
	id       int64
	ready    bool
	url      string
	alt      string
	after    int
	attempts int
	message  string
	credit   *realphoto.Credit
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

func (f *fakeStore) MarkReady(_ context.Context, id int64, url, alt string, after, attempts int, credit *realphoto.Credit) error {
	if f.readyErr != nil {
		return f.readyErr
	}
	f.marks = append(f.marks, mark{id: id, ready: true, url: url, alt: alt, after: after, attempts: attempts, credit: credit})
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

type fakeFinder struct {
	result   realphoto.Result
	requests []realphoto.Request
}

func (f *fakeFinder) Find(_ context.Context, request realphoto.Request) realphoto.Result {
	f.requests = append(f.requests, request)
	return f.result
}

type fakePhotoStore struct {
	stored    []byte
	err       error
	discarded bool
}

func (f *fakePhotoStore) StoreInlinePhoto(_ context.Context, slug string, photo []byte) (illustration.InlineIllustration, error) {
	if f.err != nil {
		return illustration.InlineIllustration{}, f.err
	}
	f.stored = photo
	return illustration.InlineIllustration{URL: "https://img.test/features/" + slug + "-inline.webp", Discard: func(context.Context) { f.discarded = true }}, nil
}

var photoCredit = &realphoto.Credit{Kind: "official", Text: "Image: WiCi", URL: "https://wici.ai/wici-one"}

func photoArticle() inlineimage.Article {
	return inlineimage.Article{ID: 4, Slug: "wici", Title: "WiCi One", Content: body("p p p p p p"), FeaturedImage: ours + "a.webp", SourceURL: "https://www.engadget.com/wici", Attempts: 1}
}

func TestWorkerPrefersAFoundPhoto(t *testing.T) {
	store := &fakeStore{articles: []inlineimage.Article{photoArticle()}}
	illustrator := &fakeIllustrator{}
	finder := &fakeFinder{result: realphoto.Result{Found: true, Image: []byte("photo"), Alt: "The WiCi One robot.", Credit: photoCredit, Reason: "official"}}
	photos := &fakePhotoStore{}
	if _, err := newWorker(store, illustrator).WithPhotos(finder, photos).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(illustrator.requests) != 0 {
		t.Fatal("drew an illustration although a photo was found")
	}
	if len(finder.requests) != 1 || finder.requests[0].SourceURL != "https://www.engadget.com/wici" || finder.requests[0].Title != "WiCi One" || !strings.Contains(finder.requests[0].Text, "Paragraph 6.") {
		t.Fatalf("finder requests = %+v", finder.requests)
	}
	if string(photos.stored) != "photo" || len(store.marks) != 1 ||
		store.marks[0] != (mark{id: 4, ready: true, url: "https://img.test/features/wici-inline.webp", alt: "The WiCi One robot.", after: 4, attempts: 2, credit: photoCredit}) {
		t.Fatalf("marks = %+v", store.marks)
	}
}

func TestWorkerDrawsWhenNoPhotoIsFoundOrStored(t *testing.T) {
	for name, test := range map[string]struct {
		result realphoto.Result
		err    error
	}{
		"no photo":         {result: realphoto.Result{Reason: "no photographable subject"}},
		"photo not stored": {result: realphoto.Result{Found: true, Image: []byte("photo"), Credit: photoCredit}, err: errors.New("s3 down")},
	} {
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{articles: []inlineimage.Article{photoArticle()}}
			illustrator := &fakeIllustrator{}
			worker := newWorker(store, illustrator).WithPhotos(&fakeFinder{result: test.result}, &fakePhotoStore{err: test.err})
			if _, err := worker.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(illustrator.requests) != 1 || len(store.marks) != 1 || store.marks[0].credit != nil || store.marks[0].url != "https://img.test/features/x-inline.webp" {
				t.Fatalf("requests %d marks %+v", len(illustrator.requests), store.marks)
			}
		})
	}
}

func TestWorkerDiscardsThePhotoWhenItCannotBeRecorded(t *testing.T) {
	store := &fakeStore{articles: []inlineimage.Article{photoArticle()}, readyErr: errors.New("disk full")}
	photos := &fakePhotoStore{}
	finder := &fakeFinder{result: realphoto.Result{Found: true, Image: []byte("photo"), Credit: photoCredit}}
	if _, err := newWorker(store, &fakeIllustrator{}).WithPhotos(finder, photos).RunOnce(context.Background()); err == nil || !photos.discarded {
		t.Fatalf("err %v discarded %t", err, photos.discarded)
	}
}

// A five-paragraph story is too short for an illustration but still gets a
// real photo, two paragraphs before the end; without one it is marked done.
func TestWorkerGivesShortArticlesOnlyRealPhotos(t *testing.T) {
	short := photoArticle()
	short.Content = body("p p p p p")
	store := &fakeStore{articles: []inlineimage.Article{short}}
	illustrator := &fakeIllustrator{}
	finder := &fakeFinder{result: realphoto.Result{Found: true, Image: []byte("photo"), Alt: "The WiCi One.", Credit: photoCredit}}
	if _, err := newWorker(store, illustrator).WithPhotos(finder, &fakePhotoStore{}).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(illustrator.requests) != 0 || len(store.marks) != 1 || !store.marks[0].ready || store.marks[0].after != 3 {
		t.Fatalf("marks = %+v, drawings = %d", store.marks, len(illustrator.requests))
	}

	store = &fakeStore{articles: []inlineimage.Article{short}}
	if _, err := newWorker(store, illustrator).WithPhotos(&fakeFinder{}, &fakePhotoStore{}).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(illustrator.requests) != 0 || len(store.marks) != 1 || store.marks[0].ready || store.marks[0].attempts != inlineimage.MaxAttempts {
		t.Fatalf("no photo: marks = %+v, drawings = %d", store.marks, len(illustrator.requests))
	}
}
