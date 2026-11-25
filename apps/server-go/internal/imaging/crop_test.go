package imaging_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
)

func TestCropRectKeepsTheFocusInFrame(t *testing.T) {
	ratio := 16.0 / 9.0
	for _, tc := range []struct {
		name   string
		w, h   int
		fx, fy float64
	}{
		{"portrait, focus near the top", 1200, 1600, 0.5, 0.05},
		{"portrait, focus near the bottom", 1200, 1600, 0.5, 0.98},
		{"panorama, focus at the left edge", 3000, 1000, 0.02, 0.5},
		{"panorama, focus at the right edge", 3000, 1000, 0.99, 0.5},
		{"centre", 1600, 1200, 0.5, 0.5},
		{"out of range", 1600, 1200, -1, 2},
	} {
		rect := imaging.CropRect(tc.w, tc.h, ratio, tc.fx, tc.fy)
		if rect.Min.X < 0 || rect.Min.Y < 0 || rect.Max.X > tc.w || rect.Max.Y > tc.h {
			t.Errorf("%s: %v leaves the %dx%d image", tc.name, rect, tc.w, tc.h)
		}
		if got := float64(rect.Dx()) / float64(rect.Dy()); math.Abs(got-ratio) > 0.01 {
			t.Errorf("%s: aspect %.3f, want %.3f", tc.name, got, ratio)
		}
		fx, fy := math.Min(math.Max(tc.fx, 0), 1), math.Min(math.Max(tc.fy, 0), 1)
		focus := image.Pt(min(int(fx*float64(tc.w)), tc.w-1), min(int(fy*float64(tc.h)), tc.h-1))
		if !focus.In(rect) {
			t.Errorf("%s: focus %v is outside %v", tc.name, focus, rect)
		}
	}
}

// halves is a square image whose top half is red and bottom half is blue.
func halves(t *testing.T, size int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		fill := color.RGBA{R: 220, A: 255}
		if y >= size/2 {
			fill = color.RGBA{B: 220, A: 255}
		}
		for x := 0; x < size; x++ {
			img.Set(x, y, fill)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestCropToAspectCropsAroundTheFocus(t *testing.T) {
	for name, tc := range map[string]struct {
		focusY   float64
		wantBlue bool
	}{"top": {0.1, false}, "bottom": {0.9, true}} {
		cropped, err := imaging.CropToAspect(halves(t, 400), 16.0/9.0, 0.5, tc.focusY)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(cropped))
		if err != nil {
			t.Fatal(err)
		}
		if b := img.Bounds(); b.Dx() != 400 || b.Dy() != 225 {
			t.Fatalf("%s: size %dx%d, want 400x225", name, b.Dx(), b.Dy())
		}
		_, _, blue, _ := img.At(200, 112).RGBA()
		if (blue > 30000) != tc.wantBlue {
			t.Errorf("%s: centre pixel blue=%d, want blue %t", name, blue, tc.wantBlue)
		}
	}
}

func TestCropToAspectLeavesAMatchingImageAlone(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1600, 900))
	for y := 0; y < 900; y++ {
		for x := 0; x < 1600; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: uint8((x + y) % 256), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	in := buf.Bytes()
	out, err := imaging.CropToAspect(in, 16.0/9.0, 0.2, 0.8)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, in) {
		t.Fatal("a 16:9 image must be returned unchanged")
	}
	if !bytes.HasPrefix(out, []byte{0xff, 0xd8, 0xff}) {
		t.Fatal("the unchanged image must still be a JPEG")
	}
}

func TestCropRectCentresOnTheFocus(t *testing.T) {
	ratio := 16.0 / 9.0
	for _, tc := range []struct {
		name   string
		w, h   int
		fx, fy float64
		want   image.Rectangle
	}{
		{"centre of a 4:3 image", 1600, 1200, 0.5, 0.5, image.Rect(0, 150, 1600, 1050)},
		{"portrait, focus above centre", 1200, 1600, 0.5, 0.4, image.Rect(0, 303, 1200, 978)},
		{"panorama, focus left of centre", 3000, 1000, 0.3, 0.5, image.Rect(11, 0, 1789, 1000)},
		{"NaN focus is the centre", 1600, 1200, math.NaN(), math.NaN(), image.Rect(0, 150, 1600, 1050)},
	} {
		if got := imaging.CropRect(tc.w, tc.h, ratio, tc.fx, tc.fy); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
