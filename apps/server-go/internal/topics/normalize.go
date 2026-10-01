// Package topics groups articles into topic hubs (a company, product,
// person or theme): it extracts the entities an article is about, resolves
// them to canonical topics through an alias table, and keeps each hub's
// summary and key facts fresh and verified. The public read side lives in
// package content.
package topics

import (
	"strings"
	"unicode"
)

// Kinds are the topic kinds the schema allows.
var Kinds = map[string]bool{"company": true, "product": true, "person": true, "theme": true}

// legalSuffixes are dropped from the end of a name when matching aliases.
var legalSuffixes = map[string]bool{
	"inc": true, "incorporated": true, "corp": true, "corporation": true, "ltd": true, "limited": true,
	"llc": true, "plc": true, "gmbh": true, "co": true, "ag": true, "sa": true, "bv": true, "nv": true,
}

// Key is the form of a name used to match aliases: lowercase, possessives and
// punctuation removed, a leading "the" and trailing legal suffixes dropped,
// and the words joined without spaces, so "OpenAI Inc", "Open AI" and
// "OpenAI's" all give "openai". It is empty when nothing meaningful is left.
func Key(name string) string {
	name = strings.ToLower(name)
	name = strings.NewReplacer("'s ", " ", "’s ", " ").Replace(name + " ")
	words := strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) > 1 && words[0] == "the" {
		words = words[1:]
	}
	for len(words) > 1 && legalSuffixes[words[len(words)-1]] {
		words = words[:len(words)-1]
	}
	return strings.Join(words, "")
}
