package contracttest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestContractMirrorAndSyncDriftCheck(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	serverGo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	canonical := filepath.Clean(filepath.Join(serverGo, "..", "server", "contracts", "node", "contracts.json"))
	mirror := filepath.Join(serverGo, "contracts", "fixtures", "node-contracts.json")
	want, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(mirror)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("generated mirror differs from canonical fixture")
	}
	script := filepath.Join(serverGo, "scripts", "sync-contracts.sh")
	cmd := exec.Command(script, "--check")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sync check: %v\n%s", err, output)
	}

	temporary := t.TempDir()
	testCanonical := filepath.Join(temporary, "canonical.json")
	testMirror := filepath.Join(temporary, "nested", "mirror.json")
	if err := os.WriteFile(testCanonical, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(testMirror), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testMirror, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(script, "--accept")
	cmd.Env = append(os.Environ(), "CONTRACT_CANONICAL_PATH="+testCanonical, "CONTRACT_MIRROR_PATH="+testMirror)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sync accept: %v\n%s", err, output)
	}
	cmd = exec.Command(script, "--check")
	cmd.Env = append(os.Environ(), "CONTRACT_CANONICAL_PATH="+testCanonical, "CONTRACT_MIRROR_PATH="+testMirror)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("accepted sync check: %v\n%s", err, output)
	}
}
