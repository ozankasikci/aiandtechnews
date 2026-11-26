package realphoto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// maxPlanText caps how much article text the planner reads.
const maxPlanText = 8000

// Plan is the planner's reading of the article: is there one concrete,
// photographable subject, and how to look for it.
type Plan struct {
	Photographable bool     `json:"photographable"`
	StoryType      string   `json:"story_type"`
	Subject        string   `json:"subject"`
	Kind           string   `json:"kind,omitempty"`
	Queries        []string `json:"queries"`
	Maker          string   `json:"maker"`
	// MakerDomain and OfficialPages point at the maker's own site when the
	// source page does not link to it: used only after checks (see
	// ParsePlan and Finder.official).
	MakerDomain   string   `json:"maker_domain,omitempty"`
	OfficialPages []string `json:"official_pages,omitempty"`
	// StandIns are real things a photo could honestly show in place of a
	// story with nothing physical in it. Only the featured slot uses them.
	StandIns []StandIn `json:"stand_ins,omitempty"`
	Reason   string    `json:"reason,omitempty"`
}

// StandIn is one stand-in subject and how to search Commons for it.
type StandIn struct {
	Subject string   `json:"subject"`
	Kind    string   `json:"kind"`
	Queries []string `json:"queries"`

	headquartersSince int // set only from the headquarters list
}

// Kinds of stand-in.
const (
	StandInPerson  = KindPerson
	StandInPlace   = "place"
	StandInProduct = "product_in_use"
	// MaxStandIns caps the stand-ins kept from one plan.
	MaxStandIns = 3
)

var standInKinds = []string{StandInPerson, StandInPlace, StandInProduct}

const (
	storyTypeResearch = "research"
	storyTypeOther    = "other"
)

var corporateStoryTypes = []string{"funding", "leadership_change", "earnings", "acquisition", "lawsuit"}
var otherStoryTypes = []string{"product", storyTypeResearch, "policy", storyTypeOther}

const planPrompt = `You pick the in-article photo for a technology news article. Decide whether the story has ONE concrete, photographable subject that a real photo could show.

Photographable: a specific physical product or device (including brand-new hardware that has just been announced), a robot, a vehicle, a chip or server you can hold, a building or data centre, a place, a physical event (a launch, a keynote, a trade show), or a person who is the story's central actor (the one acting or announcing, not someone merely quoted).
Not photographable: software, apps, AI models, websites, features, policies, laws, lawsuits, studies, reports, funding rounds, earnings, prices, market trends, abstract topics, or stories about many different things.

Return only JSON with exactly this shape:
{"photographable":true,"story_type":"funding|leadership_change|earnings|acquisition|lawsuit|product|research|policy|other","subject":"...","kind":"product|device|robot|vehicle|building|place|event|person|none","queries":["...","..."],"maker":"...","maker_domain":"...","official_pages":["..."],"reason":"...","stand_ins":[{"subject":"...","kind":"person|place|product_in_use","queries":["...","..."]}]}

story_type: always choose one based on the main news in the story:
- "funding": a company raises or secures investment.
- "leadership_change": an executive is appointed, replaced or leaves.
- "earnings": a company reports its financial results.
- "acquisition": a company buys, sells or merges with another business.
- "lawsuit": a company files or faces a lawsuit, ruling or settlement.
- "product": a launch, rollout, feature, update, pricing change or availability change of a product or model.
- "research": a study, experiment or research finding.
- "policy": a law, regulation or policy decision.
- "other": anything else.
subject: the exact subject, as specific as possible (maker, product name and model, e.g. "WiCi One desk robot"); empty when not photographable. For kind "person", use the person's full name and nothing else.
queries: two short Wikimedia Commons search queries for a photo of that exact subject, the most specific first (e.g. "WiCi One robot", "WiCi robot"). Plain words, no quotes or operators.
maker: the company or organisation that makes or owns the subject (for a person, their organisation). When photographable is false, use the company or organisation the story is about. Leave empty if unknown.
maker_domain: the maker's official website domain (e.g. "nvidia.com"), only if you are sure; empty otherwise.
official_pages: up to 2 full https URLs on maker_domain for this exact subject: its product page or the maker's own announcement. Only URLs you are confident exist; [] otherwise, and always [] for a person.
reason: one short sentence.
stand_ins: [] when photographable is true. When it is false, up to three real things a photo could honestly show in place of the subject, the most fitting first, or [] if none is honest:
- "person": the one clear leader of the company the story is about (its chief executive or founder), only when the story is a corporate action by that company: funding, a leadership change, earnings, an acquisition, a lawsuit. subject is the person's full name and nothing else.
- "place": the current main headquarters of the company, regulator or court the story is about. subject names the organisation, the building or campus name or street address when known, and the city, e.g. "Nvidia headquarters, Santa Clara" or "OpenAI headquarters, 1515 Third Street, San Francisco". The first query names the building, campus or city so the search targets the specific site.
- "product_in_use": a software product people see on a device, when the story is about that product. subject says what the photo shows, e.g. "ChatGPT app on a smartphone".
Never offer a stand-in for a study, a market trend, or a story about several companies at once. Each stand-in has two short Wikimedia Commons search queries, written like queries above.

Headline: %s

Article:
%s`

