// Package styles holds the versioned house illustration styles. Each style is
// a folder with a style.json (name, version, summary, prompt, anchors) and the
// anchor reference images it lists. See README.md for how to add or change one.
package styles

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

//go:embed */style.json */*.jpg
var files embed.FS

const (
	minAnchors = 2
	maxAnchors = 3
)

// Style is one house style.
type Style struct {
	Name    string
	Version int
	// Summary tells the analyzer which stories the style suits.
	Summary string
	// Prompt is appended to every image prompt as "Style: <Prompt>".
	Prompt  string
	Anchors []Anchor
}

// Anchor is a reference image attached to generations in this style.
type Anchor struct {
	Name string
	Data []byte
}

// ID is name@version, used in logs and reports.
func (s Style) ID() string { return fmt.Sprintf("%s@%d", s.Name, s.Version) }

// Catalog is the set of styles, sorted by name.
type Catalog struct{ styles []Style }

type definition struct {
	Name    string   `json:"name"`
	Version int      `json:"version"`
	Summary string   `json:"summary"`
	Prompt  string   `json:"prompt"`
	Anchors []string `json:"anchors"`
}

// Load reads the embedded styles and validates them.
func Load() (Catalog, error) { return load(files) }

// MustLoad is Load for wiring code; the embedded styles are covered by tests.
func MustLoad() Catalog {
	catalog, err := Load()
	if err != nil {
		panic(err)
	}
	return catalog
}

func load(fsys fs.FS) (Catalog, error) {
	definitions, err := fs.Glob(fsys, "*/style.json")
	if err != nil {
		return Catalog{}, err
	}
	var catalog Catalog
	for _, file := range definitions {
		folder := path.Dir(file)
		raw, err := fs.ReadFile(fsys, file)
		if err != nil {
			return Catalog{}, err
		}
		var def definition
		if err := json.Unmarshal(raw, &def); err != nil {
			return Catalog{}, fmt.Errorf("style %s: %w", folder, err)
		}
		if def.Name != folder {
			return Catalog{}, fmt.Errorf("style %s: name %q must match its folder", folder, def.Name)
		}
		if def.Version < 1 || strings.TrimSpace(def.Prompt) == "" || strings.TrimSpace(def.Summary) == "" {
			return Catalog{}, fmt.Errorf("style %s: version, summary and prompt are required", folder)
		}
		if len(def.Anchors) < minAnchors || len(def.Anchors) > maxAnchors {
			return Catalog{}, fmt.Errorf("style %s: want %d-%d anchors, got %d", folder, minAnchors, maxAnchors, len(def.Anchors))
		}
		style := Style{Name: def.Name, Version: def.Version, Summary: strings.TrimSpace(def.Summary), Prompt: strings.TrimSpace(def.Prompt)}
		for _, name := range def.Anchors {
			data, err := fs.ReadFile(fsys, path.Join(folder, name))
			if err != nil {
				return Catalog{}, fmt.Errorf("style %s: anchor %s: %w", folder, name, err)
			}
			style.Anchors = append(style.Anchors, Anchor{Name: name, Data: data})
		}
		catalog.styles = append(catalog.styles, style)
	}
	if len(catalog.styles) == 0 {
		return Catalog{}, fmt.Errorf("no styles found")
	}
	slices.SortFunc(catalog.styles, func(a, b Style) int { return strings.Compare(a.Name, b.Name) })
	return catalog, nil
}

// Get returns the named style.
func (c Catalog) Get(name string) (Style, bool) {
	for _, style := range c.styles {
		if style.Name == name {
			return style, true
		}
	}
	return Style{}, false
}

// All returns every style, sorted by name.
func (c Catalog) All() []Style { return slices.Clone(c.styles) }

// Names returns every style name, sorted.
func (c Catalog) Names() []string {
	names := make([]string, len(c.styles))
	for i, style := range c.styles {
		names[i] = style.Name
	}
	return names
}

// Default is the style used when a brief names none that exists: the first
// by name.
func (c Catalog) Default() Style { return c.styles[0] }
