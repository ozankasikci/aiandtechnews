package realphoto_test

import (
	"context"
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
		recent      []realphoto.StandInUse
		wantQueries []string
		wantSubject string
	}{
		{"same kind and maker", []realphoto.StandInUse{
			{Subject: "Another office", Kind: "place", Maker: " eXample	 Labs "},
		}, []string{appQuery}, "Example app on a phone"},
		{"same kind and subject", []realphoto.StandInUse{
			{Subject: " example labs  HEADQUARTERS ", Kind: "place", Maker: "Another Labs"},
		}, []string{appQuery}, "Example app on a phone"},
		{"none left", []realphoto.StandInUse{
			{Kind: "place", Maker: "example labs"},
			{Kind: "product_in_use", Maker: "example labs"},
		}, nil, ""},
		{"another kind", []realphoto.StandInUse{
			{Subject: "Example Labs headquarters", Kind: "person", Maker: "example labs"},
		}, []string{placeQuery}, "Example Labs headquarters"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var queries []string
			vision := &fakeVision{verdicts: map[string]string{"green": verdict(9, ""), "blue": verdict(9, "")}}
			finder := realphoto.NewFinder(&fakeText{plan: plan}, vision, &http.Client{Transport: twoFiles(t, &queries)}, nil)
			result := finder.Find(context.Background(), realphoto.Request{Slot: realphoto.SlotFeatured, RecentStandIns: tc.recent})
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

func TestRecentStandInsDoNotMatchEmptyMakersOrLimitExactSubjects(t *testing.T) {
	const standIn = `{"photographable":false,"story_type":"product","stand_ins":[{"subject":"A headquarters","kind":"place"}]}`
	const exact = `{"photographable":true,"subject":"A headquarters","kind":"place","maker":"Example"}`
	for _, tc := range []struct {
		name, plan string
		recent     realphoto.StandInUse
	}{
		{"empty makers", standIn, realphoto.StandInUse{Subject: "Another headquarters", Kind: "place", Maker: " 	"}},
		{"exact subject", exact, realphoto.StandInUse{Subject: "A headquarters", Kind: "place", Maker: "Example"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var queries []string
			vision := &fakeVision{verdicts: map[string]string{"green": verdict(9, ""), "blue": verdict(9, "")}}
			finder := realphoto.NewFinder(&fakeText{plan: tc.plan}, vision, &http.Client{Transport: twoFiles(t, &queries)}, nil)
			result := finder.Find(context.Background(), realphoto.Request{
				Slot: realphoto.SlotFeatured, RecentStandIns: []realphoto.StandInUse{tc.recent},
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
