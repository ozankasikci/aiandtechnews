package media_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

// The expected statuses, bodies, stored names and files below were recorded
// by running Node's createApp (apps/server/src/app.ts) with the real
// createUpload (multer 1.4.5-lts.2, busboy 1.6.0) and fs operations on a
// temporary uploads directory and an in-memory database, fed through an
// in-process fake socket (no listener). Where Go deliberately differs the test
// says so.

var fixedUploadTime = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

type mediaFixture struct {
	handler http.Handler
	db      *sql.DB
	parent  string
	dir     string
}

func newMediaFixture(t *testing.T) mediaFixture {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, media.Migrations()); err != nil {
		t.Fatal(err)
	}
	parent, dir := uploadsDir(t)
	uploads := media.NewUploads(dir)
	return mediaFixture{handler: mediaRouter(media.NewSQLiteLibrary(db), uploads), db: db, parent: parent, dir: dir}
}

func mediaRouter(store interface {
	List(context.Context) ([]media.Item, error)
	Get(context.Context, string) (media.Item, error)
	Insert(context.Context, media.NewItem) (media.Item, error)
	Delete(context.Context, string) error
}, uploads *media.Uploads) http.Handler {
	library := media.NewLibrary(store, uploads, func() time.Time { return fixedUploadTime })
	router := chi.NewRouter()
	router.Route("/api/dashboard", func(dashboard chi.Router) {
		media.NewHandler(library, uploads, slog.New(slog.NewTextHandler(io.Discard, nil))).Mount(dashboard)
	})
	return router
}

type part struct {
	name, filename, contentType, content string
	rawDisposition                       string
	noFilename                           bool
}

// multipartBody writes parts byte for byte, so non-ASCII names and unusual
// dispositions reach the server exactly like the recorded Node requests.
func multipartBody(parts ...part) []byte {
	var body bytes.Buffer
	for _, p := range parts {
		body.WriteString("--B\r\n")
		switch {
		case p.rawDisposition != "":
			body.WriteString(p.rawDisposition)
		case p.noFilename:
			body.WriteString(`Content-Disposition: form-data; name="` + p.name + `"`)
		default:
			body.WriteString(`Content-Disposition: form-data; name="` + p.name + `"; filename="` + p.filename + `"`)
		}
		body.WriteString("\r\n")
		if p.contentType != "" {
			body.WriteString("Content-Type: " + p.contentType + "\r\n")
		}
		body.WriteString("\r\n" + p.content + "\r\n")
	}
	body.WriteString("--B--\r\n")
	return body.Bytes()
}

func (f mediaFixture) do(t *testing.T, method, target, contentType string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	return response
}

func (f mediaFixture) upload(t *testing.T, parts ...part) *httptest.ResponseRecorder {
	t.Helper()
	return f.do(t, http.MethodPost, "/api/dashboard/media/upload", "multipart/form-data; boundary=B", multipartBody(parts...))
}