var (
	leadingFence  = regexp.MustCompile("^```(?:json)?\\s*")
	trailingFence = regexp.MustCompile("\\s*```$")
	jsonObject    = regexp.MustCompile(`(?s)\{.*\}`)
)

// cleanJSON strips code fences and text around the JSON object.
func cleanJSON(raw string) string {
	cleaned := strings.TrimSpace(trailingFence.ReplaceAllString(leadingFence.ReplaceAllString(strings.TrimSpace(raw), ""), ""))
	if match := jsonObject.FindString(cleaned); match != "" {
		return match
	}
	return cleaned
}

func (f *Finder) plan(ctx context.Context, request Request) (Plan, error) {
	text := strings.TrimSpace(request.Text)
	if len(text) > maxPlanText {
		text = text[:maxPlanText]
	}
	raw, err := f.text.GenerateJSON(ctx, fmt.Sprintf(planPrompt, request.Title, text))
	if err != nil {
		return Plan{}, err
	}
	return ParsePlan(raw)
}

// ParsePlan reads the planner's answer. A photographable plan needs a
// subject and at least one query.
func ParsePlan(raw string) (Plan, error) {
	var plan Plan
	if err := json.Unmarshal([]byte(cleanJSON(raw)), &plan); err != nil {
		return Plan{}, fmt.Errorf("unparsable plan: %w", err)
	}
	plan.Subject = oneLine(plan.Subject, 120)
	plan.Kind = strings.ToLower(strings.TrimSpace(plan.Kind))
	plan.Maker = oneLine(plan.Maker, 80)
	plan.Reason = oneLine(plan.Reason, 200)
	plan.Queries = cleanQueries(plan.Queries)
	plan.StoryType = strings.ToLower(strings.TrimSpace(plan.StoryType))
	corporate := slices.Contains(corporateStoryTypes, plan.StoryType)
	if !corporate && !slices.Contains(otherStoryTypes, plan.StoryType) {
		plan.StoryType = storyTypeOther
	}
	if plan.Photographable || plan.StoryType == storyTypeResearch {
		plan.StandIns = nil
	} else {
		plan.StandIns = cleanStandIns(plan.StandIns, corporate)
	}
	plan.MakerDomain = strings.TrimPrefix(strings.ToLower(oneLine(plan.MakerDomain, 80)), "www.")
	var pages []string
	for _, page := range plan.OfficialPages {
		if len(pages) < 2 && onMakerDomain(page, plan.MakerDomain) {
			pages = append(pages, page)
		}
	}
	plan.OfficialPages = pages
	if plan.Photographable && plan.Subject == "" {
		return Plan{}, errors.New("photographable plan without a subject")
	}
	if plan.Photographable && len(plan.Queries) == 0 {
		plan.Queries = []string{plan.Subject}
	}
	return plan, nil
}

// cleanQueries keeps the first two non-empty search queries, without quotes
// or operators.
func cleanQueries(raw []string) []string {
	var queries []string
	for _, query := range raw {
		if query = oneLine(strings.NewReplacer(`"`, "", "|", " ").Replace(query), 100); query != "" {
			queries = append(queries, query)
		}
		if len(queries) == 2 {
			break
		}
	}
	return queries
}

// cleanStandIns keeps at most MaxStandIns stand-ins that have a subject and a
// known kind. It resolves listed headquarters and keeps the first place for
// each subject. A stand-in without a query searches for its subject.
func cleanStandIns(raw []StandIn, allowPerson bool) []StandIn {
	var kept []StandIn
	for _, standIn := range raw {
		standIn.Subject = oneLine(standIn.Subject, 120)
		standIn.Kind = strings.ToLower(strings.TrimSpace(standIn.Kind))
		if standIn.Subject == "" || !slices.Contains(standInKinds, standIn.Kind) {
			continue
		}
		if standIn.Kind == StandInPerson && !allowPerson {
			continue
		}
		if standIn.Queries = cleanQueries(standIn.Queries); len(standIn.Queries) == 0 {
			standIn.Queries = []string{standIn.Subject}
		}
		if standIn.Kind == StandInPlace {
			if entry, ok := headquartersForSubject(standIn.Subject); ok {
				standIn.Subject = entry.subject
				standIn.Queries = slices.Clone(entry.queries)
				standIn.headquartersSince = entry.since
			}
			if slices.ContainsFunc(kept, func(other StandIn) bool {
				return other.Kind == StandInPlace && other.Subject == standIn.Subject
			}) {
				continue
			}
		}
		kept = append(kept, standIn)
		if len(kept) == MaxStandIns {
			break
		}
	}
	return kept
}

// onMakerDomain reports whether page is an https URL on the maker's own
// registrable domain, and that domain can be a maker's official site.
func onMakerDomain(page, makerDomain string) bool {
	parsed, err := url.Parse(strings.TrimSpace(page))
	if err != nil || parsed.Scheme != "https" || makerDomain == "" {
		return false
	}
	domain := registrableDomain("https://" + makerDomain)
	return domain != "" && registrableDomain(page) == domain && !blockedSite(page)
}

// oneLine collapses whitespace and cuts to at most limit bytes on a word
// boundary.
func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= limit {
		return text
	}
	cut := text[:limit]
	if space := strings.LastIndex(cut, " "); space > limit/2 {
		cut = cut[:space]
	}
	return strings.TrimRight(cut, " ,;:-") + "…"
}
