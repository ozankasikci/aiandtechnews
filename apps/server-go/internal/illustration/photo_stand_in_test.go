package illustration_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func (*fakeRecentPhotos) RecentFeaturedSubjects(context.Context, time.Time) ([]realphoto.FeaturedSubjectUse, error) {
	return nil, nil
}

func TestPublishRecordsExactPersonPhotoWithItsCredit(t *testing.T) {
	for _, tc := range []struct {
		name, subject, kind string
		record, reject      bool
	}{
		{"exact person", "Bret Taylor", realphoto.KindPerson, true, false},
		{"exact product", "Sierra device", "product", false, false},
		{"rollback person and credit", "Bret Taylor", realphoto.KindPerson, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, _ := testutil.OpenDatabase(t)
			if err := migrate.Run(ctx, db, app.Migrations()); err != nil {
				t.Fatal(err)
			}
			if tc.reject {
				if _, err := db.Exec(`CREATE TRIGGER reject_featured_subject BEFORE INSERT ON article_featured_stand_ins
					BEGIN SELECT RAISE(ABORT, 'test subject insert failure'); END`); err != nil {
					t.Fatal(err)
				}
			}

			photo := foundPhoto(t)
			photo.StandIn = nil
			photo.Plan = &realphoto.Plan{Photographable: true, Subject: tc.subject, Kind: tc.kind, Maker: "Sierra"}
			fixture := newFixture(t, illustration.PhotoStep(&fakePhotoFinder{result: photo}))
			fixture.deps.Store = &fakeImageStore{}
			made, err := illustration.NewPipeline(fixture.deps).Illustrate(ctx, fixture.request)
			if err != nil {
				t.Fatal(err)
			}
			articles := publisher.NewSQLiteArticles(db, time.Now)
			id, err := articles.Publish(ctx, publisher.NewArticle{
				Slug: "story", Title: "Story", Excerpt: "Excerpt", Content: "<p>Body.</p>",
				Source: "TechCrunch", SourceURL: "https://techcrunch.com/story", FeaturedImage: made.URL, FeaturedCredit: made.Credit,
			})
			if tc.reject {
				if err == nil || !strings.Contains(err.Error(), "test subject insert failure") {
					t.Fatalf("publish = %v, want the subject insert failure", err)
				}
				var count int
				if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM articles) +
					(SELECT COUNT(*) FROM article_featured_image_credits) +
					(SELECT COUNT(*) FROM article_featured_stand_ins)`).Scan(&count); err != nil || count != 0 {
					t.Fatalf("rows after rollback = %d, err = %v, want none", count, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var creditURL string
			if err := db.QueryRow(`SELECT photo_url FROM article_featured_image_credits WHERE article_id = ?`, id).Scan(&creditURL); err != nil || creditURL != photo.Chosen.ImageURL {
				t.Fatalf("credit photo URL = %q, err = %v", creditURL, err)
			}
			var got realphoto.FeaturedSubjectUse
			err = db.QueryRow(`SELECT subject, kind, maker FROM article_featured_stand_ins WHERE article_id = ?`, id).
				Scan(&got.Subject, &got.Kind, &got.Maker)
			if tc.record {
				want := realphoto.FeaturedSubjectUse{Subject: tc.subject, Kind: tc.kind, Maker: "Sierra"}
				if err != nil || got != want {
					t.Fatalf("recorded use = %+v, err = %v, want %+v", got, err, want)
				}
			} else if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("recorded use = %+v, err = %v, want no row", got, err)
			}
		})
	}
}

type fakeRecentFeaturedSubjects struct {
	fakeRecentPhotos
	used          []realphoto.FeaturedSubjectUse
	readErr       error
	sinceStandIns time.Time
}

func (f *fakeRecentFeaturedSubjects) RecentFeaturedSubjects(_ context.Context, since time.Time) ([]realphoto.FeaturedSubjectUse, error) {
	f.sinceStandIns = since
	return f.used, f.readErr
}

func TestPhotoStepReadsRecentFeaturedSubjectsAndCarriesTheChosenStandInToPublish(t *testing.T) {
	chosen := realphoto.FeaturedSubjectUse{Subject: "Bret Taylor", Kind: realphoto.KindPerson, Maker: "Sierra"}
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
			recent := &fakeRecentFeaturedSubjects{
				fakeRecentPhotos: fakeRecentPhotos{urls: []string{"https://upload.wikimedia.org/recent.jpg"}},
				used:             []realphoto.FeaturedSubjectUse{{Subject: "Another headquarters", Kind: realphoto.StandInPlace, Maker: "Another Company"}},
				readErr:          readErr,
			}
			fixture.deps.RecentPhotos = recent
			made, err := illustration.NewPipeline(fixture.deps).Illustrate(context.Background(), fixture.request)
			if err != nil {
				t.Fatal(err)
			}
			if made.Credit == nil || made.Credit.SubjectUse == nil || *made.Credit.SubjectUse != chosen {
				t.Fatalf("credit = %+v, want stand-in %+v", made.Credit, chosen)
			}
			if !recent.sinceStandIns.Equal(now.Add(-7*24*time.Hour)) || !recent.since.Equal(now.Add(-30*24*time.Hour)) {
				t.Fatalf("stand-ins since %v, files since %v", recent.sinceStandIns, recent.since)
			}
			request := finder.requests[0]
			want := recent.used
			if readErr != nil {
				want = nil
				if !strings.Contains(logs.String(), "recent featured subjects unavailable") || !strings.Contains(logs.String(), readErr.Error()) {
					t.Errorf("logs = %q, want recent stand-in read failure", logs.String())
				}
			}
			if !slices.Equal(request.RecentFeaturedSubjects, want) || !slices.Equal(request.Exclude, recent.urls) {
				t.Errorf("request = %+v, want recent stand-ins %+v and excluded files %v", request, want, recent.urls)
			}
		})
	}
}
