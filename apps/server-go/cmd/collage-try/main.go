// Command collage-try composes a "collage" style image from local files, for
// tuning the effects. It never generates, uploads or touches a database.
//
//	go run ./cmd/collage-try -backdrop bg.png -cutout cut.png -source src.jpg -brand openai -palette charcoal-lime -out out.png
package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"os"
	"strings"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration/brands"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration/styles"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "collage-try: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	backdrop := flag.String("backdrop", "", "AI backdrop image (required)")
	cutout := flag.String("cutout", "", "cut-out PNG with alpha")
	source := flag.String("source", "", "source photo for the torn print")
	brandList := flag.String("brand", "", "comma-separated brand ids")
	paletteName := flag.String("palette", "navy-red", "palette name")
	out := flag.String("out", "collage.png", "output PNG")
	flag.Parse()
	if *backdrop == "" {
		return errors.New("-backdrop is required")
	}
	read := func(path string) (image.Image, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return imaging.Decode(data)
	}
	bg, err := read(*backdrop)
	if err != nil {
		return err
	}
	var parts illustration.CollageParts
	if *cutout != "" {
		data, err := os.ReadFile(*cutout)
		if err != nil {
			return err
		}
		if parts.Subject, err = illustration.PrepareSubject(data); err != nil {
			return err
		}
	}
	if *source != "" {
		if parts.Scrap, err = read(*source); err != nil {
			return err
		}
	}
	if *brandList != "" {
		parts.Logos = brands.MustLoad().Resolve(strings.Split(*brandList, ","), illustration.MaxBrands)
	}
	palette, ok := styles.MustLoad().Palette(*paletteName)
	if !ok {
		return fmt.Errorf("unknown palette %q", *paletteName)
	}
	img := illustration.MixedCollage(illustration.CropTo16x9(bg), parts, palette, *out)
	data, err := illustration.EncodePNG(img)
	if err != nil {
		return err
	}
	return os.WriteFile(*out, data, 0o644)
}