func decodeMedia(t *testing.T, response *httptest.ResponseRecorder) media.Item {
	t.Helper()
	var envelope struct {
		Media media.Item `json:"media"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode %q: %v", response.Body.String(), err)
	}
	return envelope.Media
}

func assertJSON(t *testing.T, response *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if response.Code != status || response.Body.String() != body || response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("response = %d %q (%s), want %d %q", response.Code, response.Body.String(), response.Header().Get("Content-Type"), status, body)
	}
}

func TestUploadRecordsTheFileLikeNode(t *testing.T) {
	f := newMediaFixture(t)
	response := f.upload(t, part{name: "file", filename: "capture.png", contentType: "image/png", content: "synthetic image bytes"})
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	item := decodeMedia(t, response)
	if !regexp.MustCompile(`^/uploads/[0-9a-f]{32}\.png$`).MatchString(item.URL) {
		t.Fatalf("url = %q", item.URL)
	}
	want := media.Item{ID: 1, Filename: "capture.png", URL: item.URL, MIMEType: "image/png", Size: 21, UploadedAt: "2026-09-20 12:00:00"}
	if item != want {
		t.Fatalf("media = %+v, want %+v", item, want)
	}
	wantBody := `{"media":{"id":1,"filename":"capture.png","url":"` + item.URL + `","mime_type":"image/png","size":21,"uploaded_at":"2026-09-20 12:00:00"}}`
	assertJSON(t, response, http.StatusCreated, wantBody)
	content, err := os.ReadFile(filepath.Join(f.dir, strings.TrimPrefix(item.URL, "/uploads/")))
	if err != nil || string(content) != "synthetic image bytes" {
		t.Fatalf("stored file = %q, %v", content, err)
	}
}

func TestUploadNamesAndMIMETypesMatchNode(t *testing.T) {
	for _, tt := range []struct {
		name                   string
		part                   part
		wantFilename, wantMIME string
		wantExtension          string
	}{
		{"extension case is kept", part{filename: "A.Photo.JPEG", contentType: "image/jpeg"}, "A.Photo.JPEG", "image/jpeg", ".JPEG"},
		{".jpg is a JPEG", part{filename: "photo.jpg", contentType: "image/jpeg"}, "photo.jpg", "image/jpeg", ".jpg"},
		{"mixed-case .Png is a PNG", part{filename: "shot.Png", contentType: "image/png"}, "shot.Png", "image/png", ".Png"},
		{"GIF", part{filename: "anim.GIF", contentType: "image/gif"}, "anim.GIF", "image/gif", ".GIF"},
		{"path is stripped", part{filename: "../../x.png", contentType: "image/png"}, "x.png", "image/png", ".png"},
		{"backslash path is stripped", part{filename: `C:\dir\y.gif`, contentType: "image/gif"}, "y.gif", "image/gif", ".gif"},
		{"UTF-8 name is decoded as latin1", part{filename: "café.webp", contentType: "image/webp"}, "cafÃ©.webp", "image/webp", ".webp"},
		{"filename* is decoded", part{rawDisposition: `Content-Disposition: form-data; name="file"; filename*=UTF-8''caf%C3%A9.png`, contentType: "image/png"}, "café.png", "image/png", ".png"},
		{"MIME type is lowercased", part{filename: "u.png", contentType: "IMAGE/PNG"}, "u.png", "image/png", ".png"},
		{"MIME parameters are dropped", part{filename: "p.png", contentType: "image/png; charset=binary"}, "p.png", "image/png", ".png"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newMediaFixture(t)
			tt.part.name = "file"
			tt.part.content = "x"
			response := f.upload(t, tt.part)
			if response.Code != http.StatusCreated {
				t.Fatalf("status = %d %s", response.Code, response.Body.String())
			}
			item := decodeMedia(t, response)
			stored := strings.TrimPrefix(item.URL, "/uploads/")
			if item.Filename != tt.wantFilename || item.MIMEType != tt.wantMIME || !storedName.MatchString(stored) || stored[32:] != tt.wantExtension {
				t.Fatalf("media = %+v, want filename %q mime %q extension %q", item, tt.wantFilename, tt.wantMIME, tt.wantExtension)
			}
			if names := dirNames(t, f.dir); len(names) != 1 || names[0] != stored {
				t.Fatalf("uploads = %v, want [%s]", names, stored)
			}
		})
	}
}

func TestUploadIgnoresTextFieldsAndNamelessFiles(t *testing.T) {
	f := newMediaFixture(t)
	response := f.upload(t,
		part{name: "caption", noFilename: true, content: "hello"},
		part{name: "other", filename: "", contentType: "image/png", content: "skipped: no filename"},
		part{name: "file", filename: "f.png", contentType: "image/png", content: "x"},
		part{name: "after", noFilename: true, content: "ignored"},
	)
	if response.Code != http.StatusCreated || decodeMedia(t, response).Filename != "f.png" {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestUploadWithoutAFileAnswers400(t *testing.T) {
	for _, tt := range []struct {
		name, contentType string
		body              []byte
	}{
		{"empty filename", "multipart/form-data; boundary=B", multipartBody(part{name: "file", filename: "", contentType: "image/png", content: "x"})},
		{"text field only", "multipart/form-data; boundary=B", multipartBody(part{name: "file", noFilename: true, content: "not a file"})},
		{"no parts", "multipart/form-data; boundary=B", []byte("--B--\r\n")},
		{"JSON body", "application/json", []byte(`{}`)},
		{"no content type", "", nil},
		{"invalid content type", "multipart/", []byte("x")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newMediaFixture(t)
			assertJSON(t, f.do(t, http.MethodPost, "/api/dashboard/media/upload", tt.contentType, tt.body), http.StatusBadRequest, `{"error":"No file uploaded"}`)
			if names := dirNames(t, f.dir); len(names) != 0 {
				t.Fatalf("uploads = %v", names)
			}
		})
	}
}

// Node answers these with Express's default 500 HTML page whose first line is
// the multer error; Go answers 500 with that message as JSON.
func TestUploadRejectionsAnswer500WithMulterMessagesAndLeaveNoFiles(t *testing.T) {
	const invalidType = `{"error":"Only .jpg, .png, .webp, and .gif files are allowed"}`
	for _, tt := range []struct {
		name  string
		parts []part
		body  string
	}{
		{"text/plain", []part{{name: "file", filename: "t.txt", contentType: "text/plain", content: "x"}}, invalidType},
		{"svg", []part{{name: "file", filename: "s.svg", contentType: "image/svg+xml", content: "x"}}, invalidType},
		{"no part content type", []part{{name: "file", filename: "n.png", content: "x"}}, invalidType},
		{"octet-stream", []part{{name: "file", filename: "o.png", contentType: "application/octet-stream", content: "x"}}, invalidType},
		{"malformed part content type", []part{{name: "file", filename: "m.png", contentType: "image/png; =", content: "x"}}, invalidType},
		// Approved change (not Node parity): the original extension must be an
		// image extension that matches the declared type. Node stored these.
		{"html extension declared as png", []part{{name: "file", filename: "evil.html", contentType: "image/png", content: "<script>alert(1)</script>"}}, invalidType},
		{"no extension", []part{{name: "file", filename: "noext", contentType: "image/png", content: "x"}}, invalidType},
		{"dotfile name has no extension", []part{{name: "file", filename: ".png", contentType: "image/png", content: "x"}}, invalidType},
		{"trailing dot", []part{{name: "file", filename: "a.", contentType: "image/png", content: "x"}}, invalidType},
		{"svg extension declared as png", []part{{name: "file", filename: "s.svg", contentType: "image/png", content: "x"}}, invalidType},
		{"png extension declared as jpeg", []part{{name: "file", filename: "p.png", contentType: "image/jpeg", content: "x"}}, invalidType},
		{"jpeg extension declared as png", []part{{name: "file", filename: "p.jpeg", contentType: "image/png", content: "x"}}, invalidType},
		{"gif extension declared as webp", []part{{name: "file", filename: "p.gif", contentType: "image/webp", content: "x"}}, invalidType},
		{"non-ASCII extension via filename*", []part{{name: "file", rawDisposition: `Content-Disposition: form-data; name="file"; filename*=UTF-8''a.G%C4%B0F`, contentType: "image/gif", content: "x"}}, invalidType},
		{"double extension", []part{{name: "file", filename: "p.png.html", contentType: "image/png", content: "x"}}, invalidType},
		{"type is checked before the size", []part{{name: "file", filename: "big.html", contentType: "image/png", content: strings.Repeat("a", media.MaxUploadBytes)}}, invalidType},
		{"wrong field", []part{{name: "image", filename: "w.png", contentType: "image/png", content: "x"}}, `{"error":"Unexpected field"}`},
		{"wrong field is checked before the type", []part{{name: "image", filename: "w.txt", contentType: "text/plain", content: "x"}}, `{"error":"Unexpected field"}`},
		{"second file", []part{
			{name: "file", filename: "one.png", contentType: "image/png", content: "1"},
			{name: "file", filename: "two.png", contentType: "image/png", content: "2"},
		}, `{"error":"Unexpected field"}`},
		{"invalid file after a valid one", []part{
			{name: "file", filename: "one.png", contentType: "image/png", content: "1"},
			{name: "other", filename: "two.png", contentType: "image/png", content: "2"},
		}, `{"error":"Unexpected field"}`},
		{"exactly 5 MiB", []part{{name: "file", filename: "big.png", contentType: "image/png", content: strings.Repeat("a", media.MaxUploadBytes)}}, `{"error":"File too large"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newMediaFixture(t)
			assertJSON(t, f.upload(t, tt.parts...), http.StatusInternalServerError, tt.body)
			if names := dirNames(t, f.dir); len(names) != 0 {
				t.Fatalf("uploads = %v", names)
			}
			var rows int
			if err := f.db.QueryRow(`SELECT COUNT(*) FROM media`).Scan(&rows); err != nil || rows != 0 {
				t.Fatalf("rows = %d, %v", rows, err)
			}
		})
	}
}

