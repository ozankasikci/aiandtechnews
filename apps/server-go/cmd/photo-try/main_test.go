package main

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func TestRunTriesEachSlugOnceAndCanReplaceAnExistingReport(t *testing.T) {
	ctx := context.Background()
	path := photoTryDatabase(t)
	out := t.TempDir()
	args := []string{"-db", path, "-slugs", "one, one,,one", "-out", out}
	// An empty API key makes the planner fail locally, without a network call.
	getenv := func(string) string { return "" }
	reportPath := filepath.Join(out, "report.html")
	if err := run(ctx, args, getenv); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(first), "<h2>Article one</h2>") != 1 || !strings.Contains(string(first), "0 of 1 articles") {
		t.Error("report must show exactly one attempt for the repeated slug")
	}
	if err := os.WriteFile(reportPath, []byte(strings.Repeat("stale report", 400)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, args, getenv); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(reportPath)
	if err != nil || string(second) != string(first) {
		t.Fatalf("rerun report did not replace the old report: %v", err)
	}
}

func photoTryDatabase(t *testing.T) string {
	t.Helper()
	db, path := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO categories(id,name,slug,description,color) VALUES (1,'AI','ai','AI','#111111');
INSERT INTO authors(id,name,email,password_hash,role) VALUES (1,'E','e@example.invalid','fake','admin');
INSERT INTO articles(id,title,slug,excerpt,content,category_id,author_id,status,published_at,created_at,updated_at) VALUES
(1,'Article one','one','e','<h2>Heading</h2><p>Body <strong>text.</strong></p>',1,1,'published','2026-11-25 10:00:00','x','x'),
(2,'Article two','two','e','<p>Another body.</p>',1,1,'published','2026-11-25 09:00:00','x','x');
INSERT INTO article_featured_image_credits(article_id,image_url,kind,credit,photo_url,created_at) VALUES
(1,'https://img.test/1.webp','commons','Photo: Author','https://photo.test/recent.png',datetime('now','-29 days')),
(2,'https://img.test/2.webp','commons','Photo: Author','https://photo.test/old.png',datetime('now','-31 days'));`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

type stubPhotoFinder func(context.Context, realphoto.Request) realphoto.Result

func (f stubPhotoFinder) Find(ctx context.Context, request realphoto.Request) realphoto.Result {
	return f(ctx, request)
}

func TestWriteRunMatchesPipelineInputsAndReplacesChosenPhoto(t *testing.T) {
	ctx := context.Background()
	db, err := openDatabaseCopy(ctx, photoTryDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	out, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	const recentURL, chosenURL = "https://photo.test/recent.png", "https://photo.test/chosen.png"
	var selected bytes.Buffer
	if err := png.Encode(&selected, image.NewRGBA(image.Rect(0, 0, 20, 20))); err != nil {
		t.Fatal(err)
	}
	finder := stubPhotoFinder(func(_ context.Context, request realphoto.Request) realphoto.Result {
		want := []string{recentURL}
		if request.Slug == "two" {
			want = append(want, chosenURL)
		}
		if !slices.Equal(request.Exclude, want) || request.Slot != realphoto.SlotFeatured {
			t.Errorf("request for %s = %+v, want exclusions %v and featured slot", request.Slug, request, want)
		}
		if request.Slug == "two" {
			return realphoto.Result{Reason: "no photo"}
		}
		if request.Text != "Heading Body text." {
			t.Errorf("planner text = %q, want stripped article including its heading", request.Text)
		}
		return realphoto.Result{
			Found: true, Image: selected.Bytes(), Credit: &realphoto.Credit{Text: "Photo: Author"},
			Chosen: &realphoto.Candidate{ImageURL: chosenURL, Verdict: &realphoto.Verdict{Match: realphoto.MatchExact, Quality: 9, Alt: "A photo.", Notes: "Clear.", FocusX: .5, FocusY: .5}},
		}
	})
	if err := writeRun(ctx, db, out, finder, "one,two", 0); err != nil {
		t.Fatal(err)
	}
	first, err := out.ReadFile(reportName)
	if err != nil || !strings.Contains(string(first), "Cropped: yes") || !strings.Contains(string(first), "Notes: Clear.") {
		t.Errorf("first run omitted the selected verdict or crop: %v", err)
	}
	selected.Reset()
	if err := png.Encode(&selected, image.NewRGBA(image.Rect(0, 0, 16, 9))); err != nil {
		t.Fatal(err)
	}
	if err := writeRun(ctx, db, out, finder, "one", 0); err != nil {
		t.Fatal(err)
	}
	photo, err := out.ReadFile("one.png")
	if err != nil || !bytes.Equal(photo, selected.Bytes()) {
		t.Errorf("rerun did not replace the old photo: %v", err)
	}
	second, err := out.ReadFile(reportName)
	if err != nil || !strings.Contains(string(second), "Cropped: no") {
		t.Errorf("rerun omitted the uncropped status: %v", err)
	}
}

func TestOpenDatabaseCopyIsReadOnlyWithoutSidecarWrites(t *testing.T) {
	db, path := testutil.OpenDatabase(t)
	if _, err := db.Exec(`CREATE TABLE fixture (value INTEGER); INSERT INTO fixture VALUES (17)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	reader, err := openDatabaseCopy(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var value int
	if err := reader.QueryRowContext(ctx, `SELECT value FROM fixture`).Scan(&value); err != nil || value != 17 {
		t.Fatalf("read value = %d, err = %v", value, err)
	}
	if _, err := reader.ExecContext(ctx, `UPDATE fixture SET value = 18`); err == nil {
		t.Fatal("database copy accepted a write")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("database directory entries = %v", entries)
	}
}
