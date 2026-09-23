package media

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
)

// MountStatic registers GET and HEAD /uploads/* on the root router, the Go
// counterpart of app.use("/uploads", express.static(uploadRoot)) (app.ts:30).
func (u *Uploads) MountStatic(router chi.Router) {
	router.Get("/uploads/*", u.serve)
	router.Head("/uploads/*", u.serve)
}

// serve reproduces serve-static 1.16 / send 0.19 defaults for files: the same
// Content-Type table, "Cache-Control: public, max-age=0", a weak ETag of
// W/"<size hex>-<mtime ms hex>", Last-Modified, byte ranges, and conditional
// 304s. Missing files, directories, dotfiles (any segment starting with "."),
// and anything outside the uploads directory answer 404. Every response also
// carries X-Content-Type-Options: nosniff (an approved change).
func (u *Uploads) serve(w http.ResponseWriter, r *http.Request) {
	// Approved change (not Node parity): no response from the uploads
	// directory may be content-sniffed, including legacy files Node accepted
	// with any extension.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	name, ok := staticName(r.URL.Path)
	if !ok {
		notFound(w)
		return
	}
	root, err := os.OpenRoot(u.dir)
	if err != nil {
		notFound(w)
		return
	}
	defer root.Close()
	file, err := root.Open(name)
	if err != nil {
		notFound(w)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		notFound(w)
		return
	}
	header := w.Header()
	header.Set("Cache-Control", "public, max-age=0")
	header.Set("ETag", fmt.Sprintf(`W/"%x-%x"`, info.Size(), info.ModTime().UnixMilli()))
	header.Set("Content-Type", staticContentType(name))
	http.ServeContent(w, r, "", info.ModTime(), file)
}

// staticName maps the decoded request path to a slash-separated name inside
// the uploads directory, rejecting empty segments (directories), segments that
// start with "." (send's dotfiles: "ignore", which also covers "." and ".."),
// and NUL bytes.
func staticName(requestPath string) (string, bool) {
	name, ok := strings.CutPrefix(requestPath, "/uploads/")
	if !ok || name == "" || strings.ContainsRune(name, 0) {
		return "", false
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || strings.HasPrefix(segment, ".") {
			return "", false
		}
	}
	return name, true
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		body = []byte(`{"error":"Internal server error"}`)
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
