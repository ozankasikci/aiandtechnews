package illustration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

func tinyPNG(t *testing.T) []byte { return solidPNG(t, 8, 8) }

func solidPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

const compliantJSON = `{"has_readable_text":false,"has_logo_or_watermark":false,"has_flag_or_emblem":false,"has_recognizable_real_person":false,"depicts_unsupported_injury_or_violence":false,"notes":"clean"}`
const textViolationJSON = `{"has_readable_text":true,"has_logo_or_watermark":false,"has_flag_or_emblem":false,"has_recognizable_real_person":false,"depicts_unsupported_injury_or_violence":false,"notes":"text"}`

func TestParseVerdict(t *testing.T) {
	clean := illustration.ParseVerdict(compliantJSON)
	if !clean.Compliant || clean.Unverified {
		t.Fatalf("clean verdict = %+v", clean)
	}
	fenced := illustration.ParseVerdict("```json\n" + textViolationJSON + "\n```")
	if fenced.Compliant || !fenced.HasText || fenced.Unverified {
		t.Fatalf("fenced verdict = %+v", fenced)
	}
	flag := illustration.ParseVerdict(strings.Replace(compliantJSON, `"has_flag_or_emblem":false`, `"has_flag_or_emblem":true`, 1))
	if flag.Compliant || !flag.HasFlag {
		t.Fatalf("flag verdict = %+v", flag)
	}
	person := illustration.ParseVerdict(strings.Replace(compliantJSON, `"has_recognizable_real_person":false`, `"has_recognizable_real_person":true`, 1))
	if person.Compliant || !person.HasPerson {
		t.Fatalf("person verdict = %+v", person)
	}
	for _, raw := range []string{`{"has_readable_text":false}`, "not json at all", `{"has_text":false,"has_logo_or_watermark":false,"depicts_unsupported_injury_or_violence":false}`} {
		if verdict := illustration.ParseVerdict(raw); verdict.Compliant || !verdict.Unverified {
			t.Fatalf("%s: verdict = %+v, want unverified", raw, verdict)
		}
	}
}

func TestCorrectionPriority(t *testing.T) {
	cases := []struct {
		verdict illustration.Verdict
		want    string
	}{
		{illustration.Verdict{HasText: true, HasLogo: true, HasInjury: true}, illustration.TextViolationCorrection},
		{illustration.Verdict{HasLogo: true, HasFlag: true}, illustration.LogoViolationCorrection},
		{illustration.Verdict{HasFlag: true, HasPerson: true}, illustration.FlagViolationCorrection},
		{illustration.Verdict{HasPerson: true, HasInjury: true}, illustration.PersonViolationCorrection},
		{illustration.Verdict{HasInjury: true}, illustration.InjuryViolationCorrection},
		{illustration.Verdict{}, illustration.UnverifiedCorrection},
	}
	for _, tc := range cases {
		if got := illustration.Correction(tc.verdict); got != tc.want {
			t.Errorf("Correction(%+v) = %q, want %q", tc.verdict, got, tc.want)
		}
	}
}

func TestCompliancePromptAllowsSmallGlyphsAndAnonymousFaces(t *testing.T) {
	prompt := illustration.BuildCompliancePrompt(illustration.Article{Title: "H", Excerpt: "S"})
	for _, want := range []string{"Article headline: H", "flag", "coat of arms", "cannot be read as characters do NOT count", "anonymous figures are fine"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("compliance prompt lacks %q", want)
		}
	}
}

type fakeVision struct {
	raw string
	err error
}

func (f *fakeVision) ReviewImage(context.Context, string, []byte, map[string]any) (string, error) {
	return f.raw, f.err
}

func TestGeminiReviewerErrors(t *testing.T) {
	article := illustration.Article{Title: "T", Excerpt: "E"}
	verdict, err := illustration.NewGeminiReviewer(&fakeVision{raw: compliantJSON}).Review(context.Background(), article, tinyPNG(t))
	if err != nil || !verdict.Compliant {
		t.Fatalf("clean: %+v %v", verdict, err)
	}
	verdict, err = illustration.NewGeminiReviewer(&fakeVision{raw: compliantJSON}).Review(context.Background(), article, []byte("not an image"))
	if err != nil || !verdict.Unverified {
		t.Fatalf("undecodable: %+v %v", verdict, err)
	}
	for name, reviewErr := range map[string]error{"empty": gemini.ErrEmptyResponse, "400": &gemini.Error{Status: 400}} {
		verdict, err := illustration.NewGeminiReviewer(&fakeVision{err: reviewErr}).Review(context.Background(), article, tinyPNG(t))
		if err != nil || !verdict.Unverified || verdict.Compliant {
			t.Fatalf("%s: %+v %v, want unverified", name, verdict, err)
		}
	}
	_, err = illustration.NewGeminiReviewer(&fakeVision{err: &gemini.Error{Status: 404}}).Review(context.Background(), article, tinyPNG(t))
	if !publisher.IsSystemFault(err) {
		t.Fatalf("404: err = %v, want system fault", err)
	}
	transient := errors.New("connection reset")
	_, err = illustration.NewGeminiReviewer(&fakeVision{err: transient}).Review(context.Background(), article, tinyPNG(t))
	if !errors.Is(err, transient) || publisher.IsSystemFault(err) || publisher.IsPermanent(err) {
		t.Fatalf("network: err = %v, want transient", err)
	}
}

