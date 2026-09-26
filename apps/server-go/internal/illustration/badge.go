package illustration

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	xdraw "golang.org/x/image/draw"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration/brands"
)

// AddLogoBadges puts the real logos on a white tile in the bottom-right
// corner. It is the fallback when the provider could not place the logos in
// the scene itself. Logos that fail to decode are skipped.
func AddLogoBadges(img image.Image, logos []brands.Brand) image.Image {
	bounds := img.Bounds()
	logoHeight := bounds.Dy() * 7 / 100
	pad := logoHeight / 2
	var decoded []image.Image
	width := 0
	for _, logo := range logos {
		source, err := png.Decode(bytes.NewReader(logo.Logo))
		if err != nil || source.Bounds().Dy() == 0 {
			continue
		}
		w := source.Bounds().Dx() * logoHeight / source.Bounds().Dy()
		if max := bounds.Dx() / 4; w > max {
			w = max
		}
		scaled := image.NewNRGBA(image.Rect(0, 0, w, source.Bounds().Dy()*w/source.Bounds().Dx()))
		xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), source, source.Bounds(), xdraw.Over, nil)
		decoded = append(decoded, scaled)
		width += w
	}
	if len(decoded) == 0 {
		return img
	}
	width += pad * (len(decoded) + 1)
	out := image.NewNRGBA(bounds)
	draw.Draw(out, bounds, img, bounds.Min, draw.Src)
	tile := image.Rect(bounds.Max.X-pad-width, bounds.Max.Y-pad-logoHeight-2*pad, bounds.Max.X-pad, bounds.Max.Y-pad)
	draw.Draw(out, tile, image.NewUniform(color.NRGBA{R: 255, G: 255, B: 255, A: 235}), image.Point{}, draw.Over)
	x := tile.Min.X + pad
	for _, logo := range decoded {
		lb := logo.Bounds()
		y := tile.Min.Y + (tile.Dy()-lb.Dy())/2
		draw.Draw(out, image.Rect(x, y, x+lb.Dx(), y+lb.Dy()), logo, lb.Min, draw.Over)
		x += lb.Dx() + pad
	}
	return out
}
