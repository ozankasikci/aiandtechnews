package realphoto

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestHeadquartersList(t *testing.T) {
	want := map[string]struct {
		subject string
		queries []string
		aliases []string
	}{
		"OpenAI": {
			subject: "OpenAI headquarters, 1515 Third Street, Mission Bay, San Francisco",
			queries: []string{"1515 Third Street San Francisco", "OpenAI headquarters Mission Bay"},
		},
		"Google": {
			subject: "Google headquarters, Googleplex, Mountain View",
			queries: []string{"Googleplex", "Google headquarters Mountain View"},
			aliases: []string{"Alphabet"},
		},
		"Microsoft": {
			subject: "Microsoft headquarters, Redmond campus",
			queries: []string{"Microsoft Redmond campus", "Microsoft headquarters Redmond"},
		},
		"Amazon": {
			subject: "Amazon headquarters, Seattle",
			queries: []string{"Amazon Spheres Seattle", "Amazon headquarters Seattle"},
			aliases: []string{"Amazon Web Services", "AWS"},
		},
		"Nvidia": {
			subject: "Nvidia headquarters, Santa Clara",
			queries: []string{"Nvidia headquarters Santa Clara", "Nvidia Endeavor building"},
		},
		"Apple": {
			subject: "Apple Park, Cupertino",
			queries: []string{"Apple Park Cupertino", "Apple Park"},
		},
		"Meta": {
			subject: "Meta headquarters, Menlo Park",
			queries: []string{"Meta headquarters Menlo Park", "Facebook headquarters Menlo Park"},
			aliases: []string{"Meta Platforms", "Facebook"},
		},
		"Anthropic": {
			subject: "Anthropic headquarters, 500 Howard Street, San Francisco",
			queries: []string{"500 Howard Street San Francisco", "Anthropic headquarters"},
		},
		"Stripe": {
			subject: "Stripe headquarters, 354 Oyster Point Boulevard, South San Francisco",
			queries: []string{"Stripe headquarters Oyster Point", "354 Oyster Point South San Francisco"},
		},
		"Cloudflare": {
			subject: "Cloudflare headquarters, 101 Townsend Street, San Francisco",
			queries: []string{"Cloudflare headquarters San Francisco", "Cloudflare office"},
		},
		"SpaceX": {
			subject: "SpaceX headquarters, Starbase, Texas",
			queries: []string{"SpaceX Starbase", "Starbase Texas"},
		},
		"Tesla": {
			subject: "Tesla headquarters, Gigafactory Texas, Austin",
			queries: []string{"Gigafactory Texas", "Tesla Giga Texas"},
		},
		"Intel": {
			subject: "Intel headquarters, Robert Noyce Building, Santa Clara",
			queries: []string{"Robert Noyce Building", "Intel headquarters Santa Clara"},
		},
		"IBM": {
			subject: "IBM headquarters, Armonk, New York",
			queries: []string{"IBM headquarters Armonk", "IBM Armonk"},
		},
		"Salesforce": {
			subject: "Salesforce Tower, San Francisco",
			queries: []string{"Salesforce Tower", "Salesforce Tower San Francisco"},
		},
	}
	if len(headquarters) != len(want) {
		t.Errorf("headquarters entries = %d, want %d", len(headquarters), len(want))
	}
	seen := map[string]string{}
	for _, entry := range headquarters {
		for _, name := range append([]string{entry.name}, entry.aliases...) {
			key := strings.ToLower(strings.Join(strings.Fields(name), " "))
			if previous, ok := seen[key]; ok {
				t.Errorf("name or alias %q is shared by %q and %q", name, previous, entry.name)
			}
			seen[key] = entry.name
		}
		expected, ok := want[entry.name]
		if !ok {
			t.Errorf("unexpected headquarters entry %q", entry.name)
			continue
		}
		if entry.subject != expected.subject || len(entry.subject) > 120 {
			t.Errorf("%s subject = %q (%d bytes), want %q and at most 120 bytes", entry.name, entry.subject, len(entry.subject), expected.subject)
		}
		if !slices.Equal(entry.aliases, expected.aliases) {
			t.Errorf("%s aliases = %q, want %q", entry.name, entry.aliases, expected.aliases)
		}
		if !slices.Equal(entry.queries, expected.queries) || len(entry.queries) < 1 || len(entry.queries) > 2 {
			t.Errorf("%s queries = %q, want %q and one or two queries", entry.name, entry.queries, expected.queries)
		}
		for _, query := range entry.queries {
			if strings.TrimSpace(query) == "" {
				t.Errorf("%s has an empty query", entry.name)
			}
		}
	}
}

