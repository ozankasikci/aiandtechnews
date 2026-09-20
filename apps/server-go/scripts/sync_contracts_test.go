package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSyncAcceptWritesMirrorMode0644(t *testing.T) {
	directory := t.TempDir()
	canonical := filepath.Join(directory, "canonical.json")
	mirror := filepath.Join(directory, "nested", "mirror.json")
	if err := os.WriteFile(canonical, []byte("reviewed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("./sync-contracts.sh", "--accept")
	command.Env = append(os.Environ(), "CONTRACT_CANONICAL_PATH="+canonical, "CONTRACT_MIRROR_PATH="+mirror)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("sync --accept: %v\n%s", err, output)
	}
	info, err := os.Stat(mirror)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("mirror mode = %#o, want 0644", got)
	}
}
