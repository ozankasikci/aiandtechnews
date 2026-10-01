package topics

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

// Simulator applies entities to an in-memory copy of the topic tables, so a
// dry run can show which topics an extraction would create or grow without
// writing to the (read-only) database. It resolves aliases like Store.Assign.
type Simulator struct {
	byKey  map[string]string
	topics map[string]*TopicCount
}

// NewSimulator loads the existing topics, their aliases and counts.
func NewSimulator(ctx context.Context, db *sql.DB) (*Simulator, error) {
	sim := &Simulator{byKey: map[string]string{}, topics: map[string]*TopicCount{}}
	counts, err := NewStore(db, nil).Counts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range counts {
		sim.topics[counts[i].Slug] = &counts[i]
	}
	rows, err := db.QueryContext(ctx, `SELECT a.alias_norm, t.slug FROM topic_aliases a JOIN topics t ON t.id = a.topic_id`)
	if err != nil {
		return nil, fmt.Errorf("read topic aliases: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, slug string
		if err := rows.Scan(&key, &slug); err != nil {
			return nil, err
		}
		sim.byKey[key] = slug
	}
	return sim, rows.Err()
}

// Add records one article's entities and returns the topic slugs they map to.
func (s *Simulator) Add(entities []Entity) []string {
	var slugs []string
	for _, entity := range entities {
		nameKey := Key(entity.Name)
		keys := []string{nameKey}
		for _, alias := range entity.Aliases {
			if key := Key(alias); len(key) >= 3 && key != nameKey {
				keys = append(keys, key)
			}
		}
		slug := ""
		for _, key := range keys {
			if slug = s.byKey[key]; slug != "" {
				break
			}
		}
		if slug == "" {
			slug = content.Slugify(entity.Name)
			if _, taken := s.topics[slug]; taken {
				slug = content.Slugify(entity.Name + " " + entity.Kind)
			}
			s.topics[slug] = &TopicCount{Slug: slug, Name: entity.Name, Kind: entity.Kind}
		}
		for _, key := range keys {
			if _, taken := s.byKey[key]; !taken {
				s.byKey[key] = slug
			}
		}
		s.topics[slug].Articles++
		slugs = append(slugs, slug)
	}
	return slugs
}

// Counts lists the simulated topics, the most covered first.
func (s *Simulator) Counts() []TopicCount {
	out := make([]TopicCount, 0, len(s.topics))
	for _, topic := range s.topics {
		out = append(out, *topic)
	}
	slices.SortFunc(out, func(a, b TopicCount) int {
		if a.Articles != b.Articles {
			return b.Articles - a.Articles
		}
		return cmp.Compare(a.Slug, b.Slug)
	})
	return out
}
