package app_test

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/contracttest"
)

// dashboardMediaOperations is every media operation in the canonical Node
// fixture, in canonical order.
var dashboardMediaOperations = []string{"dashboard.media.list", "dashboard.media.upload", "dashboard.media.delete"}

// seedContractMedia reproduces the synthetic Node capture's media row and
// file (apps/server/scripts/contracts/synthetic-seed.ts:11-12, 49-50).
func seedContractMedia(t *testing.T, db *sql.DB, uploads string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(uploads, "synthetic-contract-image.png"), []byte("synthetic contract media\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO media (id, filename, url, mime_type, size, uploaded_at) VALUES
(401, 'synthetic-contract-image.png', '/uploads/synthetic-contract-image.png', 'image/png', 25, '2026-09-19T00:00:00.000Z')`); err != nil {
		t.Fatal(err)
	}
}

func mediaApplication(t *testing.T) (http.Handler, *sql.DB, string) {
	t.Helper()
	handler, db, uploads := dashboardApplicationWithUploads(t)
	seedContractMedia(t, db, uploads)
	return handler, db, uploads
}

// resolveEmbedded substitutes a placeholder that appears inside a larger
// string (the multipart boundary, "/uploads/$UPLOAD_FILENAME"), which
// resolveAuthOperation's whole-string replacement does not cover.
func resolveEmbedded(t *testing.T, operation contracttest.Operation, placeholder, value string) contracttest.Operation {
	t.Helper()
	data, err := json.Marshal(operation)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, []byte(placeholder), encoded[1:len(encoded)-1])
	var resolved contracttest.Operation
	if err := json.Unmarshal(data, &resolved); err != nil {
		t.Fatal(err)
	}
	return resolved
}

func binding(t *testing.T, contract *contracttest.Contract, placeholder string) contracttest.Binding {
	t.Helper()
	for _, candidate := range contract.Replay.Bindings {
		if candidate.Placeholder == placeholder {
			return candidate
		}
	}
	t.Fatalf("binding %s missing", placeholder)
	return contracttest.Binding{}
}

var uploadFilename = regexp.MustCompile(`^[0-9a-f]{32}\.png$`)

func TestDashboardMediaMatchesApprovedNodeContractSequence(t *testing.T) {
	handler, db, uploads := mediaApplication(t)
	contract, err := contracttest.Load(filepath.Join("..", "..", "contracts", "fixtures", "node-contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixtureOperations []string
	for _, operation := range contract.Operations {
		if strings.HasPrefix(operation.OperationID, "dashboard.media.") {
			fixtureOperations = append(fixtureOperations, operation.OperationID)
		}
	}
	if !reflect.DeepEqual(fixtureOperations, dashboardMediaOperations) {
		t.Fatalf("fixture media operations = %v, want %v", fixtureOperations, dashboardMediaOperations)
	}

	bindings := contractBindings(t, handler, contract)
	previous := assertAfter(t, contract, "auth.login", -1)
	resolve := func(id string) contracttest.Operation {
		t.Helper()
		previous = assertAfter(t, contract, id, previous)
		op, _ := contract.Operation(id)
		resolved, err := resolveAuthOperation(op, bindings)
		if err != nil {
			t.Fatalf("resolve %s: %v", id, err)
		}
		return resolved
	}

	if err := contracttest.Replay(handler, resolve("dashboard.media.list")); err != nil {
		t.Fatalf("dashboard.media.list replay: %v", err)
	}

	boundary := binding(t, contract, "$MULTIPART_BOUNDARY")
	if boundary.Resolver.Type != "literal" || boundary.Resolver.Value == "" {
		t.Fatalf("$MULTIPART_BOUNDARY resolver = %+v", boundary.Resolver)
	}
	upload := resolveEmbedded(t, resolve("dashboard.media.upload"), "$MULTIPART_BOUNDARY", boundary.Resolver.Value)
	response, err := contracttest.Execute(handler, upload)
	if err != nil {
		t.Fatal(err)
	}
	filenameBinding := binding(t, contract, "$UPLOAD_FILENAME")
	want := contracttest.Resolver{Type: "responseJsonPointerTransform", OperationID: "dashboard.media.upload", Pointer: "/media/url", Transform: "basename"}
	if filenameBinding.Resolver != want {
		t.Fatalf("$UPLOAD_FILENAME resolver = %+v, want %+v", filenameBinding.Resolver, want)
	}
	var uploaded struct {
		Media struct {
			ID  int64  `json:"id"`
			URL string `json:"url"`
		} `json:"media"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("upload response %s: %v", response.Body.String(), err)
	}
	filename := path.Base(uploaded.Media.URL)
	if !uploadFilename.MatchString(filename) {
		t.Fatalf("$UPLOAD_FILENAME = %q, want 32 hex digits + .png", filename)
	}
	if err := contracttest.Verify(resolveEmbedded(t, upload, "$UPLOAD_FILENAME", filename), response); err != nil {
		t.Fatalf("dashboard.media.upload replay: %v", err)
	}
	content, err := base64.StdEncoding.DecodeString(upload.Request.Multipart.ContentBase64)
	if err != nil {
		t.Fatal(err)
	}
	if stored, err := os.ReadFile(filepath.Join(uploads, filename)); err != nil || !bytes.Equal(stored, content) {
		t.Fatalf("stored upload = %q, %v", stored, err)
	}

	remove := resolve("dashboard.media.delete")
	if len(remove.Dependencies) != 1 || remove.Dependencies[0] != (contracttest.Dependency{OperationID: "dashboard.media.upload", ResponsePointer: "/media/id", RequestTarget: "/pathParameters/id"}) {
		t.Fatalf("delete dependencies = %+v", remove.Dependencies)
	}
	if got := string(remove.Request.PathParameters["id"]); got != fmt.Sprint(uploaded.Media.ID) || remove.Request.ActualPath != fmt.Sprintf("/api/dashboard/media/%d", uploaded.Media.ID) {
		t.Fatalf("delete targets id %s (%s), upload created %d", got, remove.Request.ActualPath, uploaded.Media.ID)
	}
	if err := contracttest.Replay(handler, remove); err != nil {
		t.Fatalf("dashboard.media.delete replay: %v", err)
	}
	if _, err := os.Stat(filepath.Join(uploads, filename)); !os.IsNotExist(err) {
		t.Fatalf("deleted upload still on disk: %v", err)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM media`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("media rows after delete = %d, %v", rows, err)
	}
}

func TestDashboardMediaRoutesRequireAuthentication(t *testing.T) {
	handler, db, uploads := mediaApplication(t)
	for _, route := range []struct{ method, target, contentType, body string }{
		{http.MethodGet, "/api/dashboard/media", "", ""},
		{http.MethodPost, "/api/dashboard/media/upload", "multipart/form-data; boundary=B", "--B\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.png\"\r\nContent-Type: image/png\r\n\r\nx\r\n--B--\r\n"},
		{http.MethodDelete, "/api/dashboard/media/401", "", ""},
	} {
		request := httptest.NewRequest(route.method, route.target, strings.NewReader(route.body))
		if route.contentType != "" {
			request.Header.Set("Content-Type", route.contentType)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized || response.Body.String() != `{"error":"Authentication required"}` {
			t.Errorf("%s %s = %d %s", route.method, route.target, response.Code, response.Body.String())
		}
	}
	entries, err := os.ReadDir(uploads)
	if err != nil || len(entries) != 1 {
		t.Fatalf("uploads after unauthenticated requests = %v, %v", entries, err)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM media`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("media rows = %d, %v", rows, err)
	}
}

