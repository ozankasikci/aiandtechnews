package illustration

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strconv"
	"strings"

	"golang.org/x/image/math/f64"

	xdraw "golang.org/x/image/draw"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration/brands"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration/styles"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
)

// CollageParts are the real pieces pasted onto a "collage" style backdrop.
// Any of them may be missing; MixedCollage skips what is absent.
type CollageParts struct {
	Subject *image.NRGBA   // cut-out of the source photo's subject (a person or a product)
	Scrap   image.Image    // the source photo itself, shown as a torn print behind the subject
	Logos   []brands.Brand // real brand logos, each on a taped paper card
}

func (p CollageParts) empty() bool { return p.Subject == nil && p.Scrap == nil && len(p.Logos) == 0 }

// Placement of the pieces as shares of the canvas, measured from the top-left.
const (
	scrapHeight    = 0.42
	scrapCentreX   = 0.20
	scrapCentreY   = 0.28
	subjectHeight  = 0.90
	subjectCentreX = 0.28
	logoHeight     = 0.20
	logoCentreX    = 0.47
	logoCentreY    = 0.16
	// With no cut-out, the print alone fills the left side.
	soloScrapHeight  = 0.62
	soloScrapCentreX = 0.25
	soloScrapCentreY = 0.55
)

// MixedCollage pastes the parts onto the backdrop the way a magazine collage
// would: duotone and halftone prints in the palette's colours, torn white
// paper edges with soft shadows, logos on taped cards, and paper grain over
// everything. The seed varies the torn edges between articles.
func MixedCollage(backdrop image.Image, parts CollageParts, palette styles.Palette, seed string) *image.NRGBA {
	bounds := backdrop.Bounds()
	width, height := float64(bounds.Dx()), float64(bounds.Dy())
	canvas := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(canvas, canvas.Bounds(), backdrop, bounds.Min, draw.Src)
	dark, light := paletteInk(palette)
	noiseSeed := hashSeed(seed)
	scale := height / 900

	if parts.Scrap != nil {
		sb := parts.Scrap.Bounds()
		crop := image.Rect(sb.Min.X, sb.Min.Y+sb.Dy()*15/100, sb.Min.X+sb.Dx()*55/100, sb.Min.Y+sb.Dy()*85/100)
		// Without a cut-out the print is the centrepiece: the whole photo, large, filling the left side.
		sh, cx, cy, tilt := scrapHeight, scrapCentreX, scrapCentreY, -6.0
		halftone := 0.55
		if parts.Subject == nil {
			crop = sb
			sh, cx, cy, tilt = soloScrapHeight, soloScrapCentreX, soloScrapCentreY, -3
			halftone = 0.3
		}
		print := toNRGBA(parts.Scrap, crop)
		duotone(print, dark, light, halftone)
		piece := paperBacked(print, int(14*scale*float64(print.Bounds().Dy())/(sh*height)), noiseSeed+1)
		paste(canvas, piece, sh*height, cx*width, cy*height, tilt)
	}
	if parts.Subject != nil {
		subject := toNRGBA(parts.Subject, parts.Subject.Bounds())
		duotone(subject, dark, light, 0.22)
		border := int(12 * scale * float64(subject.Bounds().Dy()) / (subjectHeight * height))
		piece := paperBacked(subject, border, noiseSeed+2)
		// Bottom-anchored: the subject rises from the lower edge like a cut print.
		h := subjectHeight * height * float64(piece.Bounds().Dy()) / float64(subject.Bounds().Dy())
		paste(canvas, piece, h, subjectCentreX*width, height-h/2+h*0.02, -2)
	}
	for i, logo := range parts.Logos {
		card := logoCard(logo, noiseSeed+3+uint32(i))
		if card == nil {
			continue
		}
		x := logoCentreX*width + float64(i)*0.2*width
		paste(canvas, card, logoHeight*height, x, logoCentreY*height+float64(i)*0.04*height, 4-float64(i)*7)
	}
	addGrain(canvas, noiseSeed, 0.05)
	return canvas
}

// paletteInk is the palette's darkest and lightest colours, with safe defaults.
func paletteInk(palette styles.Palette) (color.NRGBA, color.NRGBA) {
	dark, okDark := parseHex(palette.Dark)
	light, okLight := parseHex(palette.Light)
	if !okDark {
		dark = color.NRGBA{R: 0x22, G: 0x24, B: 0x28, A: 255}
	}
	if !okLight {
		light = color.NRGBA{R: 0xf3, G: 0xef, B: 0xe6, A: 255}
	}
	return dark, light
}

func parseHex(value string) (color.NRGBA, bool) {
	v, err := strconv.ParseUint(strings.TrimPrefix(value, "#"), 16, 32)
	if err != nil || len(strings.TrimPrefix(value, "#")) != 6 {
		return color.NRGBA{}, false
	}
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}, true
}

func hashSeed(seed string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return h.Sum32()
}

