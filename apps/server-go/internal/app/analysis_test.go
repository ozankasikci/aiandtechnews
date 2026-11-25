package app

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func TestAnalysisWorkerWiredOnlyWhenEnabled(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		db, _ := testutil.OpenDatabase(t)
		cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), JWTSecret: "synthetic-test-secret", UploadsDir: t.TempDir(), GeminiAPIKey: "synthetic-key", AnalysisEnabled: enabled, AnalysisInterval: 10 * time.Minute}
		application, err := NewWithDatabase(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db)
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if enabled {
			want = 1
		}
		if len(application.background) != want {
			t.Fatalf("enabled=%t background=%d want=%d", enabled, len(application.background), want)
		}
	}
}
