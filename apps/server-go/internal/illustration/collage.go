package illustration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	xdraw "golang.org/x/image/draw"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
)

// Collage geometry (from the approved prototype).
const (
	cutoutBottomCrop  = 0.11 // the cut-out's bottom 11% is dropped (captions, watermarks)
	personHeightShare = 0.90 // the person is 90% of the canvas height
	personCentreX     = 0.24 // centred at 24% of the width
	personMaxWidth    = 0.60 // a very wide subject (a group) is shrunk so the scene stays visible
	outlineAtHeight   = 14.0 // sticker outline in px at a 941px-high canvas
	outlineRefHeight  = 941.0
	shadowOpacity     = 0.35

	minMaskShare    = 0.04 // the subject must cover at least 4% of the cut-out
	maxMaskShare    = 0.85 // more than this is a failed mask (whole frame)
	minBoxHeight    = 0.40 // the subject must be at least 40% of the cut-out's height
	maxTopEdgeShare = 0.01 // more opaque than this along the top edge: the head is cut off
	maxSideShare    = 0.35 // more than this along a side: the subject is cut off
)

// ErrPoorCutout means the cut-out is unusable for a collage; the pipeline
// falls back to the normal illustration.
var ErrPoorCutout = errors.New("cut-out is unusable")

// Cutter cuts the foreground person out of a photo and returns a PNG with alpha.
type Cutter interface {
	Cutout(ctx context.Context, photo []byte) ([]byte, error)
}

// CutoutTool runs tools/cutout (CUTOUT_BIN), the macOS Vision cut-out.
type CutoutTool struct {
	Bin     string
	Timeout time.Duration
}

func (c *CutoutTool) Cutout(ctx context.Context, photo []byte) ([]byte, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "featured-cutout-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	extension := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp"}[imaging.SniffMIME(photo)]
	if extension == "" {
		return nil, fmt.Errorf("%w: unsupported source image type", ErrPoorCutout)
	}
	input := filepath.Join(dir, "source"+extension)
	output := filepath.Join(dir, "cutout.png")
	if err := os.WriteFile(input, photo, 0o600); err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(ctx, c.Bin, input, output).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("cutout: %w: %s", err, tail(string(out), 200))
	}
	return os.ReadFile(output)
}

// PrepareCutout crops the cut-out's bottom, checks it is a usable subject
// and returns it cropped to the subject.
func PrepareCutout(data []byte) (*image.NRGBA, error) {
	decoded, err := imaging.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPoorCutout, err)
	}
	bounds := decoded.Bounds()
	width := bounds.Dx()
	height := bounds.Dy() - int(math.Round(float64(bounds.Dy())*cutoutBottomCrop))
	if width < 16 || height < 16 {
		return nil, fmt.Errorf("%w: too small", ErrPoorCutout)
	}
	cut := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(cut, cut.Bounds(), decoded, bounds.Min, draw.Src)

	opaque := func(x, y int) bool { return cut.Pix[y*cut.Stride+x*4+3] > 127 }
	box := image.Rectangle{Min: image.Pt(width, height)}
	count, top, left, right := 0, 0, 0, 0
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if !opaque(x, y) {
				continue
			}
			count++
			box = box.Union(image.Rect(x, y, x+1, y+1))
			if y == 0 {
				top++
			}
			if x == 0 {
				left++
			}
			if x == width-1 {
				right++
			}
		}
	}
	share := float64(count) / float64(width*height)
	switch {
	case share < minMaskShare:
		return nil, fmt.Errorf("%w: subject covers %.1f%% of the photo", ErrPoorCutout, share*100)
	case share > maxMaskShare:
		return nil, fmt.Errorf("%w: mask covers %.0f%% of the photo", ErrPoorCutout, share*100)
	case float64(box.Dy()) < minBoxHeight*float64(height):
		return nil, fmt.Errorf("%w: subject is only %d of %d px tall", ErrPoorCutout, box.Dy(), height)
	case float64(top) > maxTopEdgeShare*float64(width):
		return nil, fmt.Errorf("%w: subject is cut off at the top", ErrPoorCutout)
	case float64(left) > maxSideShare*float64(height) || float64(right) > maxSideShare*float64(height):
		return nil, fmt.Errorf("%w: subject is cut off at the side", ErrPoorCutout)
	}
	subject := image.NewNRGBA(image.Rect(0, 0, box.Dx(), box.Dy()))
	draw.Draw(subject, subject.Bounds(), cut, box.Min, draw.Src)
	return subject, nil
}