// Existing articles and authors reference /uploads/<file> URLs from Node's
// media library; after cutover the copied directory must keep serving them.
func TestExistingUploadURLsKeepServingThroughTheApplication(t *testing.T) {
	handler, db, _ := mediaApplication(t)
	rows, err := db.Query(`SELECT featured_image FROM articles WHERE featured_image LIKE '/uploads/%'
UNION SELECT avatar FROM authors WHERE avatar LIKE '/uploads/%'
UNION SELECT url FROM media WHERE url LIKE '/uploads/%'`)
	if err != nil {
		t.Fatal(err)
	}
	var urls []string
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			t.Fatal(err)
		}
		urls = append(urls, url)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(urls) == 0 {
		t.Fatal("no /uploads URLs in the contract seed")
	}
	for _, url := range urls {
		request := httptest.NewRequest(http.MethodGet, url, nil)
		request.Header.Set("Origin", "https://client.example.invalid")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Body.String() != "synthetic contract media\n" ||
			response.Header().Get("Content-Type") != "image/png" ||
			response.Header().Get("Cache-Control") != "public, max-age=0" ||
			response.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("GET %s = %d %q %v", url, response.Code, response.Body.String(), response.Header())
		}
	}
	for _, target := range []string{"/uploads/missing.png", "/uploads/../go.mod", "/uploads/%2e%2e/go.mod", "/uploads/"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusNotFound || response.Body.String() != `{"error":"Not found"}` {
			t.Errorf("GET %s = %d %q", target, response.Code, response.Body.String())
		}
	}
}

func TestUploadedFilesAreServedUntilDeleted(t *testing.T) {
	handler, _, _ := mediaApplication(t)
	contract, err := contracttest.Load(filepath.Join("..", "..", "contracts", "fixtures", "node-contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	authorization := contractBindings(t, handler, contract)["$AUTHORIZATION"]
	body := "--B\r\nContent-Disposition: form-data; name=\"file\"; filename=\"photo.webp\"\r\nContent-Type: image/webp\r\n\r\nwebp bytes\r\n--B--\r\n"
	request := httptest.NewRequest(http.MethodPost, "/api/dashboard/media/upload", strings.NewReader(body))
	request.Header.Set("Content-Type", "multipart/form-data; boundary=B")
	request.Header.Set("Authorization", authorization)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("upload = %d %s", response.Code, response.Body.String())
	}
	var uploaded struct {
		Media struct {
			ID  int64  `json:"id"`
			URL string `json:"url"`
		} `json:"media"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	served := httptest.NewRecorder()
	handler.ServeHTTP(served, httptest.NewRequest(http.MethodGet, uploaded.Media.URL, nil))
	if served.Code != http.StatusOK || served.Body.String() != "webp bytes" || served.Header().Get("Content-Type") != "image/webp" {
		t.Fatalf("GET %s = %d %q %v", uploaded.Media.URL, served.Code, served.Body.String(), served.Header())
	}
	remove := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/dashboard/media/%d", uploaded.Media.ID), nil)
	remove.Header.Set("Authorization", authorization)
	removed := httptest.NewRecorder()
	handler.ServeHTTP(removed, remove)
	if removed.Code != http.StatusOK {
		t.Fatalf("delete = %d %s", removed.Code, removed.Body.String())
	}
	gone := httptest.NewRecorder()
	handler.ServeHTTP(gone, httptest.NewRequest(http.MethodGet, uploaded.Media.URL, nil))
	if gone.Code != http.StatusNotFound {
		t.Fatalf("GET after delete = %d", gone.Code)
	}
}
