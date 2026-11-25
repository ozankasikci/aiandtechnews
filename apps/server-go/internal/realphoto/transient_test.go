package realphoto_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

type textModelFunc func(context.Context, string) (string, error)

func (f textModelFunc) GenerateJSON(ctx context.Context, prompt string) (string, error) {
	return f(ctx, prompt)
}

type rateLimitedVision struct{}

func (rateLimitedVision) ReviewImage(context.Context, string, []byte, map[string]any) (string, error) {
	return "", &gemini.Error{Status: http.StatusTooManyRequests}
}

func TestFindTreatsAVisionRateLimitAsATransientMiss(t *testing.T) {
	var queries []string
	finder := realphoto.NewFinder(&fakeText{plan: robotPlan}, rateLimitedVision{}, &http.Client{Transport: twoFiles(t, &queries)}, nil)
	result := finder.Find(context.Background(), realphoto.Request{Title: "WiCi One"})
	if result.Found || !result.Transient {
		t.Fatalf("found = %t, transient = %t, want a transient miss", result.Found, result.Transient)
	}
}

func TestFindDistinguishesTransientMissesFromNoPhoto(t *testing.T) {
	ctx := context.Background()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	rateLimit := func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}
	for _, tc := range []struct {
		name      string
		ctx       context.Context
		planner   realphoto.TextModel
		routes    sites
		transient bool
	}{
		{"failed plan call", ctx, textModelFunc(func(context.Context, string) (string, error) {
			return "", &url.Error{Op: "Post", URL: "https://gemini.test", Err: errors.New("connection reset")}
		}), sites{}, true},
		{"cancelled context", cancelled, &fakeText{plan: fundingPlan}, sites{}, true},
		{"Commons search 429", ctx, &fakeText{plan: robotPlan}, sites{"commons.wikimedia.org": rateLimit}, true},
		{"official page 429", ctx, &fakeText{plan: officialRobotPlan}, sites{"wici.ai": rateLimit}, true},
		{"unparsable plan", ctx, &fakeText{plan: `not JSON`}, sites{}, false},
		{"plan without a subject", ctx, &fakeText{plan: `{"photographable":true}`}, sites{}, false},
		{"no photographable subject", ctx, &fakeText{plan: fundingPlan}, sites{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			finder := realphoto.NewFinder(tc.planner, &fakeVision{}, &http.Client{Transport: tc.routes}, nil)
			result := finder.Find(tc.ctx, realphoto.Request{Title: "Headline"})
			if result.Found || result.Transient != tc.transient {
				t.Fatalf("result = %+v, want a miss with transient = %t", result, tc.transient)
			}
		})
	}
}

func TestPlanModelErrorsAreRetriedOnlyWhenTransient(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		transient bool
	}{
		{"Gemini 429", &gemini.Error{Status: http.StatusTooManyRequests}, true},
		{"Gemini 5xx", &gemini.Error{Status: http.StatusServiceUnavailable}, true},
		{"Gemini 400", &gemini.Error{Status: http.StatusBadRequest}, false},
		{"blocked plan", gemini.ErrBlocked, false},
		{"context deadline", context.DeadlineExceeded, true},
		{"truncated network response", io.ErrUnexpectedEOF, true},
		{"generic failure", errors.New("bad request configuration"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			planner := textModelFunc(func(context.Context, string) (string, error) { return "", tc.err })
			finder := realphoto.NewFinder(planner, &fakeVision{}, &http.Client{Transport: sites{}}, nil)
			result := finder.Find(context.Background(), realphoto.Request{})
			if result.Found || result.Transient != tc.transient {
				t.Fatalf("found = %t, transient = %t, want a miss with transient = %t", result.Found, result.Transient, tc.transient)
			}
		})
	}
}

func TestOfficialHTTPRateLimitCountsOnlyForTheChosenPage(t *testing.T) {
	for _, tc := range []struct {
		name, choice             string
		sourceLimited, transient bool
		wantLinkReads            int
	}{
		{name: "linked title", choice: `{"index":-1}`, wantLinkReads: 1},
		{name: "chosen official page", choice: `{"index":0}`, wantLinkReads: 2, transient: true},
		{name: "news source", sourceLimited: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var queries []string
			linkReads := 0
			routes := sites{
				"commons.wikimedia.org": commonsAPI(t, nil, &queries),
				"publisher.test": func(w http.ResponseWriter, r *http.Request) {
					if tc.sourceLimited {
						w.WriteHeader(http.StatusTooManyRequests)
						return
					}
					serveBytes([]byte(`<a href="https://wici.ai/wici-one">WiCi</a>`), "text/html")(w, r)
				},
				"wici.ai": func(w http.ResponseWriter, _ *http.Request) {
					linkReads++
					w.WriteHeader(http.StatusTooManyRequests)
				},
			}
			finder := realphoto.NewFinder(&fakeText{plan: robotPlan, official: tc.choice}, &fakeVision{}, &http.Client{Transport: routes}, nil)
			result := finder.Find(context.Background(), realphoto.Request{SourceURL: "https://publisher.test/article"})
			if result.Found || result.Transient != tc.transient || linkReads != tc.wantLinkReads {
				t.Fatalf("found = %t, transient = %t, reads = %d, want a miss with transient = %t and %d reads", result.Found, result.Transient, linkReads, tc.transient, tc.wantLinkReads)
			}
		})
	}
}

type rateLimitedSearcher struct{ calls int }

func (s *rateLimitedSearcher) SearchText(context.Context, string) (string, error) {
	s.calls++
	return "", &gemini.Error{Status: http.StatusTooManyRequests}
}

func TestMakerPageSearchRateLimitMakesMissTransient(t *testing.T) {
	var queries []string
	routes := sites{"commons.wikimedia.org": commonsAPI(t, nil, &queries)}
	search := &rateLimitedSearcher{}
	finder := realphoto.NewFinder(&fakeText{plan: officialRobotPlan}, &fakeVision{}, &http.Client{Transport: routes}, nil).WithSearch(search)
	result := finder.Find(context.Background(), realphoto.Request{})
	if result.Found || !result.Transient || search.calls != 1 {
		t.Fatalf("found = %t, transient = %t, maker search calls = %d, want a transient miss after one search", result.Found, result.Transient, search.calls)
	}
}

func TestChooseOfficialRateLimitMakesMissTransient(t *testing.T) {
	var queries []string
	routes := sites{
		"commons.wikimedia.org": commonsAPI(t, nil, &queries),
		"publisher.test":        serveBytes([]byte(`<a href="https://wici.ai/wici-one">WiCi</a>`), "text/html"),
		"wici.ai":               serveBytes([]byte(`<title>WiCi One</title>`), "text/html"),
	}
	choices := 0
	model := textModelFunc(func(_ context.Context, prompt string) (string, error) {
		if strings.Contains(prompt, "OFFICIAL website") {
			choices++
			return "", &gemini.Error{Status: http.StatusTooManyRequests}
		}
		return robotPlan, nil
	})
	finder := realphoto.NewFinder(model, &fakeVision{}, &http.Client{Transport: routes}, nil)
	result := finder.Find(context.Background(), realphoto.Request{SourceURL: "https://publisher.test/article"})
	if result.Found || !result.Transient || choices != 1 {
		t.Fatalf("found = %t, transient = %t, official choices = %d, want a transient miss after one choice", result.Found, result.Transient, choices)
	}
}