func TestUploadAcceptsFiveMiBMinusOneByte(t *testing.T) {
	f := newMediaFixture(t)
	response := f.upload(t, part{name: "file", filename: "big.png", contentType: "image/png", content: strings.Repeat("a", media.MaxUploadBytes-1)})
	if response.Code != http.StatusCreated || decodeMedia(t, response).Size != media.MaxUploadBytes-1 {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

// Node answers 500 for these too ("Multipart: Boundary not found",
// "Unsupported content type: multipart/mixed", "Unexpected end of form"); Go's
// body is the generic internal error. Unlike Node, a truncated upload leaves
// no partial or earlier file behind.
func TestMalformedMultipartAnswers500AndLeavesNoFiles(t *testing.T) {
	for _, tt := range []struct {
		name, contentType, body string
	}{
		{"no boundary", "multipart/form-data", "x"},
		{"unsupported multipart type", "multipart/mixed; boundary=B", "--B--\r\n"},
		{"empty body", "multipart/form-data; boundary=B", ""},
		{"no boundary line", "multipart/form-data; boundary=B", "garbage without a boundary"},
		{"truncated file", "multipart/form-data; boundary=B", "--B\r\nContent-Disposition: form-data; name=\"file\"; filename=\"tr.png\"\r\nContent-Type: image/png\r\n\r\nabc"},
		{"truncated after a file", "multipart/form-data; boundary=B", "--B\r\nContent-Disposition: form-data; name=\"file\"; filename=\"tr.png\"\r\nContent-Type: image/png\r\n\r\nabc\r\n--B\r\nContent-Disposition: form-data; name=\"x\"\r\n\r\nunterminated"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newMediaFixture(t)
			assertJSON(t, f.do(t, http.MethodPost, "/api/dashboard/media/upload", tt.contentType, []byte(tt.body)), http.StatusInternalServerError, `{"error":"Internal server error"}`)
			if names := dirNames(t, f.dir); len(names) != 0 {
				t.Fatalf("uploads = %v", names)
			}
		})
	}
}

// Node rejects a text field of 1 MiB or more with 500 "Field value too long"
// and leaves the already stored file behind; Go discards text fields, caps the
// whole body at MaxUploadBytes + 1 MiB, and removes the file.
func TestUploadBodyIsCappedWhileTextFieldsAreDiscarded(t *testing.T) {
	f := newMediaFixture(t)
	big := strings.Repeat("t", media.MaxUploadBytes+(1<<20))
	response := f.upload(t, part{name: "file", filename: "f.png", contentType: "image/png", content: "x"}, part{name: "notes", noFilename: true, content: big})
	assertJSON(t, response, http.StatusInternalServerError, `{"error":"Internal server error"}`)
	if names := dirNames(t, f.dir); len(names) != 0 {
		t.Fatalf("uploads = %v", names)
	}
}

type failingInsertStore struct{ *media.SQLiteLibrary }

func (failingInsertStore) Insert(context.Context, media.NewItem) (media.Item, error) {
	return media.Item{}, errors.New("database is locked")
}

func TestUploadRemovesTheFileWhenTheRowCannotBeWritten(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, media.Migrations()); err != nil {
		t.Fatal(err)
	}
	_, dir := uploadsDir(t)
	uploads := media.NewUploads(dir)
	f := mediaFixture{handler: mediaRouter(failingInsertStore{media.NewSQLiteLibrary(db)}, uploads), db: db, dir: dir}
	assertJSON(t, f.upload(t, part{name: "file", filename: "a.png", contentType: "image/png", content: "x"}), http.StatusInternalServerError, `{"error":"Internal server error"}`)
	if names := dirNames(t, dir); len(names) != 0 {
		t.Fatalf("orphaned uploads = %v", names)
	}
}

