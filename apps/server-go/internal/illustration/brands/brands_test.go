package brands

import (
	"bytes"
	"image"
	_ "image/png"
	"testing"
)

func TestEmbeddedBrandsLoadWithDecodableLogos(t *testing.T) {
	catalog, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.All()) < 15 {
		t.Fatalf("want at least 15 brands, got %d", len(catalog.All()))
	}
	for _, brand := range catalog.All() {
		if _, _, err := image.DecodeConfig(bytes.NewReader(brand.Logo)); err != nil {
			t.Fatalf("%s logo: %v", brand.ID, err)
		}
	}
	got := catalog.Resolve([]string{"openai", "nope", "openai", "google", "meta"}, 2)
	if len(got) != 2 || got[0].ID != "openai" || got[1].ID != "google" {
		t.Fatalf("Resolve = %+v", got)
	}
}
