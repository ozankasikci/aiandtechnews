package illustration_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

type fakePhotoFinder struct {
	result   realphoto.Result
	requests []realphoto.Request
}

func (f *fakePhotoFinder) Find(_ context.Context, request realphoto.Request) realphoto.Result {
	f.requests = append(f.requests, request)
	return f.result
}

type fakeRecentPhotos struct {
	urls  []string
	err   error
	since time.Time
}

func (f *fakeRecentPhotos) RecentFeaturedPhotoURLs(_ context.Context, since time.Time) ([]string, error) {
	f.since = since
	return f.urls, f.err
}

func foundPhoto(t *testing.T) realphoto.Result {
	t.Helper()
	return realphoto.Result{
		Found: true, Image: personCutout(t, 400, 400), Alt: "Bret Taylor on stage.",
		Credit: &realphoto.Credit{Kind: realphoto.KindCommons, Text: "Photo: Jane Doe / CC BY 4.0, via Wikimedia Commons",
			URL: "https://commons.wikimedia.org/wiki/File:X.jpg", License: "CC BY 4.0", LicenseURL: "https://creativecommons.org/licenses/by/4.0"},
		Chosen:  &realphoto.Candidate{ImageURL: "https://upload.wikimedia.org/x.jpg", Verdict: &realphoto.Verdict{Quality: 9, FocusX: 0.5, FocusY: 0.2}},
		Reason:  "Wikimedia Commons photo of stand-in Bret Taylor (person), score 9",
		StandIn: &realphoto.StandIn{Subject: "Bret Taylor"},
	}
}

func TestPhotoStepWinsWithoutAnalysisOrGeneration(t *testing.T) {
	finder := &fakePhotoFinder{result: foundPhoto(t)}
	gem := &fakeProvider{name: "gemini", attempts: 3, image: solidPNG(t, 64, 36)}
	fixture := newFixture(t, illustration.PhotoStep(finder), illustration.ProviderStep(gem))
	recent := &fakeRecentPhotos{urls: []string{"https://upload.wikimedia.org/used.jpg"}}
	fixture.deps.RecentPhotos = recent
	fixture.request.Content = "<p>Sierra raised <b>$1.75 billion</b>.</p>"
	fixture.request.SourceURL = "https://techcrunch.com/sierra"
	var sourceHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sourceHits.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	fixture.request.ReferenceImageURL = server.URL + "/source.png"

	result, err := fixture.produce(t)
	if err != nil {
		t.Fatal(err)
	}
	if result.Report.Provider != "photo" || fixture.analyzer.calls != 0 || len(gem.requests) != 0 || len(fixture.reviewer.reviewed) != 0 {
		t.Fatalf("report %+v analyzer calls %d gemini calls %d", result.Report, fixture.analyzer.calls, len(gem.requests))
	}
	if hits := sourceHits.Load(); hits != 0 {
		t.Fatalf("source image fetched %d times", hits)
	}
	cropped, _, err := image.Decode(bytes.NewReader(result.Image))
	if err != nil {
		t.Fatal(err)
	}
	if bounds := cropped.Bounds(); bounds.Dx() != 400 || bounds.Dy() != 225 {
		t.Fatalf("image bounds %v, want a 400x225 crop", bounds)
	}
	if _, _, _, alpha := cropped.At(200, 0).RGBA(); alpha != 0 {
		t.Fatal("crop lost the top of the image despite the high focus point")
	}
	if photo := result.Report.Photo; photo == nil || photo.StandIn != "Bret Taylor" || photo.Quality != 9 {
		t.Fatalf("photo report = %+v", photo)
	}
	want := publisher.ImageCredit{Kind: "commons", Text: "Photo: Jane Doe / CC BY 4.0, via Wikimedia Commons",
		URL: "https://commons.wikimedia.org/wiki/File:X.jpg", License: "CC BY 4.0", LicenseURL: "https://creativecommons.org/licenses/by/4.0",
		PhotoURL: "https://upload.wikimedia.org/x.jpg", Alt: "Bret Taylor on stage."}
	if result.Credit == nil || *result.Credit != want {
		t.Fatalf("credit = %+v", result.Credit)
	}
	request := finder.requests[0]
	if request.Slot != realphoto.SlotFeatured || request.SourceURL != "https://techcrunch.com/sierra" ||
		strings.Contains(request.Text, "<") || !strings.Contains(request.Text, "$1.75 billion") {
		t.Fatalf("finder request = %+v", request)
	}
	if len(request.Exclude) != 1 || request.Exclude[0] != "https://upload.wikimedia.org/used.jpg" {
		t.Fatalf("exclude = %v", request.Exclude)
	}
	if recent.since.IsZero() {
		t.Fatal("the recent photos were not asked for")
	}
}

