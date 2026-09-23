package media_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
)

// uploadsDir returns an empty uploads directory inside a temporary parent, so
// tests can also place a file next to (outside) it.
func uploadsDir(t *testing.T) (parent, dir string) {
	t.Helper()
	parent = t.TempDir()
	dir = filepath.Join(parent, "uploads")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return parent, dir
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

var storedName = regexp.MustCompile(`^[0-9a-f]{32}`)

func TestSaveNamesFilesLikeMulterDiskStorage(t *testing.T) {
	_, dir := uploadsDir(t)
	uploads := media.NewUploads(dir)
	for original, extension := range map[string]string{
		"capture.png":  ".png",
		"noext":        "",
		"A.Photo.JPEG": ".JPEG",
		"evil.html":    ".html",
		".png":         "",
		"a.":           ".",
		"cafÃ©.webp":   ".webp",
	} {
		file, err := uploads.Save(strings.NewReader("synthetic image bytes"), original)
		if err != nil {
			t.Fatalf("Save(%q) error = %v", original, err)
		}
		if !storedName.MatchString(file.Name) || strings.TrimPrefix(file.Name, file.Name[:32]) != extension {
			t.Errorf("Save(%q) name = %q, want 32 hex digits + %q", original, file.Name, extension)
		}
		if file.URL() != "/uploads/"+file.Name || file.Size != 21 {
			t.Errorf("Save(%q) = %+v", original, file)
		}
		content, err := os.ReadFile(filepath.Join(dir, file.Name))
		if err != nil || string(content) != "synthetic image bytes" {
			t.Errorf("stored content = %q, %v", content, err)
		}
	}
}

func TestSaveUsesTheInjectedRandomSourceForNames(t *testing.T) {
	_, dir := uploadsDir(t)
	uploads := media.NewUploads(dir)
	uploads.SetRandomForTest(bytes.NewReader(bytes.Repeat([]byte{0xab}, 16)))
	file, err := uploads.Save(strings.NewReader("x"), "a.png")
	if err != nil {
		t.Fatal(err)
	}
	if file.Name != strings.Repeat("ab", 16)+".png" {
		t.Fatalf("name = %q", file.Name)
	}
}

func TestSaveFailsWithoutRandomBytesAndLeavesNothing(t *testing.T) {
	_, dir := uploadsDir(t)
	uploads := media.NewUploads(dir)
	uploads.SetRandomForTest(iotest.ErrReader(errors.New("entropy unavailable")))
	if _, err := uploads.Save(strings.NewReader("x"), "a.png"); err == nil || !strings.Contains(err.Error(), "entropy unavailable") {
		t.Fatalf("Save() error = %v", err)
	}
	if names := dirNames(t, dir); len(names) != 0 {
		t.Fatalf("files left = %v", names)
	}
}

func TestSaveNeverOverwritesAnExistingFile(t *testing.T) {
	_, dir := uploadsDir(t)
	uploads := media.NewUploads(dir)
	name := strings.Repeat("00", 16) + ".png"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	uploads.SetRandomForTest(bytes.NewReader(make([]byte, 16)))
	if _, err := uploads.Save(strings.NewReader("replacement"), "a.png"); err == nil {
		t.Fatal("Save() overwrote an existing file")
	}
	content, _ := os.ReadFile(filepath.Join(dir, name))
	if string(content) != "original" {
		t.Fatalf("existing file = %q", content)
	}
}

func TestSaveNamesAreUnique(t *testing.T) {
	_, dir := uploadsDir(t)
	uploads := media.NewUploads(dir)
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		file, err := uploads.Save(strings.NewReader("x"), "a.png")
		if err != nil {
			t.Fatal(err)
		}
		if seen[file.Name] {
			t.Fatalf("duplicate name %q", file.Name)
		}
		seen[file.Name] = true
	}
}

