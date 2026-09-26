// Package brands holds the official logos of well-known companies, placed in
// featured images of stories about them. See README.md.
package brands

import (
	"embed"
	"encoding/json"
	"fmt"
	"slices"
)

//go:embed brands.json *.png
var files embed.FS

// Brand is one company with its logo (PNG bytes).
type Brand struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	Logo    []byte   `json:"-"`
}

// Catalog is every brand, in file order.
type Catalog struct{ brands []Brand }

// Load reads the embedded brands and their logos.
func Load() (Catalog, error) {
	raw, err := files.ReadFile("brands.json")
	if err != nil {
		return Catalog{}, err
	}
	var brands []Brand
	if err := json.Unmarshal(raw, &brands); err != nil {
		return Catalog{}, fmt.Errorf("brands.json: %w", err)
	}
	seen := map[string]bool{}
	for i, brand := range brands {
		if brand.ID == "" || brand.Name == "" || seen[brand.ID] {
			return Catalog{}, fmt.Errorf("brands.json: bad or duplicate entry %+v", brand)
		}
		seen[brand.ID] = true
		if brands[i].Logo, err = files.ReadFile(brand.ID + ".png"); err != nil {
			return Catalog{}, fmt.Errorf("brand %s: %w", brand.ID, err)
		}
	}
	return Catalog{brands: brands}, nil
}

// MustLoad is Load for wiring code; the embedded brands are covered by tests.
func MustLoad() Catalog {
	catalog, err := Load()
	if err != nil {
		panic(err)
	}
	return catalog
}

// All returns every brand.
func (c Catalog) All() []Brand { return slices.Clone(c.brands) }

// IDs returns every brand id.
func (c Catalog) IDs() []string {
	ids := make([]string, len(c.brands))
	for i, brand := range c.brands {
		ids[i] = brand.ID
	}
	return ids
}

// Get returns the brand with that id.
func (c Catalog) Get(id string) (Brand, bool) {
	for _, brand := range c.brands {
		if brand.ID == id {
			return brand, true
		}
	}
	return Brand{}, false
}

// Resolve keeps the known ids, in order, without duplicates, at most max.
func (c Catalog) Resolve(ids []string, max int) []Brand {
	var out []Brand
	for _, id := range ids {
		brand, ok := c.Get(id)
		if !ok || slices.ContainsFunc(out, func(b Brand) bool { return b.ID == id }) {
			continue
		}
		out = append(out, brand)
		if len(out) == max {
			break
		}
	}
	return out
}
