package ownimage_test

import (
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/ownimage"
)

func TestIs(t *testing.T) {
	cases := map[string]bool{
		"":                            false,
		"/images/default-article.jpg": true,
		"//techcrunch.com/x.jpg":      false,
		"https://aiandtech-feature-images-106111531869.s3.eu-west-1.amazonaws.com/featured/a.webp": true,
		"https://www.aiandtech.news/x.png":                true,
		"https://aiandtech.news/x.png":                    true,
		"https://AIANDTECH.news/x.png":                    true,
		"https://images.unsplash.com/photo-1?w=800":       true,
		"http://aiandtech.news/x.png":                     false,
		"https://techcrunch.com/wp-content/uploads/a.jpg": false,
		"https://cdn.arstechnica.net/a.jpg":               false,
		"https://aiandtech.news.evil.com/a.jpg":           false,
		"https://evil.com/aiandtech.news/a.jpg":           false,
		"https://aiandtech.news@evil.com/a.jpg":           false,
		"not a url":                                       false,
	}
	for input, want := range cases {
		if got := ownimage.Is(input); got != want {
			t.Errorf("Is(%q) = %t, want %t", input, got, want)
		}
	}
}
