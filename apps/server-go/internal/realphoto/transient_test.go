package realphoto_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

type failedPlanner struct{}

func (failedPlanner) GenerateJSON(context.Context, string) (string, error) {
	return "", errors.New("planner offline")
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
		{"failed plan call", ctx, failedPlanner{}, sites{}, true},
		{"cancelled context", cancelled, &fakeText{plan: fundingPlan}, sites{}, true},
		{"Commons search 429", ctx, &fakeText{plan: robotPlan}, sites{"commons.wikimedia.org": rateLimit}, true},
		{"official page 429", ctx, &fakeText{plan: officialRobotPlan}, sites{"wici.ai": rateLimit}, true},
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
