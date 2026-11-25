package main

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func TestWriteRunExcludesStoredAndNewlyChosenStandIns(t *testing.T) {
	path := photoTryDatabase(t)
	writer, err := database.OpenExisting(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(`
		UPDATE article_featured_image_credits SET created_at = datetime('now','-6 days') WHERE article_id = 1;
		INSERT INTO article_featured_stand_ins(article_id,subject,kind,maker) VALUES
		(1,'Existing headquarters','place','Existing Labs'), (2,'Old headquarters','place','Old Labs');`); err != nil {
		writer.Close()
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db, err := openDatabaseCopy(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	out, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	stored := realphoto.StandInUse{Subject: "Existing headquarters", Kind: realphoto.StandInPlace, Maker: "Existing Labs"}
	selected := realphoto.StandInUse{Subject: "Another headquarters", Kind: realphoto.StandInPlace, Maker: "Another Labs"}
	var requests []string
	var photo bytes.Buffer
	if err := png.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 16, 9))); err != nil {
		t.Fatal(err)
	}
	finder := stubPhotoFinder(func(_ context.Context, request realphoto.Request) realphoto.Result {
		requests = append(requests, request.Slug)
		want := []realphoto.StandInUse{stored}
		if request.Slug == "two" {
			want = append(want, selected)
		}
		if !slices.Equal(request.RecentStandIns, want) {
			t.Errorf("%s recent stand-ins = %+v, want %+v", request.Slug, request.RecentStandIns, want)
		}
		if request.Slug == "two" {
			return realphoto.Result{Reason: "no usable photo of a stand-in subject; stand-in Another headquarters skipped as recently used"}
		}
		candidate := realphoto.Candidate{Source: realphoto.KindCommons, ImageURL: "https://photo.test/chosen.png", PageURL: "https://photo.test/file", CaptureYear: 2024, Usable: true}
		return realphoto.Result{
			Found: true, Image: photo.Bytes(), Credit: &realphoto.Credit{Text: "Photo: Author"}, Chosen: &candidate,
			Plan: &realphoto.Plan{Maker: selected.Maker}, StandIn: &realphoto.StandIn{Subject: selected.Subject, Kind: selected.Kind},
			Candidates: []realphoto.Candidate{candidate, {Source: realphoto.KindCommons, Reason: "unknown age"}},
		}
	})
	if err := writeRun(ctx, db, out, finder, "one,two", 0); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(requests, []string{"one", "two"}) {
		t.Fatalf("requests = %v", requests)
	}
	html, err := out.ReadFile(reportName)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1 of 2 articles got a photo", "Capture year: 2024", "Capture year: unknown", "stand-in Another headquarters skipped as recently used"} {
		if !strings.Contains(string(html), want) {
			t.Errorf("report lacks %q", want)
		}
	}
}

func TestRunExplainsMissingPhotoMigrations(t *testing.T) {
	for _, tc := range []struct {
		installed int
		want      string
	}{
		{17, "migration 18 is missing"},
		{18, "migration 19 is missing"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			ctx := context.Background()
			db, path := testutil.OpenDatabase(t)
			var installed []migrate.Descriptor
			for _, migration := range app.Migrations() {
				if migration.Version <= int64(tc.installed) {
					installed = append(installed, migration)
				}
			}
			if err := migrate.Run(ctx, db, installed); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			err := run(ctx, []string{"-db", path, "-out", t.TempDir()}, func(string) string { return "" })
			if err == nil || !strings.Contains(err.Error(), "run cmd/migrate on this copy first: "+tc.want) {
				t.Fatalf("run = %v, want copy migration instructions", err)
			}
		})
	}
}
