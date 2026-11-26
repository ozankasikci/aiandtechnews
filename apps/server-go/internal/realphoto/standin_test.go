package realphoto_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

const fundingPlan = `{"photographable":false,"story_type":"funding","subject":"","kind":"none","queries":[],"maker":"Sierra","reason":"A funding round.",
"stand_ins":[{"subject":"Bret Taylor","kind":"person","queries":["Bret Taylor","Bret Taylor Sierra"]}]}`

func TestParsePlanCleansStandIns(t *testing.T) {
	plan, err := realphoto.ParsePlan(`{"photographable":false,"story_type":"funding","reason":"r","stand_ins":[
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
	finder := realphoto.NewFinder(&fakeText{plan: fundingPlan}, vision, &http.Client{Transport: twoFiles(t, &queries)}, nil).WithClock(fixedPhotoTime)
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

func TestFindRejectsProminentPeopleInAPlaceStandIn(t *testing.T) {
	const plan = `{"photographable":false,"story_type":"funding","maker":"Anthropic",
		"stand_ins":[{"subject":"Anthropic headquarters","kind":"place"}]}`
	var queries []string
	routes := sites{
		"commons.wikimedia.org": commonsAPI(t, []commonsFile{
			{title: "File:Secretary of State Peter Kyle visits Anthropic.jpg", date: "2025", license: "CC0", width: 1600, height: 900, thumb: "https://upload.wikimedia.org/visit.png"},
		}, &queries),
		"upload.wikimedia.org": serveBytes(solidPNG(t, 1600, 900, green), "image/png"),
	}
	vision := &fakeVision{verdicts: map[string]string{"green": `{"match":"exact","photographic":true,
		"subject_prominent":true,"people_prominent":true,"quality":9,"alt":"Two people shake hands in a lobby."}`}}
	finder := realphoto.NewFinder(&fakeText{plan: plan}, vision, &http.Client{Transport: routes}, nil).WithClock(fixedPhotoTime)
	result := finder.Find(context.Background(), realphoto.Request{Title: "Anthropic raises funding", Slot: realphoto.SlotFeatured})

	if result.Found || result.StandIn != nil || result.Transient || result.Reason != "no usable photo of a stand-in subject" {
		t.Errorf("found = %t, stand-in = %v, transient = %t, reason = %q, want an ordinary miss with no stand-in",
			result.Found, result.StandIn, result.Transient, result.Reason)
	}
	if vision.calls != 1 || len(result.Candidates) != 1 {
		t.Fatalf("vision calls = %d, candidate count = %d, want one reviewed candidate", vision.calls, len(result.Candidates))
	}
	candidate := result.Candidates[0]
	if candidate.Usable || !strings.Contains(candidate.Reason, "people") {
		t.Errorf("candidate usable = %t, reason = %q, want rejection naming people", candidate.Usable, candidate.Reason)
	}
}

func TestFindStopsSearchingStandInsAfterAnImageDownloadIsRateLimited(t *testing.T) {
	const plan = `{"photographable":false,"story_type":"funding","maker":"Sierra","stand_ins":[
		{"subject":"Bret Taylor","kind":"person"},
		{"subject":"Sierra headquarters","kind":"place"}]}`
	var queries []string
	requests := 0
	routes := twoFiles(t, &queries)
	routes["upload.wikimedia.org"] = func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}
	finder := realphoto.NewFinder(&fakeText{plan: plan}, &fakeVision{}, &http.Client{Transport: routes}, nil).WithClock(fixedPhotoTime)
	result := finder.Find(context.Background(), realphoto.Request{Title: "Sierra raises funding", Slot: realphoto.SlotFeatured})

	if len(queries) != 1 || requests != 1 {
		t.Fatalf("queries = %v, image downloads = %d, want one of each", queries, requests)
	}
	if result.StandIn != nil || !strings.Contains(strings.Join(result.Notes, " "), "rate limited") {
		t.Fatalf("result = %+v, want no chosen stand-in and a rate-limit note", result)
	}
	assertRateLimitedCandidates(t, result)
}

func TestParsePlanGatesPersonStandInsByStoryType(t *testing.T) {
	for _, tc := range []struct {
		name, field, want string
		person            bool
	}{
		{"funding", `"story_type":"funding",`, "funding", true},
		{"leadership change", `"story_type":"leadership_change",`, "leadership_change", true},
		{"earnings", `"story_type":"earnings",`, "earnings", true},
		{"acquisition", `"story_type":"acquisition",`, "acquisition", true},
		{"lawsuit", `"story_type":"lawsuit",`, "lawsuit", true},
		{"product", `"story_type":"product",`, "product", false},
		{"research", `"story_type":" ReSeArCh ",`, "research", false},
		{"policy", `"story_type":"policy",`, "policy", false},
		{"other", `"story_type":"other",`, "other", false},
		{"missing", "", "other", false},
		{"unknown", `"story_type":"partnership",`, "other", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := realphoto.ParsePlan(fmt.Sprintf(`{%s"photographable":false,"stand_ins":[
				{"subject":"Bret Taylor","kind":"person"},
				{"subject":"Sierra headquarters","kind":"place"},
				{"subject":"Sierra app on a phone","kind":"product_in_use"}]}`, tc.field))
			if err != nil {
				t.Fatal(err)
			}
			if plan.StoryType != tc.want {
				t.Errorf("story type = %q, want %q", plan.StoryType, tc.want)
			}
			want := "place,product_in_use"
			if tc.person {
				want = "person," + want
			}
			if tc.want == "research" {
				want = ""
			}
			var kinds []string
			for _, standIn := range plan.StandIns {
				kinds = append(kinds, standIn.Kind)
			}
			if got := strings.Join(kinds, ","); got != want {
				t.Errorf("stand-in kinds = %q, want %q", got, want)
			}
		})
	}
}

func TestParsePlanKeepsAPhotographableResearchSubject(t *testing.T) {
	plan, err := realphoto.ParsePlan(`{"photographable":true,"story_type":"research","kind":"robot",
		"subject":"Lab robot","stand_ins":[{"subject":"Lab headquarters","kind":"place"}]}`)
	if err != nil || !plan.Photographable || plan.StoryType != "research" || plan.Kind != "robot" || plan.Subject != "Lab robot" ||
		len(plan.Queries) != 1 || plan.Queries[0] != plan.Subject || len(plan.StandIns) != 0 {
		t.Fatalf("plan = %+v, err = %v, want an exact robot photo plan", plan, err)
	}
}

func TestParsePlanKeepsAnExactPersonSubjectForAProductStory(t *testing.T) {
	plan, err := realphoto.ParsePlan(`{"photographable":true,"story_type":"product","kind":"person","subject":"Bret Taylor"}`)
	if err != nil || !plan.Photographable || plan.Subject != "Bret Taylor" || plan.Kind != realphoto.KindPerson {
		t.Fatalf("plan = %+v, err = %v", plan, err)
	}
}
