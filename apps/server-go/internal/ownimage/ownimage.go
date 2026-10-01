// Package ownimage decides whether an image URL is one the site may show: an
// image we host (or a licensed stock library), never a news source's own
// photo. It is the Go twin of apps/web/app/lib/own-image.ts; keep the two in
// step.
package ownimage

import (
	"net/url"
	"strings"
)

// hosts are the hostnames whose images the site shows.
var hosts = map[string]bool{
	"aiandtech-feature-images-106111531869.s3.eu-west-1.amazonaws.com": true,
	"www.aiandtech.news": true,
	"aiandtech.news":     true,
	// Unsplash photos are free to use under the Unsplash licence.
	"images.unsplash.com": true,
}

// Is reports whether rawURL is one of our own images: a site-relative path,
// or an https URL on one of our hosts. An empty URL is not.
func Is(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	if strings.HasPrefix(rawURL, "/") && !strings.HasPrefix(rawURL, "//") {
		return true
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" {
		return false
	}
	return hosts[strings.ToLower(parsed.Hostname())]
}