func toNRGBA(src image.Image, r image.Rectangle) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Bounds(), src, r.Min, draw.Src)
	return out
}

// duotone maps brightness onto dark→light in place, overlaying a halftone
// dot screen at the given strength. Alpha is kept.
func duotone(img *image.NRGBA, dark, light color.NRGBA, halftone float64) {
	b := img.Bounds()
	const cell = 7.0
	sin, cos := math.Sincos(0.4)
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			i := y*img.Stride + x*4
			if img.Pix[i+3] == 0 {
				continue
			}
			lum := (0.299*float64(img.Pix[i]) + 0.587*float64(img.Pix[i+1]) + 0.114*float64(img.Pix[i+2])) / 255
			lum = clamp01((lum-0.5)*1.15 + 0.5)
			// Dot screen: each rotated cell holds a dot whose area grows with darkness.
			u, v := float64(x)*cos-float64(y)*sin, float64(x)*sin+float64(y)*cos
			du, dv := math.Mod(u, cell)/cell-0.5, math.Mod(v, cell)/cell-0.5
			if du < -0.5 {
				du += 1
			}
			if dv < -0.5 {
				dv += 1
			}
			inDot := 0.0
			if math.Hypot(du, dv) < math.Sqrt((1-lum)/math.Pi) {
				inDot = 1
			}
			t := lum * (1 - halftone*inDot)
			img.Pix[i] = lerp8(dark.R, light.R, t)
			img.Pix[i+1] = lerp8(dark.G, light.G, t)
			img.Pix[i+2] = lerp8(dark.B, light.B, t)
		}
	}
}

func lerp8(a, b uint8, t float64) uint8 {
	return uint8(float64(a) + (float64(b)-float64(a))*clamp01(t) + 0.5)
}

// paperBacked puts the image on a torn off-white paper shape that follows its
// alpha, with a soft shadow, returning a larger image with room for both.
func paperBacked(img *image.NRGBA, border int, seed uint32) *image.NRGBA {
	border = max(border, 4)
	margin := border * 3
	w, h := img.Bounds().Dx()+2*margin, img.Bounds().Dy()+2*margin
	mask := make([]bool, w*h)
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			mask[(y+margin)*w+x+margin] = img.Pix[y*img.Stride+x*4+3] > 127
		}
	}
	distance := distanceTransform(mask, w, h)
	paper := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Torn edge: the border width wobbles with smooth noise.
			edge := float64(border) * (0.75 + 0.6*valueNoise(float64(x)/9, float64(y)/9, seed))
			paper[y*w+x] = clamp01(edge + 0.5 - distance[y*w+x])
		}
	}
	shadow := boxBlur(paper, w, h, max(2, border*9/10))
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	dx, dy := border/2, border*8/10
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*out.Stride + x*4
			if sx, sy := x-dx, y-dy; sx >= 0 && sy >= 0 {
				if a := 0.45 * shadow[sy*w+sx]; a > 0 {
					out.Pix[i+3] = uint8(a*255 + 0.5)
				}
			}
			if a := paper[y*w+x]; a > 0 {
				blendOver(out.Pix[i:i+4], [4]float64{247, 245, 237, a})
			}
		}
	}
	draw.Draw(out, img.Bounds().Add(image.Pt(margin, margin)), img, image.Point{}, draw.Over)
	return out
}

// blendOver draws an un-premultiplied RGBA colour over a non-premultiplied pixel.
func blendOver(dst []uint8, src [4]float64) {
	sa := clamp01(src[3])
	da := float64(dst[3]) / 255
	oa := sa + da*(1-sa)
	if oa == 0 {
		return
	}
	for c := 0; c < 3; c++ {
		dst[c] = uint8((src[c]*sa+float64(dst[c])*da*(1-sa))/oa + 0.5)
	}
	dst[3] = uint8(oa*255 + 0.5)
}

// logoCard prints the logo on an off-white paper card with a strip of tape.
func logoCard(logo brands.Brand, seed uint32) *image.NRGBA {
	decoded, err := png.Decode(bytes.NewReader(logo.Logo))
	if err != nil || decoded.Bounds().Empty() {
		return nil
	}
	lw, lh := decoded.Bounds().Dx(), decoded.Bounds().Dy()
	padX, padY := lw/8, lh*35/100
	card := image.NewNRGBA(image.Rect(0, 0, lw+2*padX, lh+2*padY))
	draw.Draw(card, card.Bounds(), image.NewUniform(color.NRGBA{R: 250, G: 248, B: 240, A: 255}), image.Point{}, draw.Src)
	draw.Draw(card, decoded.Bounds().Sub(decoded.Bounds().Min).Add(image.Pt(padX, padY)), decoded, decoded.Bounds().Min, draw.Over)
	piece := paperBacked(card, max(4, card.Bounds().Dy()/18), seed)
	cw := card.Bounds().Dx()
	margin := (piece.Bounds().Dx() - cw) / 2
	tape := image.Rect(margin+cw*36/100, 0, margin+cw*64/100, margin+card.Bounds().Dy()*18/100)
	draw.Draw(piece, tape, image.NewUniform(color.NRGBA{R: 237, G: 225, B: 184, A: 190}), image.Point{}, draw.Over)
	return piece
}

