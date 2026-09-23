package imaging_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
)

func pngImage(t *testing.T, width, height int, transparent bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			alpha := uint8(255)
			if transparent {
				alpha = 0
			}
			img.Set(x, y, color.NRGBA{R: 200, G: 30, B: 30, A: alpha})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestSniffMIME(t *testing.T) {
	if got := imaging.SniffMIME(pngImage(t, 2, 2, false)); got != "image/png" {
		t.Fatalf("png = %q", got)
	}
	if got := imaging.SniffMIME([]byte("RIFF\x00\x00\x00\x00WEBPVP8 ")); got != "image/webp" {
		t.Fatalf("webp = %q", got)
	}
	if got := imaging.SniffMIME([]byte{0xff, 0xd8, 0xff, 0xe0}); got != "image/jpeg" {
		t.Fatalf("jpeg = %q", got)
	}
	if got := imaging.SniffMIME([]byte("<svg")); got != "" {
		t.Fatalf("svg = %q", got)
	}
}

func TestFitJPEGDownscalesAndFlattens(t *testing.T) {
	out, err := imaging.FitJPEG(pngImage(t, 2048, 1024, true), 1024, 85)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 1024 || b.Dy() != 512 {
		t.Fatalf("bounds = %v", b)
	}
	r, g, b, _ := img.At(10, 10).RGBA()
	if r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
		t.Fatalf("transparent pixels should flatten to white, got %d %d %d", r>>8, g>>8, b>>8)
	}

	small, err := imaging.FitJPEG(pngImage(t, 300, 200, false), 1024, 85)
	if err != nil {
		t.Fatal(err)
	}
	if img, _ := jpeg.Decode(bytes.NewReader(small)); img.Bounds().Dx() != 300 {
		t.Fatalf("must not enlarge: %v", img.Bounds())
	}
}

func TestEncodeWebP(t *testing.T) {
	out, width, height, err := imaging.EncodeWebP(pngImage(t, 640, 360, false), 82)
	if err != nil {
		t.Fatal(err)
	}
	if imaging.SniffMIME(out) != "image/webp" || width != 640 || height != 360 {
		t.Fatalf("mime=%q %dx%d", imaging.SniffMIME(out), width, height)
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	if _, err := imaging.FitJPEG([]byte("not an image"), 1024, 85); err == nil {
		t.Fatal("garbage should fail")
	}
}
