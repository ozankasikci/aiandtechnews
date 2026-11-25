package realphoto_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

func TestFindSkipsRecentlyUsedStandInsBeforeSearch(t *testing.T) {
	const plan = `{"photographable":false,"story_type":"product","maker":"Example Labs","stand_ins":[
		{"subject":"Example Labs headquarters","kind":"place"},
		{"subject":"Example app on a phone","kind":"product_in_use"}]}`
	const placeQuery = "filetype:bitmap Example Labs headquarters"
	const appQuery = "filetype:bitmap Example app on a phone"
	for _, tc := range []struct {
		name        string
		recent      []realphoto.FeaturedSubjectUse
		wantQueries []string
		wantSubject string
	}{
		{"same kind and maker", []realphoto.FeaturedSubjectUse{
			{Subject: "Another office", Kind: "place", Maker: " eXample	 Labs "},
		}, []string{appQuery}, "Example app on a phone"},
		{"same kind and subject", []realphoto.FeaturedSubjectUse{
			{Subject: " example labs  HEADQUARTERS ", Kind: "place", Maker: "Another Labs"},
		}, []string{appQuery}, "Example app on a phone"},
		{"none left", []realphoto.FeaturedSubjectUse{
			{Kind: "place", Maker: "example labs"},
			{Kind: "product_in_use", Maker: "example labs"},
		}, nil, ""},
		{"another kind", []realphoto.FeaturedSubjectUse{
			{Subject: "Example Labs headquarters", Kind: "person", Maker: "example labs"},
		}, []string{placeQuery}, "Example Labs headquarters"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var queries []string
			vision := &fakeVision{verdicts: map[string]string{"green": verdict(9, ""), "blue": verdict(9, "")}}
			finder := realphoto.NewFinder(&fakeText{plan: plan}, vision, &http.Client{Transport: twoFiles(t, &queries)}, nil)
			result := finder.Find(context.Background(), realphoto.Request{Slot: realphoto.SlotFeatured, RecentFeaturedSubjects: tc.recent})
			if !slices.Equal(queries, tc.wantQueries) {
				t.Fatalf("queries = %v, want %v", queries, tc.wantQueries)
			}
			if tc.wantSubject == "" {
				if result.Found || result.Transient || vision.calls != 0 ||
					!strings.Contains(result.Reason, "Example Labs headquarters") ||
					!strings.Contains(result.Reason, "Example app on a phone") ||
					!strings.Contains(result.Reason, "skipped as recently used") {
					t.Fatalf("result = %+v, vision calls %d, want an ordinary miss with skip reasons", result, vision.calls)
				}
				return
			}
			if !result.Found || result.StandIn == nil || result.StandIn.Subject != tc.wantSubject {
				t.Fatalf("result = %+v, want %q", result, tc.wantSubject)
			}
		})
	}
}

func TestRecentFeaturedSubjectsDoNotMatchEmptyMakersOrLimitExactSubjects(t *testing.T) {
	const standIn = `{"photographable":false,"story_type":"product","stand_ins":[{"subject":"A headquarters","kind":"place"}]}`
	const exact = `{"photographable":true,"subject":"A headquarters","kind":"place","maker":"Example"}`
	for _, tc := range []struct {
		name, plan string
		recent     realphoto.FeaturedSubjectUse
	}{
		{"empty makers", standIn, realphoto.FeaturedSubjectUse{Subject: "Another headquarters", Kind: "place", Maker: " 	"}},
		{"exact subject", exact, realphoto.FeaturedSubjectUse{Subject: "A headquarters", Kind: "place", Maker: "Example"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var queries []string
			vision := &fakeVision{verdicts: map[string]string{"green": verdict(9, ""), "blue": verdict(9, "")}}
			finder := realphoto.NewFinder(&fakeText{plan: tc.plan}, vision, &http.Client{Transport: twoFiles(t, &queries)}, nil)
			result := finder.Find(context.Background(), realphoto.Request{
				Slot: realphoto.SlotFeatured, RecentFeaturedSubjects: []realphoto.FeaturedSubjectUse{tc.recent},
			})
			if !result.Found || !slices.Equal(queries, []string{"filetype:bitmap A headquarters"}) {
				t.Fatalf("result = %+v, queries = %v, want a photo", result, queries)
			}
			if tc.plan == exact && result.StandIn != nil {
				t.Fatalf("exact photo reported as a stand-in: %+v", result.StandIn)
			}
		})
	}
}