// paste scales the piece to the given height, rotates it by degrees and
// draws it centred at (cx, cy) on the canvas.
func paste(canvas *image.NRGBA, piece *image.NRGBA, height, cx, cy, degrees float64) {
	pb := piece.Bounds()
	s := height / float64(pb.Dy())
	sin, cos := math.Sincos(degrees * math.Pi / 180)
	// Maps piece coordinates to canvas coordinates: centre, scale, rotate, move.
	px, py := float64(pb.Dx())/2, float64(pb.Dy())/2
	m := f64.Aff3{
		s * cos, -s * sin, cx - s*(cos*px-sin*py),
		s * sin, s * cos, cy - s*(sin*px+cos*py),
	}
	xdraw.BiLinear.Transform(canvas, m, piece, pb, xdraw.Over, nil)
}

// addGrain multiplies a fine monochrome noise over the canvas.
func addGrain(canvas *image.NRGBA, seed uint32, strength float64) {
	state := seed | 1
	for i := 0; i < len(canvas.Pix); i += 4 {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		f := 1 - strength*float64(state%1000)/1000
		for c := 0; c < 3; c++ {
			canvas.Pix[i+c] = uint8(float64(canvas.Pix[i+c]) * f)
		}
	}
}

// valueNoise is smooth 2D noise in [0,1], stable for a seed.
func valueNoise(x, y float64, seed uint32) float64 {
	x0, y0 := math.Floor(x), math.Floor(y)
	fx, fy := x-x0, y-y0
	fx, fy = fx*fx*(3-2*fx), fy*fy*(3-2*fy)
	corner := func(ix, iy float64) float64 {
		h := uint32(int32(ix))*374761393 + uint32(int32(iy))*668265263 + seed*2246822519
		h = (h ^ (h >> 13)) * 1274126177
		return float64(h^(h>>16)) / float64(math.MaxUint32)
	}
	top := corner(x0, y0)*(1-fx) + corner(x0+1, y0)*fx
	bottom := corner(x0, y0+1)*(1-fx) + corner(x0+1, y0+1)*fx
	return top*(1-fy) + bottom*fy
}

// PrepareSubject decodes a cut-out and trims it to its subject. It is looser
// than PrepareCutout: the subject may be a product as well as a person, and
// may touch the edges. Nearly empty or full-frame masks are rejected.
func PrepareSubject(data []byte) (*image.NRGBA, error) {
	decoded, err := imaging.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPoorCutout, err)
	}
	cut := toNRGBA(decoded, decoded.Bounds())
	keepLargestComponent(cut)
	b := cut.Bounds()
	box := image.Rectangle{Min: image.Pt(b.Dx(), b.Dy())}
	count := 0
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			if cut.Pix[y*cut.Stride+x*4+3] > 127 {
				count++
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	share := float64(count) / float64(b.Dx()*b.Dy())
	if share < 0.02 || share > 0.9 || box.Dy() < b.Dy()/4 {
		return nil, fmt.Errorf("%w: subject covers %.1f%% of the photo", ErrPoorCutout, share*100)
	}
	return toNRGBA(cut, box), nil
}

// EncodePNG encodes an image as PNG.
func EncodePNG(img image.Image) ([]byte, error) { return encodePNG(img) }

// keepLargestComponent clears every opaque region except the largest one, so
// stray specks of a cut-out mask don't become paper scraps of their own.
func keepLargestComponent(img *image.NRGBA) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	label := make([]int32, w*h)
	opaque := func(i int) bool { return img.Pix[(i/w)*img.Stride+(i%w)*4+3] > 127 }
	best, bestSize := int32(0), 0
	next := int32(0)
	stack := []int{}
	for start := 0; start < w*h; start++ {
		if label[start] != 0 || !opaque(start) {
			continue
		}
		next++
		size := 0
		stack = append(stack[:0], start)
		label[start] = next
		for len(stack) > 0 {
			i := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			size++
			x, y := i%w, i/w
			for _, n := range [4][2]int{{x - 1, y}, {x + 1, y}, {x, y - 1}, {x, y + 1}} {
				if n[0] < 0 || n[1] < 0 || n[0] >= w || n[1] >= h {
					continue
				}
				j := n[1]*w + n[0]
				if label[j] == 0 && opaque(j) {
					label[j] = next
					stack = append(stack, j)
				}
			}
		}
		if size > bestSize {
			best, bestSize = next, size
		}
	}
	for i, l := range label {
		if l != best {
			img.Pix[(i/w)*img.Stride+(i%w)*4+3] = 0
		}
	}
}
