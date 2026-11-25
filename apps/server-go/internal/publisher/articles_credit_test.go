package publisher_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

var photoCredit = &publisher.ImageCredit{
	Kind: "commons", Text: "Photo: Jane Doe / CC BY 4.0, via Wikimedia Commons",
	URL: "https://commons.wikimedia.org/wiki/File:X.jpg", License: "CC BY 4.0",
	LicenseURL: "https://creativecommons.org/licenses/by/4.0", PhotoURL: "https://upload.wikimedia.org/x.jpg",
	Alt: "Bret Taylor on stage.",
}

func TestPublishStoresTheFeaturedCreditOnlyWhenPresent(t *testing.T) {
	db := openDB(t)
	articles := publisher.NewSQLiteArticles(db, func() time.Time { return publishNow })
	ctx := context.Background()

	with := newArticle("sierra-raises", "https://techcrunch.com/sierra", "Sierra raises a round", "<p>Body</p>")
	with.FeaturedCredit = photoCredit
	id, err := articles.Publish(ctx, with)
	if err != nil {
		t.Fatal(err)
	}
	var got publisher.ImageCredit
	err = db.QueryRow(`SELECT kind, credit, credit_url, license, license_url, photo_url, alt FROM article_featured_image_credits WHERE article_id = ?`, id).
		Scan(&got.Kind, &got.Text, &got.URL, &got.License, &got.LicenseURL, &got.PhotoURL, &got.Alt)
	if err != nil {
		t.Fatal(err)
	}
	if got != *photoCredit {
		t.Fatalf("stored credit = %+v", got)
	}

	if _, err := articles.Publish(ctx, newArticle("drawn", "https://techcrunch.com/drawn", "A drawn story", "<p>Body</p>")); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM article_featured_image_credits`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("credit rows = %d, want 1: a drawn image has no credit", rows)
	}
}

func TestPublishStoresTheFeaturedImageURLWithItsCredit(t *testing.T) {
	db := openDB(t)
	articles := publisher.NewSQLiteArticles(db, func() time.Time { return publishNow })
	ctx := context.Background()
	article := newArticle("sierra-raises", "https://techcrunch.com/sierra", "Sierra raises a round", "<p>Body</p>")
	article.FeaturedCredit = photoCredit
	id, err := articles.Publish(ctx, article)
	if err != nil {
		t.Fatal(err)
	}
	var imageURL string
	if err := db.QueryRow(`SELECT image_url FROM article_featured_image_credits WHERE article_id = ?`, id).Scan(&imageURL); err != nil {
		t.Fatal(err)
	}
	if imageURL != article.FeaturedImage {
		t.Fatalf("stored credit image URL = %q, want %q", imageURL, article.FeaturedImage)
	}

	if _, err := db.Exec(`UPDATE articles SET featured_image = 'https://img.test/features/replacement.webp' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	urls, err := articles.RecentFeaturedPhotoURLs(ctx, publishNow.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 || urls[0] != photoCredit.PhotoURL {
		t.Fatalf("recent photo URLs after replacement = %v, want [%s]", urls, photoCredit.PhotoURL)
	}
}

func TestPublishRollsBackFeaturedCreditWhenCandidateIsNotProcessing(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	store, candidateID := claimedCandidate(t, db)
	if err := store.MarkFailed(ctx, candidateID, "stopped elsewhere", publishNow); err != nil {
		t.Fatal(err)
	}
	articles := publisher.NewSQLiteArticles(db, func() time.Time { return publishNow })
	article := newArticle("sierra-raises", "https://techcrunch.com/sierra", "Sierra raises a round", "<p>Body</p>")
	article.CandidateID = candidateID
	article.FeaturedCredit = photoCredit

	if _, err := articles.Publish(ctx, article); !errors.Is(err, publisher.ErrCandidateNotProcessing) {
		t.Fatalf("err = %v, want ErrCandidateNotProcessing", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM article_featured_image_credits`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("credit rows = %d, err = %v; the insert must roll back", count, err)
	}
}

func TestRecentFeaturedPhotoURLsHonoursTheWindow(t *testing.T) {
	db := openDB(t)
	now := publishNow
	articles := publisher.NewSQLiteArticles(db, func() time.Time { return now })
	ctx := context.Background()

	old := newArticle("old", "https://techcrunch.com/old", "An old story", "<p>Body</p>")
	old.FeaturedCredit = &publisher.ImageCredit{Kind: "commons", Text: "Photo: A", PhotoURL: "https://upload.wikimedia.org/old.jpg"}
	if _, err := articles.Publish(ctx, old); err != nil {
		t.Fatal(err)
	}
	now = publishNow.Add(40 * 24 * time.Hour)
	fresh := newArticle("fresh", "https://techcrunch.com/fresh", "A fresh story", "<p>Body</p>")
	fresh.FeaturedCredit = &publisher.ImageCredit{Kind: "official", Text: "Image: Nvidia", PhotoURL: "https://nvidia.com/rack.jpg"}
	if _, err := articles.Publish(ctx, fresh); err != nil {
		t.Fatal(err)
	}

	urls, err := articles.RecentFeaturedPhotoURLs(ctx, now.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 || urls[0] != "https://nvidia.com/rack.jpg" {
		t.Fatalf("recent photo URLs = %v, want only the fresh one", urls)
	}
}
