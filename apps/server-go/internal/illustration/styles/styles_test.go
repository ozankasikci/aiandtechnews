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
	if len(names) != 2 || names[0] != "anime" || names[1] != "gouache" {
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
	gouache, ok := catalog.Get("gouache")
	if !ok || gouache.ID() != "gouache@1" {
		t.Fatalf("gouache = %+v %v", gouache.ID(), ok)
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
		"one anchor":    {"x/style.json": {Data: []byte(`{"name":"x","version":1,"summary":"s","prompt":"p","anchors":["a.jpg"]}`)}, "x/a.jpg": {}},
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
