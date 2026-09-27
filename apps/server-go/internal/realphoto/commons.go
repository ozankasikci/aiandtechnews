package realphoto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
)

// commonsSearchLimit is how many files each Commons search returns.
const commonsSearchLimit = 20

type commonsResponse struct {
	Query struct {
		Pages []commonsPage `json:"pages"`
	} `json:"query"`
	Error *struct {
		Code string `json:"code"`
		Info string `json:"info"`
	} `json:"error"`
}

type commonsPage struct {
	Title     string `json:"title"`
	Index     int    `json:"index"`
	ImageInfo []struct {
		URL            string                         `json:"url"`
		DescriptionURL string                         `json:"descriptionurl"`
		ThumbURL       string                         `json:"thumburl"`
		Width          int                            `json:"width"`
		Height         int                            `json:"height"`
		MIME           string                         `json:"mime"`
		ExtMetadata    map[string]struct{ Value any } `json:"extmetadata"`
	} `json:"imageinfo"`
}

// commons searches Wikimedia Commons with the plan's queries and verifies
// the licensed, big enough, landscape results.
func (f *Finder) commons(ctx context.Context, request Request, plan Plan) ([]Candidate, error) {
	var candidates []Candidate
	seen := map[string]bool{}
	var errs []error
	for _, query := range plan.Queries {
		pages, err := f.searchCommons(ctx, query)
		if err != nil {
			errs = append(errs, fmt.Errorf("search %q: %w", query, err))
			continue
		}
		for _, page := range pages {
			if seen[page.Title] || len(page.ImageInfo) == 0 {
				continue
			}
			seen[page.Title] = true
			candidates = append(candidates, commonsCandidate(page))
		}
	}
	f.verifyAll(ctx, plan, request, KindCommons, candidates)
	return candidates, errors.Join(errs...)
}

func (f *Finder) searchCommons(ctx context.Context, query string) ([]commonsPage, error) {
	params := url.Values{
		"action":                {"query"},
		"format":                {"json"},
		"formatversion":         {"2"},
		"generator":             {"search"},
		"gsrsearch":             {"filetype:bitmap " + query},
		"gsrnamespace":          {"6"},
		"gsrlimit":              {fmt.Sprint(commonsSearchLimit)},
		"prop":                  {"imageinfo"},
		"iiprop":                {"url|size|mime|extmetadata"},
		"iiurlwidth":            {"1600"},
		"iiextmetadatafilter":   {"LicenseShortName|LicenseUrl|Artist|Credit|UsageTerms|Restrictions"},
		"iiextmetadatalanguage": {"en"},
	}
	page, err := f.get(ctx, f.commonsAPI+"?"+params.Encode(), "application/json", maxAPIBytes, false, func(mediaType string) bool {
		return mediaType == "application/json"
	})
	if err != nil {
		return nil, err
	}
	var response commonsResponse
	if err := json.Unmarshal(page.Body, &response); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%s: %s", response.Error.Code, response.Error.Info)
	}
	pages := response.Query.Pages
	sortByRank(pages, func(page commonsPage) int { return page.Index })
	return pages, nil
}

// commonsCandidate turns a search result into a candidate, with Reason set
// when its licence, size or shape rules it out before any download.
func commonsCandidate(page commonsPage) Candidate {
	info := page.ImageInfo[0]
	meta := func(key string) string {
		if value, ok := info.ExtMetadata[key]; ok {
			if text, ok := value.Value.(string); ok {
				return text
			}
		}
		return ""
	}
	candidate := Candidate{
		Source:     KindCommons,
		ImageURL:   info.ThumbURL,
		PageURL:    info.DescriptionURL,
		License:    oneLine(stripHTML(meta("LicenseShortName")), 40),
		LicenseURL: strings.TrimSpace(meta("LicenseUrl")),
		Artist:     cleanArtist(meta("Artist")),
		Width:      info.Width,
		Height:     info.Height,
	}
	if candidate.ImageURL == "" {
		candidate.ImageURL = info.URL
	}
	license, ok := FreeLicense(candidate.License)
	switch {
	case !ok:
		candidate.Reason = "licence not allowed: " + candidate.License
	case candidate.Artist == "" && license != "public domain" && license != "cc0":
		candidate.Reason = "no author to credit"
	case !strings.HasPrefix(candidate.PageURL, "https://"):
		candidate.Reason = "no file page"
	case !strings.HasPrefix(candidate.ImageURL, "https://"):
		candidate.Reason = "no image URL"
	}
	if candidate.Reason != "" {
		return candidate
	}
	if ok, why := fitsSlot(info.Width, info.Height); !ok {
		candidate.Reason = why
	}
	return candidate
}

var ccVersion = regexp.MustCompile(`^\d+(\.\d+)?( [a-z-]+)?$`)

// FreeLicense accepts CC0, public domain, CC BY x and CC BY-SA x licence
// short names and returns their family ("cc0", "public domain", "cc by",
// "cc by-sa"). NC and ND licences, GFDL-only and anything else are refused.
func FreeLicense(shortName string) (string, bool) {
	name := strings.ToLower(strings.Join(strings.Fields(shortName), " "))
	switch {
	case name == "cc0" || strings.HasPrefix(name, "cc0 ") || name == "cc-zero":
		return "cc0", true
	case name == "public domain" || name == "pd" || strings.HasPrefix(name, "public domain "):
		return "public domain", true
	case strings.HasPrefix(name, "cc by-sa "):
		if ccVersion.MatchString(strings.TrimPrefix(name, "cc by-sa ")) {
			return "cc by-sa", true
		}
	case strings.HasPrefix(name, "cc by "):
		if ccVersion.MatchString(strings.TrimPrefix(name, "cc by ")) {
			return "cc by", true
		}
	}
	return "", false
}

var (
	htmlTag     = regexp.MustCompile(`(?s)<[^>]*>`)
	artistNoise = regexp.MustCompile(`(?i)^(photo|photograph|image|picture)\s*(by|:)\s*`)
)

func stripHTML(text string) string {
	return strings.Join(strings.Fields(html.UnescapeString(htmlTag.ReplaceAllString(text, " "))), " ")
}

// cleanArtist is the Commons Artist field as a short plain credit.
func cleanArtist(raw string) string {
	artist := artistNoise.ReplaceAllString(stripHTML(raw), "")
	lower := strings.ToLower(artist)
	if lower == "unknown" || lower == "unknown author" || lower == "anonymous" || strings.HasPrefix(lower, "unknown ") {
		return ""
	}
	return oneLine(artist, 60)
}

// commonsCredit is "Photo: <Artist> / <License>, via Wikimedia Commons".
func commonsCredit(candidate Candidate) Credit {
	text := "Photo: " + candidate.License + ", via Wikimedia Commons"
	if candidate.Artist != "" {
		text = "Photo: " + candidate.Artist + " / " + candidate.License + ", via Wikimedia Commons"
	}
	return Credit{Kind: KindCommons, Text: text, URL: candidate.PageURL, License: candidate.License, LicenseURL: candidate.LicenseURL}
}
