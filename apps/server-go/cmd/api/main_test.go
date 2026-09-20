package main

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestContextCanceledOnSignalRestoresDefaultHandlingFirst(t *testing.T) {
	notifications := make(chan os.Signal, 1)
	restored := make(chan struct{})
	ctx, stop := contextCanceledOnSignal(context.Background(), notifications, func() {
		close(restored)
	})
	defer stop()

	notifications <- syscall.SIGTERM
	select {
	case <-ctx.Done():
		select {
		case <-restored:
		default:
			t.Fatal("context canceled before default signal handling was restored")
		}
	case <-time.After(time.Second):
		t.Fatal("context was not canceled after signal")
	}
}

func TestContextCanceledOnSignalStopCleansUpWithoutSignal(t *testing.T) {
	notifications := make(chan os.Signal)
	restored := false
	ctx, stop := contextCanceledOnSignal(context.Background(), notifications, func() {
		restored = true
	})
	stop()
	if !restored {
		t.Fatal("stop did not restore signal handling")
	}
	if ctx.Err() != context.Canceled {
		t.Fatalf("context error = %v, want context canceled", ctx.Err())
	}
}

func TestFindWorktreeRootFromRepositoryAndModuleDirectories(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, "apps", "server-go")
	if err := os.MkdirAll(filepath.Join(module, "cmd", "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pnpm-workspace.yaml"), []byte("packages: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module github.com/ozankasikci/aiandtechnews/apps/server-go\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, start := range []string{root, module, filepath.Join(module, "cmd", "api")} {
		got, err := findWorktreeRoot(start)
		if err != nil {
			t.Fatalf("findWorktreeRoot(%q) error = %v", start, err)
		}
		if got != root {
			t.Errorf("findWorktreeRoot(%q) = %q, want %q", start, got, root)
		}
	}
}

func TestFindWorktreeRootRejectsUnrelatedDirectory(t *testing.T) {
	if _, err := findWorktreeRoot(t.TempDir()); err == nil {
		t.Fatal("findWorktreeRoot() error = nil")
	}
}
