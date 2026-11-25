package illustration_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

func (*fakeRecentPhotos) RecentFeaturedStandIns(context.Context, time.Time) ([]realphoto.StandInUse, error) {
	return nil, nil
}

type fakeRecentStandIns struct {
	fakeRecentPhotos
	used          []realphoto.StandInUse
	readErr       error
	sinceStandIns time.Time
}

func (f *fakeRecentStandIns) RecentFeaturedStandIns(_ context.Context, since time.Time) ([]realphoto.StandInUse, error) {
	f.sinceStandIns = since
	return f.used, f.readErr
}

func TestPhotoStepReadsRecentStandInsAndCarriesTheChosenStandInToPublish(t *testing.T) {
	chosen := realphoto.StandInUse{Subject: "Bret Taylor", Kind: realphoto.KindPerson, Maker: "Sierra"}
	for _, readErr := range []error{nil, errors.New("database busy")} {
		name := "successful read"
		if readErr != nil {
			name = "failed read"
		}
		t.Run(name, func(t *testing.T) {
			photo := foundPhoto(t)
			photo.Plan = &realphoto.Plan{Maker: chosen.Maker}
			photo.StandIn.Kind = chosen.Kind
			finder := &fakePhotoFinder{result: photo}
			fixture := newFixture(t, illustration.PhotoStep(finder))
			fixture.deps.Store = &fakeImageStore{}
			now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
			fixture.deps.Now = func() time.Time { return now }
			var logs bytes.Buffer
			fixture.deps.Logger = slog.New(slog.NewTextHandler(&logs, nil))
			recent := &fakeRecentStandIns{
				fakeRecentPhotos: fakeRecentPhotos{urls: []string{"https://upload.wikimedia.org/recent.jpg"}},
				used:             []realphoto.StandInUse{{Subject: "Another headquarters", Kind: realphoto.StandInPlace, Maker: "Another Company"}},
				readErr:          readErr,
			}
			fixture.deps.RecentPhotos = recent
			made, err := illustration.NewPipeline(fixture.deps).Illustrate(context.Background(), fixture.request)
			if err != nil {
				t.Fatal(err)
			}
			if made.Credit == nil || made.Credit.StandIn == nil || *made.Credit.StandIn != chosen {
				t.Fatalf("credit = %+v, want stand-in %+v", made.Credit, chosen)
			}
			if !recent.sinceStandIns.Equal(now.Add(-7*24*time.Hour)) || !recent.since.Equal(now.Add(-30*24*time.Hour)) {
				t.Fatalf("stand-ins since %v, files since %v", recent.sinceStandIns, recent.since)
			}
			request := finder.requests[0]
			want := recent.used
			if readErr != nil {
				want = nil
				if !strings.Contains(logs.String(), "recent featured stand-ins unavailable") || !strings.Contains(logs.String(), readErr.Error()) {
					t.Errorf("logs = %q, want recent stand-in read failure", logs.String())
				}
			}
			if !slices.Equal(request.RecentStandIns, want) || !slices.Equal(request.Exclude, recent.urls) {
				t.Errorf("request = %+v, want recent stand-ins %+v and excluded files %v", request, want, recent.urls)
			}
		})
	}
}