func TestSaveRejectsFilesThatReachFiveMiB(t *testing.T) {
	_, dir := uploadsDir(t)
	uploads := media.NewUploads(dir)
	largest, err := uploads.Save(bytes.NewReader(make([]byte, media.MaxUploadBytes-1)), "big.png")
	if err != nil || largest.Size != media.MaxUploadBytes-1 {
		t.Fatalf("5 MiB - 1 byte: %+v, %v", largest, err)
	}
	for _, size := range []int{media.MaxUploadBytes, media.MaxUploadBytes + 1} {
		if _, err := uploads.Save(bytes.NewReader(make([]byte, size)), "huge.png"); !errors.Is(err, media.ErrFileTooLarge) {
			t.Fatalf("size %d error = %v, want ErrFileTooLarge", size, err)
		}
	}
	if names := dirNames(t, dir); len(names) != 1 || names[0] != largest.Name {
		t.Fatalf("files left = %v, want only %s", names, largest.Name)
	}
}

func TestSaveRemovesThePartialFileWhenTheStreamFails(t *testing.T) {
	_, dir := uploadsDir(t)
	uploads := media.NewUploads(dir)
	broken := io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(io.ErrUnexpectedEOF))
	if _, err := uploads.Save(broken, "a.png"); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Save() error = %v", err)
	}
	if names := dirNames(t, dir); len(names) != 0 {
		t.Fatalf("files left = %v", names)
	}
}

func TestSaveFailsWhenTheUploadsDirectoryIsMissing(t *testing.T) {
	uploads := media.NewUploads(filepath.Join(t.TempDir(), "missing"))
	if _, err := uploads.Save(strings.NewReader("x"), "a.png"); err == nil {
		t.Fatal("Save() error = nil")
	}
}

func TestSaveRejectsANULInTheExtension(t *testing.T) {
	_, dir := uploadsDir(t)
	if _, err := media.NewUploads(dir).Save(strings.NewReader("x"), "a.p\x00g"); err == nil {
		t.Fatal("Save() error = nil")
	}
}

func TestRemoveDeletesTheURLBasenameInsideTheUploadsDirectory(t *testing.T) {
	parent, dir := uploadsDir(t)
	uploads := media.NewUploads(dir)
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "kept.png"), "kept")
	write(filepath.Join(dir, "gone.png"), "gone")
	write(filepath.Join(dir, "secret.txt"), "inside")
	write(filepath.Join(parent, "secret.txt"), "outside")

	if err := uploads.Remove("/uploads/gone.png"); err != nil {
		t.Fatal(err)
	}
	// path.basename confines "/uploads/../secret.txt" to <uploads>/secret.txt.
	if err := uploads.Remove("/uploads/../secret.txt"); err != nil {
		t.Fatal(err)
	}
	if names := dirNames(t, dir); len(names) != 1 || names[0] != "kept.png" {
		t.Fatalf("uploads = %v", names)
	}
	if content, err := os.ReadFile(filepath.Join(parent, "secret.txt")); err != nil || string(content) != "outside" {
		t.Fatalf("file outside the uploads directory changed: %q, %v", content, err)
	}
}

func TestRemoveTreatsAMissingFileAsDone(t *testing.T) {
	_, dir := uploadsDir(t)
	if err := media.NewUploads(dir).Remove("/uploads/missing.png"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
}

func TestRemoveRefusesDirectoriesAndTheUploadsDirectoryItself(t *testing.T) {
	parent, dir := uploadsDir(t)
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	uploads := media.NewUploads(dir)
	for _, url := range []string{"/uploads/sub", "/uploads/sub/", "", "/", "/uploads/.", "/uploads/.."} {
		if err := uploads.Remove(url); err == nil {
			t.Errorf("Remove(%q) error = nil", url)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "sub")); err != nil {
		t.Fatalf("directory removed: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("uploads directory removed: %v", err)
	}
	if _, err := os.Stat(parent); err != nil {
		t.Fatalf("parent removed: %v", err)
	}
}

func TestRemoveDeletesASymlinkButNeverItsTarget(t *testing.T) {
	parent, dir := uploadsDir(t)
	target := filepath.Join(parent, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := media.NewUploads(dir).Remove("https://cdn.example/a/link.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "link.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink still present: %v", err)
	}
	if content, err := os.ReadFile(target); err != nil || string(content) != "secret" {
		t.Fatalf("symlink target changed: %q, %v", content, err)
	}
}