func TestParsePlanOverridesHeadquarters(t *testing.T) {
	const amazon = "Amazon headquarters, Seattle"
	amazonQueries := []string{"Amazon Spheres Seattle", "Amazon headquarters Seattle"}
	for _, tc := range []struct {
		subject string
		want    string
		queries []string
	}{
		{"OpenAI headquarters, 3150 18th Street, San Francisco", "OpenAI headquarters, 1515 Third Street, Mission Bay, San Francisco", []string{"1515 Third Street San Francisco", "OpenAI headquarters Mission Bay"}},
		{"Amazon Web Services headquarters, 410 Terry Ave N, Seattle", amazon, amazonQueries},
		{"AWS headquarters", amazon, amazonQueries},
		{"  aMaZoN\t WEB   services   HQ, Seattle  ", amazon, amazonQueries},
		{"SpaceX headquarters, Hawthorne, California", "SpaceX headquarters, Starbase, Texas", []string{"SpaceX Starbase", "Starbase Texas"}},
		{"Apple, Cupertino", "Apple Park, Cupertino", []string{"Apple Park Cupertino", "Apple Park"}},
		{"Alphabet campus", "Google headquarters, Googleplex, Mountain View", []string{"Googleplex", "Google headquarters Mountain View"}},
		{"Meta Platforms office", "Meta headquarters, Menlo Park", []string{"Meta headquarters Menlo Park", "Facebook headquarters Menlo Park"}},
		{"Facebook offices", "Meta headquarters, Menlo Park", []string{"Meta headquarters Menlo Park", "Facebook headquarters Menlo Park"}},
		{"IBM", "IBM headquarters, Armonk, New York", []string{"IBM headquarters Armonk", "IBM Armonk"}},
	} {
		t.Run(tc.subject, func(t *testing.T) {
			input := StandIn{Kind: StandInPlace, Subject: tc.subject, Queries: []string{"planner query"}}
			got := parseHeadquartersTestPlan(t, "funding", "Another Company", []StandIn{input})
			if len(got) != 1 || got[0].Kind != StandInPlace || got[0].Subject != tc.want || !slices.Equal(got[0].Queries, tc.queries) {
				t.Errorf("stand-ins = %+v, want place %q with queries %q", got, tc.want, tc.queries)
			}
		})
	}
}

func TestParsePlanPreservesUnmatchedStandIns(t *testing.T) {
	for _, tc := range []struct {
		kind    string
		subject string
	}{
		{StandInPlace, "Metabase headquarters"},
		{StandInPlace, "Apple Store Fifth Avenue"},
		{StandInPlace, "Federal Communications Commission headquarters, Washington"},
		{StandInPlace, "Googleplex, Mountain View"},
		{StandInPlace, "Meta headquartersAnnex"},
		{StandInPerson, "Meta"},
		{StandInProduct, "Google app on a phone"},
	} {
		t.Run(tc.subject, func(t *testing.T) {
			input := StandIn{Kind: tc.kind, Subject: tc.subject, Queries: []string{"planner query", "another planner query"}}
			got := parseHeadquartersTestPlan(t, "lawsuit", "Microsoft", []StandIn{input})
			if len(got) != 1 || got[0].Kind != input.Kind || got[0].Subject != input.Subject || !slices.Equal(got[0].Queries, input.Queries) {
				t.Errorf("stand-ins = %+v, want the original %+v", got, input)
			}
		})
	}
}

func TestParsePlanDeduplicatesHeadquartersBeforeApplyingCap(t *testing.T) {
	standIns := []StandIn{
		{Kind: StandInPlace, Subject: "Microsoft HQ, Seattle"},
		{Kind: StandInPlace, Subject: "Federal Communications Commission headquarters, Washington"},
		{Kind: StandInPlace, Subject: "Microsoft campus, Redmond"},
		{Kind: StandInPlace, Subject: "Supreme Court, Washington"},
		{Kind: StandInPlace, Subject: "Another courthouse"},
	}
	got := parseHeadquartersTestPlan(t, "policy", "Microsoft", standIns)
	var subjects []string
	for _, standIn := range got {
		subjects = append(subjects, standIn.Subject)
	}
	want := []string{"Microsoft headquarters, Redmond campus", standIns[1].Subject, standIns[3].Subject}
	if !slices.Equal(subjects, want) {
		t.Errorf("subjects = %q, want %q", subjects, want)
	}
	if got := parseHeadquartersTestPlan(t, "research", "Microsoft", standIns); len(got) != 0 {
		t.Errorf("research stand-ins = %+v, want none", got)
	}
}

func TestParsePlanKeepsFirstPlaceWithSameSubject(t *testing.T) {
	const subject = "Supreme Court, Washington"
	first := StandIn{Kind: StandInPlace, Subject: subject, Queries: []string{"first query"}}
	second := StandIn{Kind: StandInPlace, Subject: subject, Queries: []string{"second query"}}
	got := parseHeadquartersTestPlan(t, "policy", "", []StandIn{first, second})
	if len(got) != 1 || got[0].Subject != first.Subject || !slices.Equal(got[0].Queries, first.Queries) {
		t.Errorf("stand-ins = %+v, want only the first %+v", got, first)
	}
}

func parseHeadquartersTestPlan(t *testing.T, storyType, maker string, standIns []StandIn) []StandIn {
	t.Helper()
	raw, err := json.Marshal(Plan{StoryType: storyType, Maker: maker, StandIns: standIns})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ParsePlan(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	return plan.StandIns
}
