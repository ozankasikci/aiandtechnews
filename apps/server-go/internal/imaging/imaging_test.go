package imaging_test

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
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

// pngHeader returns just a PNG signature and IHDR chunk declaring the given
// size: enough for image.DecodeConfig, with no pixel data at all.
func pngHeader(width, height uint32) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8], ihdr[9] = 8, 6 // 8-bit RGBA
	chunk := append([]byte("IHDR"), ihdr...)
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(ihdr)))
	buf.Write(chunk)
	_ = binary.Write(&buf, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return buf.Bytes()
}

func TestDecodeRejectsOversizedDimensions(t *testing.T) {
	for name, data := range map[string][]byte{
		"50000x50000":     pngHeader(50000, 50000),
		"wide 9000x10":    pngHeader(9000, 10),
		"too many pixels": pngHeader(8000, 8000),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := imaging.FitJPEG(data, 1024, 80); err == nil || !strings.Contains(err.Error(), "too large") {
				t.Fatalf("FitJPEG err = %v, want too large", err)
			}
			if _, _, _, err := imaging.EncodeWebP(data, 82); err == nil || !strings.Contains(err.Error(), "too large") {
				t.Fatalf("EncodeWebP err = %v, want too large", err)
			}
		})
	}
}
