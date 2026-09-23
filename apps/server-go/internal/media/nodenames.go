package media

import "strings"

// This file ports the small pieces of Node, busboy, and the `mime@1.6` table
// that decide the stored upload name, the recorded original name, and the
// Content-Type of a served upload. Each function names its Node source.

// busboyBasename ports busboy 1.6 lib/utils.js basename(): everything after
// the last '/' or '\', with "." and ".." collapsed to "".
func busboyBasename(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	if name == "." || name == ".." {
		return ""
	}
	return name
}

// latin1ToUTF8 reproduces busboy's default parameter decoding
// (defParamCharset "latin1"): every byte of a plain filename parameter becomes
// the code point of the same value, so a UTF-8 "café.png" is recorded as
// "cafÃ©.png" exactly like Node.
func latin1ToUTF8(value string) string {
	runes := make([]rune, len(value))
	for i := 0; i < len(value); i++ {
		runes[i] = rune(value[i])
	}
	return string(runes)
}

// nodeExtname ports Node's path.posix.extname for a name without slashes:
// the text from the last '.' unless that dot starts the name
// (".png" -> "", "a." -> ".", "..png" -> ".png", ".." -> "").
func nodeExtname(name string) string {
	startDot, startPart, end := -1, 0, -1
	matchedSlash := true
	preDotState := 0
	for i := len(name) - 1; i >= 0; i-- {
		c := name[i]
		if c == '/' {
			if !matchedSlash {
				startPart = i + 1
				break
			}
			continue
		}
		if end == -1 {
			matchedSlash = false
			end = i + 1
		}
		if c == '.' {
			if startDot == -1 {
				startDot = i
			} else if preDotState != 1 {
				preDotState = 1
			}
		} else if startDot != -1 {
			preDotState = -1
		}
	}
	if startDot == -1 || end == -1 || preDotState == 0 ||
		(preDotState == 1 && startDot == end-1 && startDot == startPart+1) {
		return ""
	}
	return name[startDot:end]
}

// nodeBasename ports Node's path.posix.basename without a suffix argument:
// trailing slashes are ignored and "" or "/" yield "".
func nodeBasename(value string) string {
	value = strings.TrimRight(value, "/")
	if i := strings.LastIndexByte(value, '/'); i >= 0 {
		return value[i+1:]
	}
	return value
}

// staticContentTypes is the part of send 0.19's mime@1.6 table that uploads
// can plausibly use, with send's charset rule (UTF-8 for text types and the
// JSON/JavaScript types) already applied. Anything else is served as
// application/octet-stream, which is also mime@1.6's answer for extensions it
// does not know (for example .avif, .heic, .jfif).
var staticContentTypes = map[string]string{
	"png":  "image/png",
	"apng": "image/apng",
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"jpe":  "image/jpeg",
	"gif":  "image/gif",
	"webp": "image/webp",
	"svg":  "image/svg+xml",
	"ico":  "image/x-icon",
	"bmp":  "image/bmp",
	"tif":  "image/tiff",
	"tiff": "image/tiff",
	"html": "text/html; charset=UTF-8",
	"htm":  "text/html; charset=UTF-8",
	"txt":  "text/plain; charset=UTF-8",
	"css":  "text/css; charset=UTF-8",
	"csv":  "text/csv; charset=UTF-8",
	"md":   "text/markdown; charset=UTF-8",
	"js":   "application/javascript; charset=UTF-8",
	"json": "application/json; charset=UTF-8",
	"xml":  "application/xml",
	"pdf":  "application/pdf",
	"zip":  "application/zip",
	"mp4":  "video/mp4",
	"webm": "video/webm",
}

// staticContentType ports mime@1.6 lookup(): the text after the last '.', '/'
// or '\', lowercased, looked up in the table above.
func staticContentType(name string) string {
	extension := name
	if i := strings.LastIndexAny(name, `./\`); i >= 0 {
		extension = name[i+1:]
	}
	if contentType, ok := staticContentTypes[strings.ToLower(extension)]; ok {
		return contentType
	}
	return "application/octet-stream"
}
