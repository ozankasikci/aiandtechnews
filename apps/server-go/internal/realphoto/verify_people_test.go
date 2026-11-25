package realphoto

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestPeopleProminentVerdictContract(t *testing.T) {
	prompt := VerifyPrompt(Plan{Kind: "product"}, "Headline", KindCommons)
	for _, want := range []string{"\npeople_prominent:", `"people_prominent":false`} {
		if !strings.Contains(prompt, want) {
			t.Errorf("vision prompt lacks %q", want)
		}
	}
	properties := verdictSchema["properties"].(map[string]any)
	field, ok := properties["people_prominent"].(map[string]any)
	if !ok || field["type"] != "BOOLEAN" {
		t.Errorf("schema property people_prominent = %v, want BOOLEAN", properties["people_prominent"])
	}
	if !slices.Contains(verdictSchema["required"].([]string), "people_prominent") {
		t.Error("schema does not require people_prominent")
	}

	for _, tc := range []struct {
		name, field string
		want        bool
	}{
		{"true", `,"people_prominent":true`, true},
		{"false", `,"people_prominent":false`, false},
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
			if got := fields["people_prominent"]; got != tc.want {
				t.Errorf("people_prominent = %v, want %t", got, tc.want)
			}
		})
	}
}

func TestUsableForRejectsProminentPeopleOnlyForBuildingsAndPlaces(t *testing.T) {
	const exact = `{"match":"exact","photographic":true,"subject_prominent":true,"quality":9,`
	people, err := ParseVerdict(exact + `"people_prominent":true}`)
	if err != nil {
		t.Fatal(err)
	}
	incidental, err := ParseVerdict(exact + `"people_prominent":false}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, slot := range []string{SlotFeatured, SlotInline} {
		for _, kind := range []string{"place", "building", "person", "event", "product", "device", "robot", "vehicle", "product_in_use"} {
			t.Run(slot+"/"+kind, func(t *testing.T) {
				if ok, why := incidental.usableFor(slot, kind); !ok || why != "" {
					t.Errorf("incidental people: usable = %t, reason = %q, want usable", ok, why)
				}
				ok, why := people.usableFor(slot, kind)
				if kind == "place" || kind == "building" {
					if ok || !strings.Contains(why, "people") {
						t.Errorf("prominent people: usable = %t, reason = %q, want rejection naming people", ok, why)
					}
				} else if !ok || why != "" {
					t.Errorf("prominent people: usable = %t, reason = %q, want usable", ok, why)
				}
			})
		}
	}
}

func TestVerifyPromptRequiresShowingBuildingsAndPlacesThemselves(t *testing.T) {
	const placeRule = "the picture must show the building or place itself"
	for _, source := range []string{KindCommons, KindOfficial} {
		for _, kind := range []string{"place", "building", "person", "product"} {
			t.Run(source+"/"+kind, func(t *testing.T) {
				prompt := VerifyPromptFor(Plan{Kind: kind}, "Headline", source, SlotFeatured)
				_, tail, found := strings.Cut(prompt, placeRule)
				if kind == "place" || kind == "building" {
					line, _, _ := strings.Cut(tail, "\n")
					if !found || !strings.Contains(line, "architecture is the subject") ||
						!strings.Contains(line, "A photo of people taken at or inside it is not an exact match") {
						t.Errorf("vision prompt lacks the building or place rule on one line:\n%s", prompt)
					}
				} else if found {
					t.Errorf("vision prompt for %s includes the building or place rule", kind)
				}
			})
		}
	}
}
