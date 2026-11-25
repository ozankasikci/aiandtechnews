package realphoto_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

const fundingPlan = `{"photographable":false,"subject":"","kind":"none","queries":[],"maker":"Sierra","reason":"A funding round.",
"stand_ins":[{"subject":"Bret Taylor","kind":"person","queries":["Bret Taylor","Bret Taylor Sierra"]}]}`

func TestParsePlanCleansStandIns(t *testing.T) {
	plan, err := realphoto.ParsePlan(`{"photographable":false,"reason":"r","stand_ins":[
		{"subject":"  Bret   Taylor ","kind":"Person","queries":["a","b","c"]},
		{"subject":"A mood","kind":"metaphor","queries":["x"]},
		{"subject":"","kind":"place","queries":["y"]},
		{"subject":"Sierra headquarters","kind":"place","queries":[]},
		{"subject":"Sierra app on a phone","kind":"product_in_use","queries":["Sierra app"]},
		{"subject":"Fourth","kind":"place","queries":["z"]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.StandIns) != realphoto.MaxStandIns {
		t.Fatalf("stand-ins = %+v, want %d", plan.StandIns, realphoto.MaxStandIns)
	}
	first, second := plan.StandIns[0], plan.StandIns[1]
	if first.Subject != "Bret Taylor" || first.Kind != "person" || len(first.Queries) != 2 {
		t.Fatalf("first = %+v", first)
	}
	if second.Subject != "Sierra headquarters" || len(second.Queries) != 1 || second.Queries[0] != "Sierra headquarters" {
		t.Fatalf("second = %+v: a stand-in without queries searches for its subject", second)
	}
}

func TestParsePlanDropsStandInsOfAPhotographableStory(t *testing.T) {
	plan, err := realphoto.ParsePlan(`{"photographable":true,"subject":"WiCi One","queries":["WiCi One"],"stand_ins":[{"subject":"x","kind":"place","queries":["x"]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.StandIns) != 0 {
		t.Fatalf("stand-ins = %+v, want none", plan.StandIns)
	}
}

func TestFindUsesAStandInOnlyForTheFeaturedSlot(t *testing.T) {
	var queries []string
	vision := &fakeVision{verdicts: map[string]string{"green": verdict(8, ""), "blue": verdict(9, "")}}
	finder := realphoto.NewFinder(&fakeText{plan: fundingPlan}, vision, &http.Client{Transport: twoFiles(t, &queries)}, nil)
	request := realphoto.Request{Slug: "sierra", Title: "Sierra raises $1.75 billion", Text: "..."}

	inline := finder.Find(context.Background(), request)
	if inline.Found || len(queries) != 0 {
		t.Fatalf("the body must not use a stand-in: found %t, queries %v", inline.Found, queries)
	}

	request.Slot = realphoto.SlotFeatured
	featured := finder.Find(context.Background(), request)
	if !featured.Found || featured.StandIn == nil || featured.StandIn.Subject != "Bret Taylor" {
		t.Fatalf("featured = %+v", featured)
	}
	if featured.Credit.Kind != realphoto.KindCommons || featured.Alt != "Photo of Bret Taylor." {
		t.Fatalf("credit %+v alt %q", featured.Credit, featured.Alt)
	}
	if len(queries) != 2 || queries[0] != "filetype:bitmap Bret Taylor" {
		t.Fatalf("queries = %v", queries)
	}
}

func TestFindStopsSearchingStandInsAfterAnImageDownloadIsRateLimited(t *testing.T) {
	const plan = `{"photographable":false,"maker":"Sierra","stand_ins":[
		{"subject":"Bret Taylor","kind":"person"},
		{"subject":"Sierra headquarters","kind":"place"}]}`
	var queries []string
	requests := 0
	routes := twoFiles(t, &queries)
	routes["upload.wikimedia.org"] = func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}
	finder := realphoto.NewFinder(&fakeText{plan: plan}, &fakeVision{}, &http.Client{Transport: routes}, nil)
	result := finder.Find(context.Background(), realphoto.Request{Title: "Sierra raises funding", Slot: realphoto.SlotFeatured})

	if len(queries) != 1 || requests != 1 {
		t.Fatalf("queries = %v, image downloads = %d, want one of each", queries, requests)
	}
	if result.StandIn != nil || !strings.Contains(strings.Join(result.Notes, " "), "rate limited") {
		t.Fatalf("result = %+v, want no chosen stand-in and a rate-limit note", result)
	}
	assertRateLimitedCandidates(t, result)
}
