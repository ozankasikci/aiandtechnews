package siterevalidate_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/siterevalidate"
)

type recorded struct {
	auth, contentType string
	slugs             []string
}

func recordingServer(t *testing.T, status int) (*httptest.Server, func() []recorded) {
	t.Helper()
	var mu sync.Mutex
	var calls []recorded
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Slugs []string `json:"slugs"`
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		mu.Lock()
		calls = append(calls, recorded{r.Header.Get("Authorization"), r.Header.Get("Content-Type"), body.Slugs})
		mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server, func() []recorded {
		mu.Lock()
		defer mu.Unlock()
		return append([]recorded(nil), calls...)
	}
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestQueuePostsSlugsWithTheCronSecret(t *testing.T) {
	server, calls := recordingServer(t, http.StatusOK)
	notifier, drain := siterevalidate.New(server.URL+"/api/revalidate", " secret-value ", quietLogger())
	notifier.Notify([]string{"one", "two", "one", " "})
	drain()
	got := calls()
	if len(got) != 1 {
		t.Fatalf("calls = %#v", got)
	}
	if got[0].auth != "Bearer secret-value" || got[0].contentType != "application/json" || fmt.Sprint(got[0].slugs) != "[one two]" {
		t.Fatalf("call = %#v", got[0])
	}
}

func TestQueueSplitsLongSlugListsIntoBatches(t *testing.T) {
	server, calls := recordingServer(t, http.StatusOK)
	notifier, drain := siterevalidate.New(server.URL, "s", quietLogger())
	slugs := make([]string, 120)
	for i := range slugs {
		slugs[i] = fmt.Sprintf("slug-%d", i)
	}
	notifier.Notify(slugs)
	drain()
	got := calls()
	if len(got) != 3 || len(got[0].slugs) != 50 || len(got[1].slugs) != 50 || len(got[2].slugs) != 20 {
		t.Fatalf("batches = %d", len(got))
	}
}

func TestQueueFailuresAreOnlyLogged(t *testing.T) {
	server, calls := recordingServer(t, http.StatusUnauthorized)
	notifier, drain := siterevalidate.New(server.URL, "wrong", quietLogger())
	notifier.Notify([]string{"one"})
	drain()
	if len(calls()) != 1 {
		t.Fatal("request not sent")
	}
	// An unreachable site does not panic or block either.
	unreachable, drainUnreachable := siterevalidate.New("http://127.0.0.1:1/api/revalidate", "s", quietLogger())
	unreachable.Notify([]string{"one"})
	drainUnreachable()
}

func TestDisabledWithoutURLAndDropsAfterClose(t *testing.T) {
	notifier, drain := siterevalidate.New("  ", "s", quietLogger())
	if _, ok := notifier.(siterevalidate.Disabled); !ok {
		t.Fatalf("notifier = %T, want Disabled", notifier)
	}
	notifier.Notify([]string{"one"})
	drain()

	server, calls := recordingServer(t, http.StatusOK)
	queue, closeQueue := siterevalidate.New(server.URL, "s", quietLogger())
	closeQueue()
	queue.Notify([]string{"late"})
	if len(calls()) != 0 {
		t.Fatal("notification after close was sent")
	}
}

type recordingNotifier struct{ got [][]string }

func (r *recordingNotifier) Notify(slugs []string) { r.got = append(r.got, slugs) }

func TestMultiNotifiesEveryNotifier(t *testing.T) {
	a, b := &recordingNotifier{}, &recordingNotifier{}
	siterevalidate.Multi{a, nil, b}.Notify([]string{"x"})
	if len(a.got) != 1 || len(b.got) != 1 {
		t.Fatalf("a=%v b=%v", a.got, b.got)
	}
}

func TestQueuePostsTopicsAndOmitsThemWhenEmpty(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
	}))
	t.Cleanup(server.Close)
	notifier, drain := siterevalidate.New(server.URL, "s", quietLogger())
	siterevalidate.NotifyWithTopics(notifier, []string{"a"}, []string{"nvidia", "openai", "nvidia"})
	siterevalidate.NotifyWithTopics(notifier, nil, []string{"rubin"})
	notifier.Notify([]string{"b"})
	drain()
	mu.Lock()
	defer mu.Unlock()
	joined := fmt.Sprint(bodies)
	for _, want := range []string{`{"slugs":["a"],"topics":["nvidia","openai"]}`, `{"slugs":[],"topics":["rubin"]}`, `{"slugs":["b"]}`} {
		found := false
		for _, body := range bodies {
			found = found || body == want
		}
		if !found {
			t.Fatalf("missing %s in %s", want, joined)
		}
	}
}
