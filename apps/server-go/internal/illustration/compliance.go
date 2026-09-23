package illustration

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Verdict ports IllustrationComplianceVerdict.
type Verdict struct {
	Compliant  bool
	HasText    bool
	HasLogo    bool
	HasInjury  bool
	Notes      string
	Unverified bool
}

var (
	leadingFence  = regexp.MustCompile("(?i)^```(?:json)?\\s*")
	trailingFence = regexp.MustCompile("\\s*```$")
	jsonObject    = regexp.MustCompile(`(?s)\{.*\}`)
)

func unverified(notes string) Verdict { return Verdict{Notes: notes, Unverified: true} }

// ParseVerdict ports the response handling in checkIllustrationCompliance:
// fail closed on anything unparsable or incomplete.
func ParseVerdict(raw string) Verdict {
	cleaned := strings.TrimSpace(trailingFence.ReplaceAllString(leadingFence.ReplaceAllString(raw, ""), ""))
	candidate := cleaned
	if match := jsonObject.FindString(cleaned); match != "" {
		candidate = match
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(candidate), &parsed); err != nil {
		return unverified("compliance check returned unparsable JSON")
	}
	flags := [3]bool{}
	for i, key := range []string{"has_text", "has_logo_or_watermark", "depicts_unsupported_injury_or_violence"} {
		value, ok := parsed[key].(bool)
		if !ok {
			return unverified("compliance check returned an incomplete verdict")
		}
		flags[i] = value
	}
	notes, _ := parsed["notes"].(string)
	return Verdict{Compliant: !flags[0] && !flags[1] && !flags[2], HasText: flags[0], HasLogo: flags[1], HasInjury: flags[2], Notes: notes}
}

// Correction ports correctionForVerdict.
func Correction(verdict Verdict) string {
	switch {
	case verdict.HasText:
		return TextViolationCorrection
	case verdict.HasLogo:
		return LogoViolationCorrection
	case verdict.HasInjury:
		return InjuryViolationCorrection
	}
	return UnverifiedCorrection
}
