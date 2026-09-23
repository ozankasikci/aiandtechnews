package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
)

// MaxUploadBytes is multer's limits.fileSize (apps/server/src/upload.ts). busboy
// raises the limit when a file *reaches* it, so the largest accepted upload is
// MaxUploadBytes-1 bytes and a file of exactly 5 MiB is rejected, as in Node.
const MaxUploadBytes = 5 << 20

// ErrFileTooLarge reports an upload that reached MaxUploadBytes.
var ErrFileTooLarge = errors.New("File too large")

// errUnsafeName reports a stored name that would leave the uploads directory
// or address the directory itself.
var errUnsafeName = errors.New("unsafe upload name")

// Uploads is the local uploads directory: Node's uploadRoot, served at
// /uploads/*. Every file operation goes through an os.Root opened on the
// directory, so no name (including "..", absolute paths, and symlinks that
// point elsewhere) can reach a file outside it.
type Uploads struct {
	dir    string
	random io.Reader
}

// NewUploads uses dir as the uploads directory. It does not touch the
// filesystem; the directory is opened per operation, so a missing directory
// fails requests rather than composition. cmd/api creates it at startup, like
// Node's index.ts.
func NewUploads(dir string) *Uploads {
	return &Uploads{dir: dir, random: rand.Reader}
}

// Dir returns the uploads directory.
func (u *Uploads) Dir() string { return u.dir }

// StoredFile is one saved upload.
type StoredFile struct {
	Name string // the generated file name, also the last segment of the URL
	Size int64
	URL  string // what media.url records: /uploads/<name> locally, an absolute URL on S3
}

// newUploadName is multer's diskStorage filename callback: 16 crypto/rand
// bytes as hex followed by path.extname(originalName). Every backend uses it.
func newUploadName(random io.Reader, originalName string) (string, error) {
	var token [16]byte
	if _, err := io.ReadFull(random, token[:]); err != nil {
		return "", fmt.Errorf("generate upload name: %w", err)
	}
	name := hex.EncodeToString(token[:]) + nodeExtname(originalName)
	if err := checkStoredName(name); err != nil {
		return "", err
	}
	return name, nil
}

// Save streams content into a new file under newUploadName. Content that
// reaches MaxUploadBytes is rejected with ErrFileTooLarge; on any error the
// partial file is removed. The content type is implied by the extension when
// the file is served, so it is not stored.
func (u *Uploads) Save(_ context.Context, content io.Reader, originalName, _ string) (StoredFile, error) {
	name, err := newUploadName(u.random, originalName)
	if err != nil {
		return StoredFile{}, err
	}
	root, err := os.OpenRoot(u.dir)
	if err != nil {
		return StoredFile{}, fmt.Errorf("open uploads directory: %w", err)
	}
	defer root.Close()
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return StoredFile{}, fmt.Errorf("create upload: %w", err)
	}
	size, copyErr := io.Copy(file, io.LimitReader(content, MaxUploadBytes))
	if copyErr == nil && size == MaxUploadBytes {
		copyErr = ErrFileTooLarge
	}
	closeErr := file.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		_ = root.Remove(name)
		return StoredFile{}, err
	}
	return StoredFile{Name: name, Size: size, URL: "/uploads/" + name}, nil
}

// Remove deletes the file Node's delete handler would address,
// path.join(uploadRoot, path.basename(url)). A missing file is not an error
// (Node checks existsSync first). A directory, the uploads directory itself
// (an empty, "." or ".." basename), or a path outside the directory is an
// error, as Node's unlinkSync fails for them too; nothing is removed then. A
// symlink is removed itself, never its target.
func (u *Uploads) Remove(_ context.Context, url string) error {
	name := nodeBasename(url)
	if err := checkStoredName(name); err != nil {
		return err
	}
	root, err := os.OpenRoot(u.dir)
	if err != nil {
		return fmt.Errorf("open uploads directory: %w", err)
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat upload: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("remove upload %q: is a directory", name)
	}
	if err := root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove upload: %w", err)
	}
	return nil
}

// checkStoredName accepts a single, non-empty path segment only.
func checkStoredName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") {
		return fmt.Errorf("%w: %q", errUnsafeName, name)
	}
	return nil
}