func seedMedia(t *testing.T, db *sql.DB, rows ...string) {
	t.Helper()
	for _, row := range rows {
		if _, err := db.Exec(`INSERT INTO media (id, filename, url, mime_type, size, uploaded_at) VALUES ` + row); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListOrdersByUploadedAtTextDescending(t *testing.T) {
	f := newMediaFixture(t)
	assertJSON(t, f.do(t, http.MethodGet, "/api/dashboard/media", "", nil), http.StatusOK, `{"media":[]}`)
	seedMedia(t, f.db,
		`(401, 'synthetic-contract-image.png', '/uploads/synthetic-contract-image.png', 'image/png', 25, '2026-09-19T00:00:00.000Z')`,
		`(402, 'b.png', '/uploads/b.png', 'image/png', 1, '2026-09-20 12:00:00')`,
		`(403, 'c.gif', '/uploads/c.gif', 'image/gif', 2, '2026-09-19 23:59:59')`,
	)
	// Text ordering: "2026-09-20 ..." > "2026-09-19T..." > "2026-09-19 23..." ('T' sorts after ' ').
	assertJSON(t, f.do(t, http.MethodGet, "/api/dashboard/media", "", nil), http.StatusOK,
		`{"media":[{"id":402,"filename":"b.png","url":"/uploads/b.png","mime_type":"image/png","size":1,"uploaded_at":"2026-09-20 12:00:00"},`+
			`{"id":401,"filename":"synthetic-contract-image.png","url":"/uploads/synthetic-contract-image.png","mime_type":"image/png","size":25,"uploaded_at":"2026-09-19T00:00:00.000Z"},`+
			`{"id":403,"filename":"c.gif","url":"/uploads/c.gif","mime_type":"image/gif","size":2,"uploaded_at":"2026-09-19 23:59:59"}]}`)
}

func TestDeleteRemovesTheFileAndTheRow(t *testing.T) {
	f := newMediaFixture(t)
	item := decodeMedia(t, f.upload(t, part{name: "file", filename: "a.png", contentType: "image/png", content: "x"}))
	assertJSON(t, f.do(t, http.MethodDelete, "/api/dashboard/media/1", "", nil), http.StatusOK, `{"success":true}`)
	if names := dirNames(t, f.dir); len(names) != 0 {
		t.Fatalf("uploads = %v after deleting %s", names, item.URL)
	}
	assertJSON(t, f.do(t, http.MethodDelete, "/api/dashboard/media/1", "", nil), http.StatusNotFound, `{"error":"Media not found"}`)
}

func TestDeleteMatchesNodeForMissingFilesAndIDs(t *testing.T) {
	f := newMediaFixture(t)
	seedMedia(t, f.db,
		`(401, 'gone.png', '/uploads/gone.png', 'image/png', 1, '2026-09-19 00:00:00')`,
		`(402, 'spaced.png', '/uploads/spaced.png', 'image/png', 1, '2026-09-19 00:00:00')`,
	)
	// The file is already missing: Node checks existsSync and still deletes the row.
	assertJSON(t, f.do(t, http.MethodDelete, "/api/dashboard/media/401", "", nil), http.StatusOK, `{"success":true}`)
	for _, id := range []string{"9999", "abc", "401"} {
		assertJSON(t, f.do(t, http.MethodDelete, "/api/dashboard/media/"+id, "", nil), http.StatusNotFound, `{"error":"Media not found"}`)
	}
	// The id is bound as text, so SQLite's integer affinity matches " 402".
	assertJSON(t, f.do(t, http.MethodDelete, "/api/dashboard/media/%20402", "", nil), http.StatusOK, `{"success":true}`)
}

func TestDeleteNeverRemovesAnythingOutsideTheUploadsDirectory(t *testing.T) {
	f := newMediaFixture(t)
	writeUpload(t, filepath.Join(f.parent, "secret.txt"), "outside", modified)
	writeUpload(t, filepath.Join(f.dir, "secret.txt"), "inside", modified)
	if err := os.Symlink(filepath.Join(f.parent, "secret.txt"), filepath.Join(f.dir, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	seedMedia(t, f.db,
		`(900, 'x', '/uploads/../secret.txt', 'image/png', 1, '2026-09-19 00:00:00')`,
		`(903, 'x', 'https://cdn.example/a/link.txt', 'image/png', 1, '2026-09-19 00:00:00')`,
	)
	assertJSON(t, f.do(t, http.MethodDelete, "/api/dashboard/media/900", "", nil), http.StatusOK, `{"success":true}`)
	assertJSON(t, f.do(t, http.MethodDelete, "/api/dashboard/media/903", "", nil), http.StatusOK, `{"success":true}`)
	if names := dirNames(t, f.dir); len(names) != 0 {
		t.Fatalf("uploads = %v", names)
	}
	if content, err := os.ReadFile(filepath.Join(f.parent, "secret.txt")); err != nil || string(content) != "outside" {
		t.Fatalf("file outside uploads changed: %q, %v", content, err)
	}
}

// Node's unlinkSync fails with EPERM for a directory (including the uploads
// directory itself for an empty basename); the handler throws before the row
// DELETE, so Express answers 500 and the row stays.
func TestDeleteKeepsTheRowWhenTheFileCannotBeRemoved(t *testing.T) {
	f := newMediaFixture(t)
	if err := os.Mkdir(filepath.Join(f.dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	seedMedia(t, f.db,
		`(901, 'x', '/uploads/sub', 'image/png', 1, '2026-09-19 00:00:00')`,
		`(902, 'x', '', 'image/png', 1, '2026-09-19 00:00:00')`,
		`(904, 'x', '/uploads/..', 'image/png', 1, '2026-09-19 00:00:00')`,
	)
	for _, id := range []string{"901", "902", "904"} {
		assertJSON(t, f.do(t, http.MethodDelete, "/api/dashboard/media/"+id, "", nil), http.StatusInternalServerError, `{"error":"Internal server error"}`)
		var rows int
		if err := f.db.QueryRow(`SELECT COUNT(*) FROM media WHERE id = ?`, id).Scan(&rows); err != nil || rows != 1 {
			t.Fatalf("row %s count = %d, %v", id, rows, err)
		}
	}
	if _, err := os.Stat(filepath.Join(f.dir, "sub")); err != nil {
		t.Fatalf("directory removed: %v", err)
	}
}

// The server's ReadTimeout (15s in production) would cut off a slow 5 MiB
// upload; the upload handler alone extends its read deadline. The server here
// uses a 300ms ReadTimeout and the client sends the body over ~1.2s.
func TestUploadOutlivesTheServerReadTimeoutWhileOtherRoutesDoNot(t *testing.T) {
	if testing.Short() {
		t.Skip("slow-body test")
	}
	f := newMediaFixture(t)
	mux := http.NewServeMux()
	mux.Handle("/api/", f.handler)
	mux.HandleFunc("/control", func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusRequestTimeout)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewUnstartedServer(mux)
	server.Config.ReadTimeout = 300 * time.Millisecond
	server.Config.WriteTimeout = 300 * time.Millisecond
	server.Start()
	t.Cleanup(server.Close)

	slowPost := func(target string, body []byte) (int, string, error) {
		reader, writer := io.Pipe()
		go func() {
			third := len(body) / 3
			for _, chunk := range [][]byte{body[:third], body[third : 2*third], body[2*third:]} {
				time.Sleep(400 * time.Millisecond)
				if _, err := writer.Write(chunk); err != nil {
					_ = writer.CloseWithError(err)
					return
				}
			}
			_ = writer.Close()
		}()
		request, err := http.NewRequest(http.MethodPost, server.URL+target, reader)
		if err != nil {
			return 0, "", err
		}
		request.Header.Set("Content-Type", "multipart/form-data; boundary=B")
		response, err := server.Client().Do(request)
		if err != nil {
			return 0, "", err
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		return response.StatusCode, string(data), err
	}

	body := multipartBody(part{name: "file", filename: "slow.png", contentType: "image/png", content: strings.Repeat("s", 3000)})
	status, text, err := slowPost("/api/dashboard/media/upload", body)
	if err != nil || status != http.StatusCreated {
		t.Fatalf("slow upload = %d %q, %v", status, text, err)
	}
	if status, _, err := slowPost("/control", body); err == nil && status == http.StatusOK {
		t.Fatal("control route read a slow body past the server ReadTimeout; the test cannot prove the extension")
	}
}
