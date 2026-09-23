package media

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// uploadField is the single file field (upload.single("file"), dashboard.ts:586).
const uploadField = "file"

// maxUploadRequestBytes caps the whole multipart body: one maximal file plus
// 1 MiB for part headers and any text fields, which are discarded. Node has no
// total cap; this bounds work, while memory stays constant because parts are
// streamed straight to disk.
const maxUploadRequestBytes = MaxUploadBytes + 1<<20

// UploadReadTimeout replaces the server's ReadTimeout (15s) for the upload
// route only: a 5 MiB body on a slow link needs longer, while every other
// route keeps the short server-wide limit. uploadWriteTimeout lets the
// response be written after a body that took the whole read window.
const (
	UploadReadTimeout  = 2 * time.Minute
	uploadWriteTimeout = UploadReadTimeout + 30*time.Second
)

// extendUploadDeadlines moves this connection's read and write deadlines via
// http.ResponseController. Writers without deadline support (httptest
// recorders) are left alone.
func extendUploadDeadlines(w http.ResponseWriter, now time.Time) error {
	controller := http.NewResponseController(w)
	if err := controller.SetReadDeadline(now.Add(UploadReadTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	if err := controller.SetWriteDeadline(now.Add(uploadWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

// allowedExtensions is upload.ts's fileFilter MIME allowlist (the part's
// lowercased type/subtype; busboy drops parameters), extended by an approved
// change: the original name's extension, compared case-insensitively, must be
// an image extension matching that type. Node checked the type only, so a
// file named evil.html declared as image/png was stored and served as HTML.
var allowedExtensions = map[string]map[string]bool{
	"image/jpeg": {".jpg": true, ".jpeg": true},
	"image/png":  {".png": true},
	"image/webp": {".webp": true},
	"image/gif":  {".gif": true},
}

// acceptedImage reports whether the declared type and the original name pass
// the upload filter. The stored name keeps the extension's original case.
func acceptedImage(mimeType, originalName string) bool {
	return allowedExtensions[mimeType][strings.ToLower(nodeExtname(originalName))]
}

// Messages of the multer errors that Express turns into a 500 page.
const (
	messageInvalidType     = "Only .jpg, .png, .webp, and .gif files are allowed"
	messageUnexpectedField = "Unexpected field"
)

// uploadError is a multer rejection. Node answers each with Express's default
// 500 HTML page; Go answers 500 with the same message as JSON.
type uploadError struct{ message string }

func (e *uploadError) Error() string { return e.message }

var errNoFile = errors.New("No file uploaded")

type libraryService interface {
	List(context.Context) ([]Item, error)
	Record(context.Context, StoredFile, string, string) (Item, error)
	Delete(context.Context, string) error
}

// Handler serves the authenticated dashboard media routes.
type Handler struct {
	library libraryService
	files   Storage
	logger  *slog.Logger
}

func NewHandler(library libraryService, files Storage, logger *slog.Logger) *Handler {
	return &Handler{library: library, files: files, logger: logger}
}

// Mount registers the routes relative to /api/dashboard. The caller owns
// authentication: every route here must sit behind RequireAuth.
func (h *Handler) Mount(router chi.Router) {
	router.Get("/media", h.list)
	router.Post("/media/upload", h.upload)
	router.Delete("/media/{id}", h.delete)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.library.List(r.Context())
	if err != nil {
		h.internalError(w, r, "list media", err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Media []Item `json:"media"`
	}{items})
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	// Extend the deadline before the multipart body is read.
	if err := extendUploadDeadlines(w, time.Now()); err != nil {
		h.logger.WarnContext(r.Context(), "extend media upload deadlines", "error", err)
	}
	upload, err := h.receive(w, r)
	var rejected *uploadError
	switch {
	case errors.Is(err, errNoFile):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": errNoFile.Error()})
		return
	case errors.As(err, &rejected):
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": rejected.message})
		return
	case err != nil:
		h.internalError(w, r, "receive media upload", err)
		return
	}
	item, err := h.library.Record(r.Context(), upload.file, upload.originalName, upload.mimeType)
	if err != nil {
		h.internalError(w, r, "record media upload", err)
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		Media Item `json:"media"`
	}{item})
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	err := h.library.Delete(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, ErrMediaNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": ErrMediaNotFound.Error()})
		return
	}
	if err != nil {
		h.internalError(w, r, "delete media", err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Success bool `json:"success"`
	}{true})
}

type receivedUpload struct {
	file         StoredFile
	originalName string
	mimeType     string
}

// receive ports multer 1.4.5-lts.2 upload.single("file") over busboy 1.6:
//   - a request that is not multipart has no file (an empty multipart body is
//     malformed, a 500, as busboy reports "Unexpected end of form");
//   - a file part is one with a filename parameter (or an
//     application/octet-stream part); an empty name after busboy's basename is
//     skipped, as multer ignores files without a name;
//   - a file in any field but "file", or a second one there, is "Unexpected
//     field"; then the type filter applies (Node's MIME allowlist plus the
//     approved extension match); then the size limit;
//   - text fields are ignored.
//
// Parts are streamed; only the accepted file reaches the storage backend, and
// it is removed again if a later part fails.
func (h *Handler) receive(w http.ResponseWriter, r *http.Request) (receivedUpload, error) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return receivedUpload{}, errNoFile
	}
	if mediaType != "multipart/form-data" {
		return receivedUpload{}, errors.New("unsupported multipart content type " + mediaType)
	}
	if params["boundary"] == "" {
		return receivedUpload{}, errors.New("multipart: boundary not found")
	}
	reader := multipart.NewReader(http.MaxBytesReader(w, r.Body, maxUploadRequestBytes), params["boundary"])
	var accepted *receivedUpload
	fail := func(err error) (receivedUpload, error) {
		if accepted != nil {
			if removeErr := h.files.Remove(r.Context(), accepted.file.URL); removeErr != nil {
				err = errors.Join(err, removeErr)
			}
		}
		return receivedUpload{}, err
	}
	for {
		part, err := reader.NextRawPart()
		// Only a bare io.EOF is the closing boundary; the reader wraps io.EOF
		// ("multipart: NextPart: EOF") for bodies that end without one.
		if err == io.EOF {
			break
		}
		if err != nil {
			return fail(err)
		}
		field, filename, isFile := describePart(part)
		if !isFile || filename == "" {
			if _, err := io.Copy(io.Discard, part); err != nil {
				return fail(err)
			}
			continue
		}
		if field != uploadField || accepted != nil {
			return fail(&uploadError{messageUnexpectedField})
		}
		partType := partMIMEType(part.Header.Get("Content-Type"))
		if !acceptedImage(partType, filename) {
			return fail(&uploadError{messageInvalidType})
		}
		file, err := h.files.Save(r.Context(), part, filename, partType)
		if errors.Is(err, ErrFileTooLarge) {
			return fail(&uploadError{ErrFileTooLarge.Error()})
		}
		if err != nil {
			return fail(err)
		}
		accepted = &receivedUpload{file: file, originalName: filename, mimeType: partType}
	}
	if accepted == nil {
		return receivedUpload{}, errNoFile
	}
	return *accepted, nil
}

// describePart reads a part's Content-Disposition like busboy: parts that are
// not form-data are skipped; filename* wins over filename; a plain filename is
// decoded as latin1; busboy's basename is applied.
func describePart(part *multipart.Part) (field, filename string, isFile bool) {
	disposition := part.Header.Get("Content-Disposition")
	kind, params, err := mime.ParseMediaType(disposition)
	if err != nil || kind != "form-data" {
		return "", "", false
	}
	name, hasName := params["filename"]
	if hasName && !strings.Contains(strings.ToLower(disposition), "filename*") {
		name = latin1ToUTF8(name)
	}
	isFile = (hasName && name != "") || partMIMEType(part.Header.Get("Content-Type")) == "application/octet-stream"
	return params["name"], busboyBasename(name), isFile
}

// partMIMEType ports busboy's part Content-Type handling: the lowercased
// type/subtype without parameters, or text/plain when absent or malformed.
func partMIMEType(value string) string {
	if value == "" {
		return "text/plain"
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil || !strings.Contains(mediaType, "/") {
		return "text/plain"
	}
	return mediaType
}

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, message string, err error) {
	h.logger.ErrorContext(r.Context(), message, "error", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal server error"})
}