// Composite places the person on the background: 90% of the canvas height,
// bottom-anchored, centred at 24% of the width (at most 60% wide), with a white sticker outline
// and a soft shadow. The person's pixels are only resampled, never redrawn.
func Composite(background image.Image, person *image.NRGBA) *image.RGBA {
	bounds := background.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), background, bounds.Min, draw.Src)

	pw, ph := float64(person.Bounds().Dx()), float64(person.Bounds().Dy())
	scale := personHeightShare * float64(height) / ph
	if pw*scale > personMaxWidth*float64(width) {
		scale = personMaxWidth * float64(width) / pw
	}
	tw, th := max(1, int(math.Round(pw*scale))), max(1, int(math.Round(ph*scale)))
	radius := outlineAtHeight * float64(height) / outlineRefHeight
	// A broad subject may run off the left edge, like a cropped press photo.
	x0 := int(math.Round(personCentreX*float64(width) - float64(tw)/2))
	y0 := height - th
	placed := image.Rect(x0, y0, x0+tw, y0+th)

	scaled := image.NewRGBA(image.Rect(0, 0, tw, th))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), person, person.Bounds(), draw.Src, nil)

	mask := make([]bool, width*height)
	for y := 0; y < th; y++ {
		for x := 0; x < tw; x++ {
			cx, cy := x0+x, y0+y
			if cx < 0 || cx >= width || cy < 0 || cy >= height {
				continue
			}
			mask[cy*width+cx] = scaled.Pix[y*scaled.Stride+x*4+3] > 127
		}
	}
	distance := distanceTransform(mask, width, height)
	outline := make([]float64, width*height)
	for i, d := range distance {
		outline[i] = clamp01(radius + 0.5 - d)
	}

	shadow := boxBlur(outline, width, height, int(math.Max(2, radius*0.8)))
	dx, dy := int(math.Round(radius*0.5)), int(math.Round(radius*0.7))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			sx, sy := x-dx, y-dy
			if sx < 0 || sy < 0 {
				continue
			}
			darken := 1 - shadowOpacity*shadow[sy*width+sx]
			i := y*canvas.Stride + x*4
			for c := 0; c < 3; c++ {
				canvas.Pix[i+c] = uint8(float64(canvas.Pix[i+c]) * darken)
			}
		}
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			a := outline[y*width+x]
			if a == 0 {
				continue
			}
			i := y*canvas.Stride + x*4
			for c := 0; c < 3; c++ {
				canvas.Pix[i+c] = uint8(float64(canvas.Pix[i+c])*(1-a) + 255*a)
			}
		}
	}
	draw.Draw(canvas, placed, scaled, image.Point{}, draw.Over)
	return canvas
}

// CropTo16x9 centre-crops an image to 16:9.
func CropTo16x9(src image.Image) image.Image {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	target := image.Rect(0, 0, width, height)
	if width*9 > height*16 {
		w := height * 16 / 9
		target = image.Rect((width-w)/2, 0, (width-w)/2+w, height)
	} else if width*9 < height*16 {
		h := width * 9 / 16
		target = image.Rect(0, (height-h)/2, width, (height-h)/2+h)
	}
	if target.Dx() == width && target.Dy() == height {
		return src
	}
	out := image.NewRGBA(image.Rect(0, 0, target.Dx(), target.Dy()))
	draw.Draw(out, out.Bounds(), src, bounds.Min.Add(target.Min), draw.Src)
	return out
}

func encodePNG(img image.Image) ([]byte, error) {
	var out bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// distanceTransform returns each pixel's Euclidean distance to the nearest
// true pixel of mask (Felzenszwalb and Huttenlocher).
func distanceTransform(mask []bool, width, height int) []float64 {
	const inf = 1e20
	grid := make([]float64, width*height)
	for i, on := range mask {
		if !on {
			grid[i] = inf
		}
	}
	size := max(width, height)
	f := make([]float64, size)
	d := make([]float64, size)
	v := make([]int, size)
	z := make([]float64, size+1)
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			f[y] = grid[y*width+x]
		}
		transform1D(f[:height], d[:height], v, z)
		for y := 0; y < height; y++ {
			grid[y*width+x] = d[y]
		}
	}
	for y := 0; y < height; y++ {
		row := grid[y*width : (y+1)*width]
		copy(f, row)
		transform1D(f[:width], d[:width], v, z)
		for x := 0; x < width; x++ {
			row[x] = math.Sqrt(d[x])
		}
	}
	return grid
}

func transform1D(f, d []float64, v []int, z []float64) {
	n := len(f)
	k := 0
	v[0] = 0
	z[0], z[1] = math.Inf(-1), math.Inf(1)
	for q := 1; q < n; q++ {
		s := ((f[q] + float64(q*q)) - (f[v[k]] + float64(v[k]*v[k]))) / float64(2*q-2*v[k])
		for s <= z[k] {
			k--
			s = ((f[q] + float64(q*q)) - (f[v[k]] + float64(v[k]*v[k]))) / float64(2*q-2*v[k])
		}
		k++
		v[k] = q
		z[k], z[k+1] = s, math.Inf(1)
	}
	k = 0
	for q := 0; q < n; q++ {
		for z[k+1] < float64(q) {
			k++
		}
		d[q] = float64((q-v[k])*(q-v[k])) + f[v[k]]
	}
}

// boxBlur approximates a Gaussian blur with three separable box passes.
func boxBlur(src []float64, width, height, radius int) []float64 {
	out := append([]float64(nil), src...)
	tmp := make([]float64, len(src))
	for pass := 0; pass < 3; pass++ {
		blurLine(out, tmp, width, height, radius, true)
		blurLine(tmp, out, width, height, radius, false)
	}
	return out
}

func blurLine(src, dst []float64, width, height, radius int, horizontal bool) {
	lines, length := height, width
	if !horizontal {
		lines, length = width, height
	}
	at := func(line, i int) int {
		if horizontal {
			return line*width + i
		}
		return i*width + line
	}
	window := float64(2*radius + 1)
	for line := 0; line < lines; line++ {
		sum := 0.0
		for i := -radius; i <= radius; i++ {
			if i >= 0 && i < length {
				sum += src[at(line, i)]
			}
		}
		for i := 0; i < length; i++ {
			dst[at(line, i)] = sum / window
			if out := i - radius; out >= 0 {
				sum -= src[at(line, out)]
			}
			if in := i + radius + 1; in < length {
				sum += src[at(line, in)]
			}
		}
	}
}

func clamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}