func TestFindMatchesStandInMakersByWholeWordPrefix(t *testing.T) {
	for _, tc := range []struct {
		name, maker, recentMaker, recentKind string
		skip                                 bool
	}{
		{"parent used", "Amazon Web Services", " Amazon ", "place", true},
		{"division used", "Amazon", "AMAZON\t Web  Services", "place", true},
		{"another parent", "Google DeepMind", "Google", "place", true},
		{"Meta and Metabase", "Meta", "Metabase", "place", false},
		{"Open and OpenAI", "OpenAI", "Open", "place", false},
		{"empty maker", "  ", "Amazon", "place", false},
		{"empty recent maker", "Amazon", " \t ", "place", false},
		{"different kind", "Amazon Web Services", "Amazon", "product_in_use", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := fmt.Sprintf(`{"photographable":false,"story_type":"product","maker":%q,
				"stand_ins":[{"subject":"Current headquarters","kind":"place"}]}`, tc.maker)
			var queries []string
			vision := &fakeVision{verdicts: map[string]string{"green": verdict(9, ""), "blue": verdict(9, "")}}
			finder := realphoto.NewFinder(&fakeText{plan: plan}, vision, &http.Client{Transport: twoFiles(t, &queries)}, nil)
			result := finder.Find(context.Background(), realphoto.Request{
				Slot:                   realphoto.SlotFeatured,
				RecentFeaturedSubjects: []realphoto.FeaturedSubjectUse{{Subject: "Prior headquarters", Kind: tc.recentKind, Maker: tc.recentMaker}},
			})
			if tc.skip {
				if result.Found || result.Transient || len(queries) != 0 || !strings.Contains(result.Reason, "skipped as recently used") {
					t.Fatalf("found = %t, transient = %t, reason = %q, queries = %v, want a skip before searching", result.Found, result.Transient, result.Reason, queries)
				}
			} else if !result.Found || !slices.Equal(queries, []string{"filetype:bitmap Current headquarters"}) {
				t.Fatalf("found = %t, queries = %v, want a headquarters photo", result.Found, queries)
			}
		})
	}
}

func TestFindUsesExactPersonHistoryOnlyForLaterStandIns(t *testing.T) {
	const exact = `{"photographable":true,"subject":"Bret Taylor","kind":"person","maker":"Sierra"}`
	const withFallback = `{"photographable":false,"story_type":"funding","maker":"Sierra","stand_ins":[
		{"subject":"Another Leader","kind":"person"},{"subject":"Sierra headquarters","kind":"place"}]}`
	const bySubject = `{"photographable":false,"story_type":"funding","maker":"Another company","stand_ins":[
		{"subject":"Bret Taylor","kind":"person"}]}`
	const personQuery = "filetype:bitmap Bret Taylor"
	var queries []string
	model := &fakeText{plan: exact}
	vision := &fakeVision{verdicts: map[string]string{"green": verdict(8, ""), "blue": verdict(9, "")}}
	finder := realphoto.NewFinder(model, vision, &http.Client{Transport: twoFiles(t, &queries)}, nil).WithClock(fixedPhotoTime)
	ctx := context.Background()
	request := realphoto.Request{Slot: realphoto.SlotFeatured, Slug: "first"}
	first := finder.Find(ctx, request)
	if !first.Found || first.StandIn != nil || !slices.Equal(queries, []string{personQuery}) {
		t.Fatalf("first photo: found = %t, stand-in = %v, queries = %v", first.Found, first.StandIn, queries)
	}
	use := first.FeaturedSubjectUse()
	wantUse := realphoto.FeaturedSubjectUse{Subject: "Bret Taylor", Kind: realphoto.KindPerson, Maker: "Sierra"}
	if use == nil || *use != wantUse {
		t.Errorf("recorded use = %+v, want %+v", use, wantUse)
	}
	if use != nil {
		request.RecentFeaturedSubjects = []realphoto.FeaturedSubjectUse{*use}
	}
	request.Exclude = []string{first.Chosen.ImageURL}

	for _, tc := range []struct {
		name, plan, wantStandIn string
		wantQueries             []string
		miss                    bool
	}{
		{"same subject", bySubject, "", nil, true},
		{"same maker then next stand-in", withFallback, "Sierra headquarters", []string{"filetype:bitmap Sierra headquarters"}, false},
		{"same exact subject again", exact, "", []string{personQuery}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queries = nil
			model.plan = tc.plan
			request.Slug = tc.name
			result := finder.Find(ctx, request)
			if !slices.Equal(queries, tc.wantQueries) || result.Found == tc.miss || result.Transient {
				t.Fatalf("found = %t, transient = %t, reason = %q, queries = %v, want queries %v", result.Found, result.Transient, result.Reason, queries, tc.wantQueries)
			}
			if tc.miss && !strings.Contains(result.Reason, "stand-in Bret Taylor skipped as recently used") {
				t.Errorf("miss reason = %q, want a recent-use skip", result.Reason)
			}
			if tc.wantStandIn != "" && (result.StandIn == nil || result.StandIn.Subject != tc.wantStandIn) {
				t.Errorf("stand-in = %+v, want %q", result.StandIn, tc.wantStandIn)
			}
			if tc.plan == exact && (result.StandIn != nil || result.Chosen.ImageURL == first.Chosen.ImageURL) {
				t.Errorf("repeat exact subject did not get a different exact photo")
			}
		})
	}
}
