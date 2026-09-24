package illustration_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
)

func encodeNRGBA(t *testing.T, img *image.NRGBA) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func cutoutWith(t *testing.T, width, height int, opaque func(x, y int) bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if opaque(x, y) {
				img.Set(x, y, color.NRGBA{R: 200, G: 40, B: 40, A: 255})
			}
		}
	}
	return encodeNRGBA(t, img)
}

func TestPrepareCutoutCropsBottomAndSubject(t *testing.T) {
	data := cutoutWith(t, 200, 100, func(x, y int) bool { return x >= 60 && x < 140 && y >= 10 })
	person, err := illustration.PrepareCutout(data)
	if err != nil {
		t.Fatal(err)
	}
	// Bottom 11% (11 px) dropped, then cropped to the subject: 80 x (89-10).
	if b := person.Bounds(); b.Dx() != 80 || b.Dy() != 79 {
		t.Fatalf("subject = %v", b)
	}
}

func TestPrepareCutoutRejectsPoorMasks(t *testing.T) {
	cases := map[string]func(x, y int) bool{
		"tiny":        func(x, y int) bool { return x >= 95 && x < 105 && y >= 40 && y < 50 },
		"whole frame": func(x, y int) bool { return true },
		"short":       func(x, y int) bool { return x >= 40 && x < 160 && y >= 60 && y < 80 },
		"head cut":    func(x, y int) bool { return x >= 60 && x < 140 },
		"side cut":    func(x, y int) bool { return x < 80 && y >= 10 },
		"empty":       func(x, y int) bool { return false },
	}
	for name, opaque := range cases {
		if _, err := illustration.PrepareCutout(cutoutWith(t, 200, 100, opaque)); !errors.Is(err, illustration.ErrPoorCutout) {
			t.Errorf("%s: err = %v, want ErrPoorCutout", name, err)
		}
	}
	if _, err := illustration.PrepareCutout([]byte("not a png")); !errors.Is(err, illustration.ErrPoorCutout) {
		t.Errorf("garbage: err = %v", err)
	}
}

func TestCompositePlacesPersonWithStickerOutline(t *testing.T) {
	const width, height = 1672, 941
	background := image.NewRGBA(image.Rect(0, 0, width, height))
	blue := color.RGBA{R: 20, G: 60, B: 160, A: 255}
	for i := 0; i < len(background.Pix); i += 4 {
		background.Pix[i], background.Pix[i+1], background.Pix[i+2], background.Pix[i+3] = blue.R, blue.G, blue.B, 255
	}
	person := image.NewNRGBA(image.Rect(0, 0, 300, 500))
	skin := color.NRGBA{R: 200, G: 40, B: 40, A: 255}
	for y := 0; y < 500; y++ {
		for x := 0; x < 300; x++ {
			person.Set(x, y, skin)
		}
	}
	out := illustration.Composite(background, person)

	// 90% of the height, bottom-anchored, centred at 24% of the width.
	th := int(math.Round(0.9 * height))
	tw := int(math.Round(300 * float64(th) / 500))
	top := height - th
	w := width
	centre := int(0.24 * float64(w))
	left := centre - tw/2
	at := func(x, y int) color.RGBA { return out.RGBAAt(x, y) }
	if got := at(centre, height-2); got.R != 200 || got.G != 40 || got.B != 40 {
		t.Fatalf("person centre bottom = %v", got)
	}
	if got := at(centre, top+5); got.R != 200 {
		t.Fatalf("person top = %v", got)
	}
	// White outline about 14px outside the person, on its left and top.
	if got := at(left-7, height/2); got.R < 240 || got.G < 240 || got.B < 240 {
		t.Fatalf("left outline = %v", got)
	}
	if got := at(centre, top-7); got.R < 240 || got.G < 240 || got.B < 240 {
		t.Fatalf("top outline = %v", got)
	}
	// Beyond the outline: background, darkened a little by the shadow on the
	// lower right, untouched far away.
	if got := at(left-40, top-40); got != blue {
		t.Fatalf("far background = %v", got)
	}
	if got := at(left+tw+16, height/2); got.B >= blue.B || got.B < 90 {
		t.Fatalf("shadow = %v, want slightly darker blue", got)
	}
	if got := at(width-10, 10); got != blue {
		t.Fatalf("right side = %v", got)
	}
}

func TestCompositeLimitsWidePeople(t *testing.T) {
	background := image.NewRGBA(image.Rect(0, 0, 1600, 900))
	wide := image.NewNRGBA(image.Rect(0, 0, 1000, 400))
	for i := 3; i < len(wide.Pix); i += 4 {
		wide.Pix[i] = 255
	}
	out := illustration.Composite(background, wide)
	// Never wider than 46% of the canvas, so the right side stays clear.
	if got := out.RGBAAt(int(0.24*1600)+int(0.46*1600)/2+20, 899); got.A != 0 && (got.R|got.G|got.B) != 0 {
		t.Fatalf("pixel right of a wide person = %v", got)
	}
}

func TestCropTo16x9(t *testing.T) {
	for _, tc := range []struct{ w, h, wantW, wantH int }{{1600, 900, 1600, 900}, {1536, 1024, 1536, 864}, {2000, 900, 1600, 900}} {
		got := illustration.CropTo16x9(image.NewRGBA(image.Rect(0, 0, tc.w, tc.h))).Bounds()
		if got.Dx() != tc.wantW || got.Dy() != tc.wantH {
			t.Errorf("%dx%d -> %v, want %dx%d", tc.w, tc.h, got, tc.wantW, tc.wantH)
		}
	}
}