func TestPhotoStepHandsOverWhenNoPhotoPasses(t *testing.T) {
	finder := &fakePhotoFinder{result: realphoto.Result{Reason: "no usable photo of a stand-in subject"}}
	gem := &fakeProvider{name: "gemini", attempts: 3, image: solidPNG(t, 64, 36)}
	fixture := newFixture(t, illustration.PhotoStep(finder), illustration.ProviderStep(gem))

	result, err := fixture.produce(t)
	if err != nil {
		t.Fatal(err)
	}
	if result.Report.Provider != "gemini" || result.Credit != nil || fixture.analyzer.calls != 1 {
		t.Fatalf("report %+v credit %+v analyzer calls %d", result.Report, result.Credit, fixture.analyzer.calls)
	}
	if len(result.Report.Attempts) == 0 || result.Report.Attempts[0].Provider != "photo" {
		t.Fatalf("attempts = %+v, want the photo step first", result.Report.Attempts)
	}
}

func TestPhotoStepIsSkippedWhenOnlyADrawingIsWanted(t *testing.T) {
	finder := &fakePhotoFinder{result: foundPhoto(t)}
	gem := &fakeProvider{name: "gemini", attempts: 3, image: solidPNG(t, 64, 36)}
	fixture := newFixture(t, illustration.PhotoStep(finder), illustration.ProviderStep(gem))

	result, err := illustration.NewPipeline(fixture.deps).ProduceGenerated(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if len(finder.requests) != 0 || result.Report.Provider != "gemini" {
		t.Fatalf("finder calls %d provider %q", len(finder.requests), result.Report.Provider)
	}
}

func TestProduceGeneratedSkipsPhotoAfterGenerationFailure(t *testing.T) {
	finder := &fakePhotoFinder{result: foundPhoto(t)}
	codex := &fakeProvider{name: "codex", attempts: 1, errs: []error{errors.New("no image")}}
	gem := &fakeProvider{name: "gemini", attempts: 1, image: solidPNG(t, 64, 36)}
	fixture := newFixture(t, illustration.ProviderStep(codex), illustration.PhotoStep(finder), illustration.ProviderStep(gem))

	result, err := illustration.NewPipeline(fixture.deps).ProduceGenerated(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if len(codex.requests) != 1 || len(finder.requests) != 0 || result.Report.Provider != "gemini" || result.Credit != nil {
		t.Fatalf("codex calls %d, finder calls %d, provider %q, credit %+v", len(codex.requests), len(finder.requests), result.Report.Provider, result.Credit)
	}
}

func TestAChainThatOnlyFindsNoPhotoFailsForGood(t *testing.T) {
	finder := &fakePhotoFinder{result: realphoto.Result{Reason: "no photographable subject"}}
	fixture := newFixture(t, illustration.PhotoStep(finder))

	_, err := fixture.produce(t)
	if err == nil || !publisher.IsPermanent(err) || !strings.Contains(err.Error(), illustration.ErrNoPhoto.Error()) {
		t.Fatalf("err = %v, want a permanent failure that says no usable real photo", err)
	}
}

func TestIllustrateReturnsThePhotoCredit(t *testing.T) {
	finder := &fakePhotoFinder{result: foundPhoto(t)}
	fixture := newFixture(t, illustration.PhotoStep(finder))
	fixture.deps.Store = &fakeImageStore{}

	made, err := illustration.NewPipeline(fixture.deps).Illustrate(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if made.Credit == nil || made.Credit.PhotoURL != "https://upload.wikimedia.org/x.jpg" || made.URL == "" {
		t.Fatalf("illustration = %+v", made)
	}
}

func TestIllustrateStoresAWidePhotoAsScaledWebP(t *testing.T) {
	photo := foundPhoto(t)
	photo.Image = solidPNG(t, 3200, 1800)
	finder := &fakePhotoFinder{result: photo}
	fixture := newFixture(t, illustration.PhotoStep(finder))
	store := &fakeImageStore{}
	fixture.deps.Store = store

	if _, err := illustration.NewPipeline(fixture.deps).Illustrate(context.Background(), fixture.request); err != nil {
		t.Fatal(err)
	}
	decoded, err := imaging.Decode(store.stored)
	if err != nil || imaging.SniffMIME(store.stored) != "image/webp" {
		t.Fatalf("stored image is not WebP: %v", err)
	}
	if bounds := decoded.Bounds(); bounds.Dx() != 2000 || bounds.Dy() != 1125 {
		t.Fatalf("stored %dx%d, want 2000x1125", bounds.Dx(), bounds.Dy())
	}
}

func TestPhotoAfterGenerationFailureDoesNotRecordAnIllustrationStyle(t *testing.T) {
	finder := &fakePhotoFinder{result: foundPhoto(t)}
	gem := &fakeProvider{name: "gemini", attempts: 1, errs: []error{errors.New("no image")}}
	fixture := newFixture(t, illustration.ProviderStep(gem), illustration.PhotoStep(finder))
	history := &fakeHistory{}
	fixture.deps.History = history
	fixture.deps.Store = &fakeImageStore{}

	made, err := illustration.NewPipeline(fixture.deps).Illustrate(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if len(gem.requests) != 1 || made.Credit == nil || len(history.recorded) != 0 {
		t.Fatalf("generator calls %d, credit %+v, recorded styles %+v", len(gem.requests), made.Credit, history.recorded)
	}
}
