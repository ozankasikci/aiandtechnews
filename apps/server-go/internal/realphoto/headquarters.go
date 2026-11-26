package realphoto

import "strings"

type headquartersEntry struct {
	name    string
	aliases []string
	subject string
	queries []string
}

var headquarters = []headquartersEntry{
	{
		name:    "OpenAI",
		subject: "OpenAI headquarters, 1515 Third Street, Mission Bay, San Francisco",
		queries: []string{"1515 Third Street San Francisco", "OpenAI headquarters Mission Bay"},
	},
	{
		name:    "Google",
		aliases: []string{"Alphabet"},
		subject: "Google headquarters, Googleplex, Mountain View",
		queries: []string{"Googleplex", "Google headquarters Mountain View"},
	},
	{
		name:    "Microsoft",
		subject: "Microsoft headquarters, Redmond campus",
		queries: []string{"Microsoft Redmond campus", "Microsoft headquarters Redmond"},
	},
	{
		name:    "Amazon",
		aliases: []string{"Amazon Web Services", "AWS"},
		subject: "Amazon headquarters, Seattle",
		queries: []string{"Amazon Spheres Seattle", "Amazon headquarters Seattle"},
	},
	{
		name:    "Nvidia",
		subject: "Nvidia headquarters, Santa Clara",
		queries: []string{"Nvidia headquarters Santa Clara", "Nvidia Endeavor building"},
	},
	{
		name:    "Apple",
		subject: "Apple Park, Cupertino",
		queries: []string{"Apple Park Cupertino", "Apple Park"},
	},
	{
		name:    "Meta",
		aliases: []string{"Meta Platforms", "Facebook"},
		subject: "Meta headquarters, Menlo Park",
		queries: []string{"Meta headquarters Menlo Park", "Facebook headquarters Menlo Park"},
	},
	{
		name:    "Anthropic",
		subject: "Anthropic headquarters, 500 Howard Street, San Francisco",
		queries: []string{"500 Howard Street San Francisco", "Anthropic headquarters"},
	},
	{
		name:    "Stripe",
		subject: "Stripe headquarters, 354 Oyster Point Boulevard, South San Francisco",
		queries: []string{"Stripe headquarters Oyster Point", "354 Oyster Point South San Francisco"},
	},
	{
		name:    "Cloudflare",
		subject: "Cloudflare headquarters, 101 Townsend Street, San Francisco",
		queries: []string{"Cloudflare headquarters San Francisco", "Cloudflare office"},
	},
	{
		name:    "SpaceX",
		subject: "SpaceX headquarters, Starbase, Texas",
		queries: []string{"SpaceX Starbase", "Starbase Texas"},
	},
	{
		name:    "Tesla",
		subject: "Tesla headquarters, Gigafactory Texas, Austin",
		queries: []string{"Gigafactory Texas", "Tesla Giga Texas"},
	},
	{
		name:    "Intel",
		subject: "Intel headquarters, Robert Noyce Building, Santa Clara",
		queries: []string{"Robert Noyce Building", "Intel headquarters Santa Clara"},
	},
	{
		name:    "IBM",
		subject: "IBM headquarters, Armonk, New York",
		queries: []string{"IBM headquarters Armonk", "IBM Armonk"},
	},
	{
		name:    "Salesforce",
		subject: "Salesforce Tower, San Francisco",
		queries: []string{"Salesforce Tower", "Salesforce Tower San Francisco"},
	},
}

func headquartersForSubject(subject string) (headquartersEntry, bool) {
	// A comma can directly follow the name or a headquarters descriptor.
	beforeComma, _, _ := strings.Cut(subject, ",")
	words := strings.Fields(beforeComma)
	var match headquartersEntry
	longest := 0
	for _, entry := range headquarters {
		for _, name := range append([]string{entry.name}, entry.aliases...) {
			nameWords := strings.Fields(name)
			if len(nameWords) <= longest || !hasWordPrefixFold(words, nameWords) {
				continue
			}
			if remaining := words[len(nameWords):]; len(remaining) > 0 {
				switch strings.ToLower(remaining[0]) {
				case "headquarters", "hq", "campus", "office", "offices":
				default:
					continue
				}
			}
			match, longest = entry, len(nameWords)
		}
	}
	return match, longest > 0
}
