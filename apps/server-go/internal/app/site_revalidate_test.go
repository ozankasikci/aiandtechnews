package app_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/editorial"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

// A dashboard change to a published article asks the site to refresh its
// cached pages, authorized with the site's CRON_SECRET.
func TestDashboardArticleChangesRevalidateTheSite(t *testing.T) {
	var mu sync.Mutex
	var auths []string
	var slugs [][]string
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Slugs []string `json:"slugs"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		auths, slugs = append(auths, r.Header.Get("Authorization")), append(slugs, body.Slugs)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer site.Close()

	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	seedContractArticles(t, db)
	fixed := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db"),
		JWTSecret: authTestSecret, UploadsDir: t.TempDir(), SiteRevalidateURL: site.URL + "/api/revalidate", NewsletterCronSecret: "cron-secret"}
	application, err := app.NewWithDatabaseAt(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db, func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := editorial.NewJWT(authTestSecret, func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.Sign(editorial.Identity{ID: 201, Email: "editorial@example.invalid", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, application.Handler(), http.MethodDelete, "/api/dashboard/articles/301", "", "Bearer "+token)
	if response.Code != http.StatusOK {
		t.Fatalf("delete = %d %s", response.Code, response.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(slugs)
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(slugs) != 1 || len(slugs[0]) != 1 || slugs[0][0] != "synthetic-published-newer" || auths[0] != "Bearer cron-secret" {
		t.Fatalf("site revalidations = %v (auth %v)", slugs, auths)
	}
}
