package topics

import (
	"strings"
	"sync"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

// genericThemes are too broad to be useful hubs on an AI and technology
// site; every story would belong to them.
var genericThemes = map[string]bool{
	Key("Artificial intelligence"): true, Key("AI"): true, Key("Technology"): true, Key("Tech"): true,
	Key("Machine learning"): true, Key("Generative AI"): true, Key("Tech industry"): true,
	Key("Technology industry"): true, Key("Innovation"): true, Key("Startups"): true, Key("Software"): true,
}

var (
	sourceKeysOnce sync.Once
	sourceKeys     []string
)

// excluded reports whether a name must never become a topic: an approved
// news source (or something named after one, like "TechCrunch Disrupt"),
// since the site never names its sources, or a theme too generic to help.
func excluded(name string) bool {
	key := Key(name)
	if genericThemes[key] {
		return true
	}
	sourceKeysOnce.Do(func() {
		for _, source := range content.SourceNames() {
			label, _, _ := strings.Cut(strings.TrimPrefix(strings.ToLower(source), "www."), ".")
			for _, candidate := range []string{Key(source), Key(label)} {
				if len(candidate) >= 3 {
					sourceKeys = append(sourceKeys, candidate)
				}
			}
		}
	})
	for _, source := range sourceKeys {
		if key == source || (len(source) >= 5 && strings.HasPrefix(key, source)) {
			return true
		}
	}
	return false
}
