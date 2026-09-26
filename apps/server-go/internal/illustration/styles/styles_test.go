package styles

import (
	"bytes"
	"image"
	_ "image/jpeg"
	"testing"
	"testing/fstest"
)

func TestEmbeddedStylesLoad(t *testing.T) {
	catalog, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	names := catalog.Names()
	if len(names) != 3 || names[0] != "collage" || names[1] != "graphic" || names[2] != "midcentury" {
		t.Fatalf("names = %v", names)
	}
	for _, style := range catalog.All() {
		if len(style.Anchors) < minAnchors || len(style.Anchors) > maxAnchors {
			t.Fatalf("%s: %d anchors", style.Name, len(style.Anchors))
		}
		for _, anchor := range style.Anchors {
			config, _, err := image.DecodeConfig(bytes.NewReader(anchor.Data))
			if err != nil {
				t.Fatalf("%s/%s: %v", style.Name, anchor.Name, err)
			}
			if config.Width > 1280 || config.Height > 1280 {
				t.Fatalf("%s/%s is %dx%d; downscale anchors to about 1024px", style.Name, anchor.Name, config.Width, config.Height)
			}
		}
	}
	graphic, ok := catalog.Get("graphic")
	if !ok || graphic.ID() != "graphic@2" {
		t.Fatalf("graphic = %+v %v", graphic.ID(), ok)
	}
	if _, ok := catalog.Get("oil"); ok {
		t.Fatal("unexpected style oil")
	}
}

func TestLoadRejectsInvalidStyles(t *testing.T) {
	valid := `{"name":"x","version":1,"summary":"s","prompt":"p","anchors":["a.jpg","b.jpg"]}`
	cases := map[string]fstest.MapFS{
		"name mismatch": {"x/style.json": {Data: []byte(`{"name":"y","version":1,"summary":"s","prompt":"p","anchors":["a.jpg","b.jpg"]}`)}, "x/a.jpg": {}, "x/b.jpg": {}},
		"no prompt":     {"x/style.json": {Data: []byte(`{"name":"x","version":1,"summary":"s","prompt":" ","anchors":["a.jpg","b.jpg"]}`)}, "x/a.jpg": {}, "x/b.jpg": {}},
		"no anchors":    {"x/style.json": {Data: []byte(`{"name":"x","version":1,"summary":"s","prompt":"p","anchors":[]}`)}},
		"missing file":  {"x/style.json": {Data: []byte(valid)}, "x/a.jpg": {}},
		"empty":         {},
	}
	for name, fsys := range cases {
		if _, err := load(fsys); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := load(fstest.MapFS{"x/style.json": {Data: []byte(valid)}, "x/a.jpg": {}, "x/b.jpg": {}}); err != nil {
		t.Fatalf("valid: %v", err)
	}
}

func TestEmbeddedPalettes(t *testing.T) {
	catalog := MustLoad()
	palettes := catalog.Palettes()
	if len(palettes) < 6 {
		t.Fatalf("want at least 6 palettes, got %d", len(palettes))
	}
	allowed := catalog.PalettesExcept([]string{palettes[0].Name, palettes[1].Name})
	if len(allowed) != len(palettes)-2 || allowed[0].Name != palettes[2].Name {
		t.Fatalf("PalettesExcept = %v", allowed)
	}
	every := make([]string, len(palettes))
	for i, palette := range palettes {
		every[i] = palette.Name
	}
	if len(catalog.PalettesExcept(every)) != len(palettes) {
		t.Fatal("avoiding every palette must still offer all of them")
	}
}

func TestWithoutStyles(t *testing.T) {
	catalog := MustLoad()
	kept := catalog.WithoutStyles([]string{"graphic", "collage"})
	if names := kept.Names(); len(names) != 1 || names[0] != "midcentury" || len(kept.Palettes()) != len(catalog.Palettes()) {
		t.Fatalf("names = %v, palettes = %d", names, len(kept.Palettes()))
	}
	if len(catalog.WithoutStyles(catalog.Names()).Names()) != len(catalog.Names()) {
		t.Fatal("avoiding every style must still offer all of them")
	}
}

func TestCollageStyleAndDefault(t *testing.T) {
	catalog := MustLoad()
	collage, ok := catalog.Get("collage")
	if !ok || !collage.Collage {
		t.Fatalf("collage = %+v", collage)
	}
	if def := catalog.Default(); def.Collage {
		t.Fatalf("default must not be a collage: %s", def.Name)
	}
	for _, palette := range catalog.Palettes() {
		if len(palette.Dark) != 6 || len(palette.Light) != 6 {
			t.Fatalf("palette %s lacks duotone inks", palette.Name)
		}
	}
}
