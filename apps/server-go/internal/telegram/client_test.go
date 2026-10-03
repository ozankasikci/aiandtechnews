package telegram_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/telegram"
)

const token = "123456:SECRET-token"

// request is one call the fake Bot API received.
type request struct {
	method   string
	fields   map[string]string
	photo    []byte
	filename string
}

type fakeBot struct {
	mu       sync.Mutex
	requests []request
	// reply answers each method; nil means ok with message_id 42.
	reply  map[string]func(w http.ResponseWriter)
	images map[string][]byte
}

func newFakeBot(t *testing.T) (*fakeBot, *httptest.Server) {
	t.Helper()
	bot := &fakeBot{reply: map[string]func(http.ResponseWriter){}, images: map[string][]byte{}}
	server := httptest.NewServer(http.HandlerFunc(bot.serve(t)))
	t.Cleanup(server.Close)
	return bot, server
}

func (b *fakeBot) serve(t *testing.T) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/images/") {
			data, ok := b.images[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(data)
			return
		}
		prefix := "/bot" + token + "/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			t.Errorf("path = %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		got := request{method: strings.TrimPrefix(r.URL.Path, prefix), fields: map[string]string{}}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				t.Errorf("multipart: %v", err)
			}
			for key, values := range r.MultipartForm.Value {
				got.fields[key] = values[0]
			}
			if files := r.MultipartForm.File["photo"]; len(files) == 1 {
				file, _ := files[0].Open()
				got.photo, _ = io.ReadAll(file)
				got.filename = files[0].Filename
			}
		} else {
			_ = r.ParseForm()
			for key, values := range r.PostForm {
				got.fields[key] = values[0]
			}
		}
		b.mu.Lock()
		b.requests = append(b.requests, got)
		reply := b.reply[got.method]
		b.mu.Unlock()
		if reply != nil {
			reply(w)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":42}}`)
	}
}

func pngImage() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 32, 18))
	for x := 0; x < 32; x++ {
		img.Set(x, x%18, color.RGBA{R: 200, A: 255})
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		panic(err)
	}
	return out.Bytes()
}

func TestPostUploadsAPNGPhotoWithAnHTMLCaption(t *testing.T) {
	bot, server := newFakeBot(t)
	bot.images["/images/a.png"] = pngImage()
	client := telegram.NewClient(token, server.URL, nil)
	id, err := client.Post(context.Background(), "@channel", "<b>Hi</b>", server.URL+"/images/a.png")
	if err != nil || id != 42 {
		t.Fatalf("Post() = %d, %v", id, err)
	}
	if len(bot.requests) != 1 {
		t.Fatalf("requests = %+v", bot.requests)
	}
	got := bot.requests[0]
	if got.method != "sendPhoto" || got.fields["chat_id"] != "@channel" || got.fields["caption"] != "<b>Hi</b>" ||
		got.fields["parse_mode"] != "HTML" || got.filename != "photo.png" || !bytes.Equal(got.photo, bot.images["/images/a.png"]) {
		t.Fatalf("request = %+v", got)
	}
}

func TestPostConvertsWebPToJPEG(t *testing.T) {
	bot, server := newFakeBot(t)
	webp, _, _, err := imaging.EncodeWebP(pngImage(), 80)
	if err != nil {
		t.Fatal(err)
	}
	bot.images["/images/a.webp"] = webp
	client := telegram.NewClient(token, server.URL, nil)
	if _, err := client.Post(context.Background(), "@channel", "text", server.URL+"/images/a.webp"); err != nil {
		t.Fatal(err)
	}
	got := bot.requests[0]
	if got.method != "sendPhoto" || got.filename != "photo.jpg" || imaging.SniffMIME(got.photo) != "image/jpeg" {
		t.Fatalf("request = %s %s %s", got.method, got.filename, imaging.SniffMIME(got.photo))
	}
}

