package realphoto

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestPlanNamesTheCurrentHeadquarters(t *testing.T) {
	const subject = "OpenAI headquarters, 1515 Third Street, San Francisco"
	_, rules, ok := strings.Cut(planPrompt, `- "place":`)
	if !ok {
		t.Fatal("planner prompt has no place stand-in rule")
	}
	rule, _, _ := strings.Cut(rules, "\n")
	for _, want := range []string{
		"current main headquarters",
		"the organisation, the building or campus name or street address when known, and the city",
		subject,
		"The first query names the building, campus or city",
	} {
		if !strings.Contains(rule, want) {
			t.Errorf("place stand-in rule lacks %q", want)
		}
	}

	plan, err := ParsePlan(`{"photographable":false,"story_type":"funding","stand_ins":[{"kind":"place","subject":"` + subject + `","queries":["1515 Third Street San Francisco"]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.StandIns) != 1 || plan.StandIns[0].Kind != StandInPlace || plan.StandIns[0].Subject != subject {
		t.Fatalf("stand-ins = %+v, want a place stand-in with subject %q", plan.StandIns, subject)
	}
}

func TestOtherSiteVerdictContract(t *testing.T) {
	prompt := VerifyPrompt(Plan{Kind: StandInPlace}, "Headline", KindCommons)
	_, rules, _ := strings.Cut(prompt, "\nother_site: ")
	rule, _, _ := strings.Cut(rules, "\n")
	for _, want := range []string{
		"file title, description or the image itself",
		"different site than the subject names",
		"former office", "regional, branch or overseas office", "data centre", "shop", "another city",
		"False when they show the named site or give no sign of a different one",
	} {
		if !strings.Contains(rule, want) {
			t.Errorf("other_site rule lacks %q", want)
		}
	}
	if !strings.Contains(prompt, `"other_site":false`) {
		t.Error("vision prompt JSON example lacks other_site")
	}
	properties := verdictSchema["properties"].(map[string]any)
	field, ok := properties["other_site"].(map[string]any)
	if !ok || field["type"] != "BOOLEAN" {
		t.Errorf("schema property other_site = %v, want BOOLEAN", properties["other_site"])
	}
	if !slices.Contains(verdictSchema["required"].([]string), "other_site") {
		t.Error("schema does not require other_site")
	}

	for _, tc := range []struct {
		name, field string
		want        bool
	}{
		{"true", `,"other_site":true`, true},
		{"false", `,"other_site":false`, false},
		{"omitted", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseVerdict(`{"match":"exact","quality":9` + tc.field + `}`)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if got := fields["other_site"]; got != tc.want {
				t.Errorf("other_site = %v, want %t", got, tc.want)
			}
		})
	}
}

func TestVerifyPromptRequiresTheNamedSiteForBuildingsAndPlaces(t *testing.T) {
	const siteRule = "The photo must show the site the subject names. Another site of the same organisation is not an exact match."
	for _, source := range []string{KindCommons, KindOfficial} {
		for _, kind := range []string{StandInPlace, "building", KindPerson, "product"} {
			t.Run(source+"/"+kind, func(t *testing.T) {
				prompt := VerifyPromptFor(Plan{Kind: kind}, "Headline", source, SlotFeatured)
				context, _, _ := strings.Cut(prompt, "\nJudge the image:")
				want := kind == StandInPlace || kind == "building"
				if found := slices.Contains(strings.Split(context, "\n"), siteRule); found != want {
					t.Errorf("vision prompt context contains the named-site line = %t, want %t", found, want)
				}
			})
		}
	}
}

func TestUsableForRejectsOtherSitesOnlyForBuildingsAndPlaces(t *testing.T) {
	const exact = `{"match":"exact","photographic":true,"subject_prominent":true,"quality":9,`
	otherSite, err := ParseVerdict(exact + `"other_site":true}`)
	if err != nil {
		t.Fatal(err)
	}
	namedSite, err := ParseVerdict(exact + `"other_site":false}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, slot := range []string{SlotFeatured, SlotInline} {
		for _, kind := range []string{StandInPlace, "building", KindPerson, "event", "product", StandInProduct} {
			t.Run(slot+"/"+kind, func(t *testing.T) {
				if ok, why := namedSite.usableFor(slot, kind); !ok || why != "" {
					t.Errorf("named site: usable = %t, reason = %q, want usable", ok, why)
				}
				ok, why := otherSite.usableFor(slot, kind)
				if kind == StandInPlace || kind == "building" {
					if ok || !strings.Contains(why, "different site") {
						t.Errorf("other site: usable = %t, reason = %q, want rejection naming a different site", ok, why)
					}
				} else if !ok || why != "" {
					t.Errorf("other site: usable = %t, reason = %q, want usable", ok, why)
				}
			})
		}
	}
}
