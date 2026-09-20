package app_test

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/contracttest"
)

func TestHealthMatchesApprovedNodeContract(t *testing.T) {
	contract, err := contracttest.Load(filepath.Join("..", "..", "contracts", "fixtures", "node-contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	op, ok := contract.Operation("health.get")
	if !ok {
		t.Fatal("health.get missing")
	}
	application, err := app.New(config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db")}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := contracttest.Replay(application.Handler(), op); err != nil {
		t.Fatalf("health replay: %v", err)
	}
}
