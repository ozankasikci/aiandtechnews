package publisher_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

func TestPublishRecordsOnlyStandInsAndReadsThemSinceTheCutoff(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	now := publishNow
	articles := publisher.NewSQLiteArticles(db, func() time.Time { return now })
	cutoff := now.Add(-7 * 24 * time.Hour)
	old := realphoto.StandInUse{Subject: "Old headquarters", Kind: realphoto.StandInPlace, Maker: "Old Company"}
	fresh := realphoto.StandInUse{Subject: "Bret Taylor", Kind: realphoto.KindPerson, Maker: "Sierra"}
	creditWithStandIn := func(standIn realphoto.StandInUse) *publisher.ImageCredit {
		credit := *photoCredit
		credit.StandIn = &standIn
		return &credit
	}
	for _, tc := range []struct {
		slug   string
		at     time.Time
		credit *publisher.ImageCredit
	}{
		{"old", cutoff.Add(-time.Second), creditWithStandIn(old)},
		{"fresh", cutoff, creditWithStandIn(fresh)},
		{"exact", publishNow, photoCredit},
		{"drawn", publishNow, nil},
	} {
		now = tc.at
		article := newArticle(tc.slug, "https://techcrunch.com/"+tc.slug, tc.slug, "<p>Body</p>")
		article.FeaturedCredit = tc.credit
		id, err := articles.Publish(ctx, article)
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM article_featured_stand_ins WHERE article_id = ?`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		want := 0
		if tc.credit != nil && tc.credit.StandIn != nil {
			want = 1
		}
		if count != want {
			t.Errorf("%s stand-in rows = %d, want %d", tc.slug, count, want)
		}
	}
	for _, tc := range []struct {
		since time.Time
		want  []realphoto.StandInUse
	}{
		{cutoff, []realphoto.StandInUse{fresh}},
		{cutoff.Add(-time.Second), []realphoto.StandInUse{fresh, old}},
		{cutoff.Add(time.Second), nil},
	} {
		got, err := articles.RecentFeaturedStandIns(ctx, tc.since)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("stand-ins since %v = %+v, err = %v, want %+v", tc.since, got, err, tc.want)
		}
	}
}

func TestPublishRollsBackStandInAndCreditTogether(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	store, candidateID := claimedCandidate(t, db)
	if err := store.MarkFailed(ctx, candidateID, "stopped elsewhere", publishNow); err != nil {
		t.Fatal(err)
	}
	articles := publisher.NewSQLiteArticles(db, func() time.Time { return publishNow })
	article := newArticle("sierra-raises", "https://techcrunch.com/sierra", "Sierra raises a round", "<p>Body</p>")
	article.CandidateID = candidateID
	credit := *photoCredit
	credit.StandIn = &realphoto.StandInUse{Subject: "Bret Taylor", Kind: realphoto.KindPerson, Maker: "Sierra"}
	article.FeaturedCredit = &credit
	if _, err := articles.Publish(ctx, article); !errors.Is(err, publisher.ErrCandidateNotProcessing) {
		t.Fatalf("err = %v, want ErrCandidateNotProcessing", err)
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM article_featured_stand_ins`,
		`SELECT COUNT(*) FROM article_featured_image_credits`,
	} {
		var count int
		if err := db.QueryRow(query).Scan(&count); err != nil || count != 0 {
			t.Errorf("%s = %d, err = %v; the insert must roll back", query, count, err)
		}
	}
}