func TestPostFallsBackToAMessage(t *testing.T) {
	for name, setup := range map[string]func(*fakeBot, string) string{
		"no image":      func(*fakeBot, string) string { return "" },
		"missing image": func(_ *fakeBot, url string) string { return url + "/images/missing.webp" },
		"not an image": func(b *fakeBot, url string) string {
			b.images["/images/x"] = []byte("<html>")
			return url + "/images/x"
		},
		"broken WebP": func(b *fakeBot, url string) string {
			b.images["/images/b"] = []byte("RIFF\x00\x00\x00\x00WEBPjunk")
			return url + "/images/b"
		},
		"photo rejected": rejectPhoto,
	} {
		t.Run(name, func(t *testing.T) {
			bot, server := newFakeBot(t)
			imageURL := setup(bot, server.URL)
			id, err := telegram.NewClient(token, server.URL, nil).Post(context.Background(), "-100123", "<b>T</b>", imageURL)
			if err != nil || id != 42 {
				t.Fatalf("Post() = %d, %v", id, err)
			}
			last := bot.requests[len(bot.requests)-1]
			if last.method != "sendMessage" || last.fields["chat_id"] != "-100123" || last.fields["text"] != "<b>T</b>" ||
				last.fields["parse_mode"] != "HTML" || last.fields["disable_web_page_preview"] != "false" {
				t.Fatalf("request = %+v", last)
			}
		})
	}
}

func rejectPhoto(b *fakeBot, url string) string {
	b.images["/images/a.png"] = pngImage()
	b.reply["sendPhoto"] = func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"ok":false,"error_code":400,"description":"Bad Request: IMAGE_PROCESS_FAILED"}`)
	}
	return url + "/images/a.png"
}

func TestFloodControlIsTransientAndNotDowngraded(t *testing.T) {
	bot, server := newFakeBot(t)
	bot.images["/images/a.png"] = pngImage()
	bot.reply["sendPhoto"] = func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 17","parameters":{"retry_after":17}}`)
	}
	_, err := telegram.NewClient(token, server.URL, nil).Post(context.Background(), "@c", "t", server.URL+"/images/a.png")
	if err == nil || !telegram.IsTransient(err) || telegram.RetryAfter(err) != 17*time.Second {
		t.Fatalf("err = %v transient = %t retry = %s", err, telegram.IsTransient(err), telegram.RetryAfter(err))
	}
	if len(bot.requests) != 1 {
		t.Fatalf("a 429 fell back to sendMessage: %+v", bot.requests)
	}
}

func TestPermanentErrorsAreNotTransient(t *testing.T) {
	bot, server := newFakeBot(t)
	bot.reply["sendMessage"] = func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"ok":false,"error_code":403,"description":"Forbidden: bot is not a member of the channel chat"}`)
	}
	_, err := telegram.NewClient(token, server.URL, nil).SendMessage(context.Background(), "@c", "t")
	if err == nil || telegram.IsTransient(err) || !strings.Contains(err.Error(), "bot is not a member") {
		t.Fatalf("err = %v", err)
	}
	bot.reply["sendMessage"] = func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `<html>bad gateway</html>`)
	}
	if _, err := telegram.NewClient(token, server.URL, nil).SendMessage(context.Background(), "@c", "t"); !telegram.IsTransient(err) {
		t.Fatalf("a 502 is not transient: %v", err)
	}
}

func TestErrorsNeverContainTheToken(t *testing.T) {
	bot, server := newFakeBot(t)
	bot.reply["sendMessage"] = func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"ok":false,"description":"Unauthorized for `+token+`"}`)
	}
	_, err := telegram.NewClient(token, server.URL, nil).SendMessage(context.Background(), "@c", "t")
	if err == nil || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v", err)
	}
	// A transport error quotes the request URL, token included.
	server.Close()
	_, err = telegram.NewClient(token, server.URL, nil).SendMessage(context.Background(), "@c", "t")
	if err == nil || !telegram.IsTransient(err) || strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("err = %v", err)
	}
}
