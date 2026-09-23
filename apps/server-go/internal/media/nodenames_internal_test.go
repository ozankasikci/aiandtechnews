package media

import "testing"

// The expected values below were recorded from Node 22 (path.extname,
// path.posix.basename), busboy 1.6 (basename), and send 0.19's mime@1.6
// (lookup + charsets) on this repository's apps/server dependencies.

func TestNodeExtnameMatchesNode(t *testing.T) {
	for name, want := range map[string]string{
		"capture.png":  ".png",
		"noext":        "",
		"A.Photo.JPEG": ".JPEG",
		".png":         "",
		"a.":           ".",
		"..png":        ".png",
		"a..":          ".",
		"a.b.c":        ".c",
		".a.b":         ".b",
		"...":          ".",
		"a.tar.gz":     ".gz",
		"x. png":       ". png",
		".":            "",
		"..":           "",
		"a.p?g":        ".p?g",
		"":             "",
	} {
		if got := nodeExtname(name); got != want {
			t.Errorf("nodeExtname(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestBusboyBasenameMatchesBusboy(t *testing.T) {
	for name, want := range map[string]string{
		"capture.png":      "capture.png",
		"../../x.png":      "x.png",
		`C:\dir\y.gif`:     "y.gif",
		"dir/":             "",
		"a/..":             "",
		"a/.":              "",
		".":                "",
		"..":               "",
		"...":              "...",
		"":                 "",
		`mixed/path\z.png`: "z.png",
	} {
		if got := busboyBasename(name); got != want {
			t.Errorf("busboyBasename(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestNodeBasenameMatchesPathPosixBasename(t *testing.T) {
	for value, want := range map[string]string{
		"/uploads/abc.png":               "abc.png",
		"/uploads/sub/":                  "sub",
		"/uploads/../secret.txt":         "secret.txt",
		"https://cdn.example/a/link.txt": "link.txt",
		"/":                              "",
		"":                               "",
		"plain":                          "plain",
		"/uploads/..":                    "..",
	} {
		if got := nodeBasename(value); got != want {
			t.Errorf("nodeBasename(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestLatin1ToUTF8ReproducesBusboyParamDecoding(t *testing.T) {
	if got := latin1ToUTF8("café.webp"); got != "cafÃ©.webp" {
		t.Fatalf("latin1ToUTF8 = %q", got)
	}
	if got := latin1ToUTF8("plain.png"); got != "plain.png" {
		t.Fatalf("latin1ToUTF8 ASCII = %q", got)
	}
}

func TestStaticContentTypeMatchesSendMime(t *testing.T) {
	for name, want := range map[string]string{
		"a.png":                            "image/png",
		"a.PNG":                            "image/png",
		"a.jpg":                            "image/jpeg",
		"a.jpeg":                           "image/jpeg",
		"a.JPEG":                           "image/jpeg",
		"a.jpe":                            "image/jpeg",
		"a.gif":                            "image/gif",
		"a.webp":                           "image/webp",
		"a.svg":                            "image/svg+xml",
		"a.ico":                            "image/x-icon",
		"a.bmp":                            "image/bmp",
		"a.tif":                            "image/tiff",
		"a.apng":                           "image/apng",
		"a.avif":                           "application/octet-stream",
		"a.heic":                           "application/octet-stream",
		"a.jfif":                           "application/octet-stream",
		"a.html":                           "text/html; charset=UTF-8",
		"a.htm":                            "text/html; charset=UTF-8",
		"a.txt":                            "text/plain; charset=UTF-8",
		"a.json":                           "application/json; charset=UTF-8",
		"a.js":                             "application/javascript; charset=UTF-8",
		"a.xml":                            "application/xml",
		"a.pdf":                            "application/pdf",
		"a.":                               "application/octet-stream",
		"noext":                            "application/octet-stream",
		"sub/png":                          "image/png",
		"cccff1c03913c356183d1ff7cdf1fd41": "application/octet-stream",
	} {
		if got := staticContentType(name); got != want {
			t.Errorf("staticContentType(%q) = %q, want %q", name, got, want)
		}
	}
}
