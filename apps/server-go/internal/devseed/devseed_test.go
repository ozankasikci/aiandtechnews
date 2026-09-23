package devseed_test

import (
	"context"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/devseed"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

var now = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

func TestSeedCreatesLoginAndReviewableCandidates(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	ctx := context.Background()
	if err := migrate.Run(ctx, db, app.Migrations()); err != nil {
		t.Fatal(err)
	}

	summary, err := devseed.Seed(ctx, db, now, "dev-password")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Email != devseed.Email || summary.Inserted != len(devseed.Candidates) {
		t.Fatalf("summary = %+v", summary)
	}

	var hash, role string
	if err := db.QueryRow(`SELECT password_hash, role FROM authors WHERE email = ?`, devseed.Email).Scan(&hash, &role); err != nil {
		t.Fatal(err)
	}
	if role != "admin" || bcrypt.CompareHashAndPassword([]byte(hash), []byte("dev-password")) != nil {
		t.Fatalf("author role=%q, password mismatch", role)
	}

	service, err := newsroom.NewService(newsroom.NewSQLiteStore(db), func() time.Time { return now }, newsroom.RandomMinutes)
	if err != nil {
		t.Fatal(err)
	}
	overview, err := service.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Pending != int64(len(devseed.Candidates)-2) || overview.Failed != 1 || overview.PublishedToday != 1 {
		t.Fatalf("overview = %+v", overview)
	}
}

func TestSeedIsIdempotentAndResetsPassword(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	ctx := context.Background()
	if err := migrate.Run(ctx, db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	if _, err := devseed.Seed(ctx, db, now, "first"); err != nil {
		t.Fatal(err)
	}
	summary, err := devseed.Seed(ctx, db, now, "second")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Inserted != 0 {
		t.Fatalf("second run inserted %d", summary.Inserted)
	}
	var hash string
	if err := db.QueryRow(`SELECT password_hash FROM authors WHERE email = ?`, devseed.Email).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("second")) != nil {
		t.Fatal("password was not reset on re-seed")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM candidates`).Scan(&count); err != nil || count != len(devseed.Candidates) {
		t.Fatalf("candidates = %d err=%v", count, err)
	}
}