func TestFetchReference(t *testing.T) {
	png := tinyPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
		case "/svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte("<svg></svg>"))
		case "/html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html></html>"))
		case "/notfound":
			w.WriteHeader(http.StatusNotFound)
		case "/big":
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Length", "9000000")
			_, _ = w.Write(png)
		case "/undeclared-big":
			w.Header().Set("Content-Type", "image/png")
			chunk := bytes.Repeat([]byte{0}, 1<<20)
			for written := 0; written <= illustration.MaxReferenceBytes; written += len(chunk) {
				if _, err := w.Write(chunk); err != nil {
					return
				}
				w.(http.Flusher).Flush()
			}
		}
	}))
	defer server.Close()
	client := server.Client()

	if data, err := illustration.FetchReference(context.Background(), client, server.URL+"/png"); err != nil || !bytes.Equal(data, png) {
		t.Fatalf("png: data=%v err=%v", data, err)
	}
	for path, reason := range map[string]string{
		"/svg":            "content type",
		"/html":           "content type",
		"/notfound":       "HTTP 404",
		"/big":            "declares",
		"/undeclared-big": "exceeds",
	} {
		data, err := illustration.FetchReference(context.Background(), client, server.URL+path)
		if data != nil || !errors.Is(err, illustration.ErrUnusableReference) || !strings.Contains(err.Error(), reason) {
			t.Fatalf("%s: data=%d bytes err=%v, want unusable (%s)", path, len(data), err, reason)
		}
	}
	if data, err := illustration.FetchReference(context.Background(), client, ""); err != nil || data != nil {
		t.Fatalf("empty: data=%v err=%v", data, err)
	}
}

func TestReferenceDialGuard(t *testing.T) {
	for _, address := range []string{
		"127.0.0.1:80", "10.0.0.1:443", "172.16.5.4:80", "192.168.1.1:80", "169.254.169.254:80",
		"[::1]:443", "[fe80::1]:80", "[fc00::1]:80", "224.0.0.1:80", "0.0.0.0:80", "[::]:80",
		"[::ffff:127.0.0.1]:80", "not-an-ip:80",
	} {
		if err := illustration.CheckReferenceDialAddress("tcp", address, nil); err == nil {
			t.Errorf("%s: dial allowed, want rejected", address)
		}
	}
	for _, address := range []string{"93.184.216.34:443", "[2606:2800:220:1:248:1893:25c8:1946]:443"} {
		if err := illustration.CheckReferenceDialAddress("tcp", address, nil); err != nil {
			t.Errorf("%s: %v, want allowed", address, err)
		}
	}
}

func TestReferenceClientRejectsLoopbackTargets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(tinyPNG(t))
	}))
	defer server.Close()
	_, err := illustration.FetchReference(context.Background(), illustration.NewReferenceClient(), server.URL+"/png")
	if !errors.Is(err, illustration.ErrUnusableReference) || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err = %v, want the loopback dial refused", err)
	}
}

func TestReferenceClientRedirectPolicy(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/file":
			http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/hop/"):
			var remaining int
			_, _ = fmt.Sscanf(r.URL.Path, "/hop/%d", &remaining)
			if remaining == 0 {
				w.Header().Set("Content-Type", "image/png")
				_, _ = w.Write(tinyPNG(t))
				return
			}
			http.Redirect(w, r, fmt.Sprintf("%s/hop/%d", server.URL, remaining-1), http.StatusFound)
		}
	}))
	defer server.Close()
	client := illustration.NewReferenceClientAllowingAnyAddress()

	if _, err := client.Get(server.URL + "/file"); err == nil || !strings.Contains(err.Error(), "scheme") {
		t.Fatalf("file redirect: err = %v, want scheme rejected", err)
	}
	resp, err := client.Get(server.URL + "/hop/3")
	if err != nil {
		t.Fatalf("3 redirects: %v", err)
	}
	_ = resp.Body.Close()
	if _, err := client.Get(server.URL + "/hop/4"); err == nil || !strings.Contains(err.Error(), "redirects") {
		t.Fatalf("4 redirects: err = %v, want rejected", err)
	}
}
