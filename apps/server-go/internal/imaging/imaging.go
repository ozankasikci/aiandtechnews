// Package imaging decodes, resizes and re-encodes images for the publisher.
package imaging

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"

	"github.com/chai2010/webp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// SniffMIME ports sniffImageMimeType for the formats the publisher handles.
func SniffMIME(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg"
	case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}):
		return "image/png"
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif"
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

// Decompression-bomb limits: a small file can declare enormous dimensions,
// so the declared size is checked before any pixels are allocated.
const (
	MaxDimension = 8192
	MaxPixels    = 40_000_000
)

// Decode decodes a JPEG, PNG, GIF or WebP after checking the declared size
// against the decompression-bomb limits.
func Decode(data []byte) (image.Image, error) { return decode(data) }

func decode(data []byte) (image.Image, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image header: %w", err)
	}
	if config.Width > MaxDimension || config.Height > MaxDimension || config.Width*config.Height > MaxPixels {
		return nil, fmt.Errorf("image %dx%d is too large (max %d per side, %d pixels)", config.Width, config.Height, MaxDimension, MaxPixels)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	return img, nil
}

// FitJPEG resizes to fit inside maxEdge×maxEdge without enlarging, flattens
// transparency onto white and encodes JPEG (sharp resize fit:inside +
// withoutEnlargement + flatten white + jpeg). EXIF orientation is not applied;
// that is an accepted difference from sharp for generated and reference images.
func FitJPEG(data []byte, maxEdge, quality int) ([]byte, error) {
	src, err := decode(data)
	if err != nil {
		return nil, err
	}
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width > maxEdge || height > maxEdge {
		if width >= height {
			height = max(1, height*maxEdge/width)
			width = maxEdge
		} else {
			width = max(1, width*maxEdge/height)
			height = maxEdge
		}
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(canvas, canvas.Bounds(), src, bounds, draw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, canvas, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return out.Bytes(), nil
}

// EncodeWebP re-encodes an image as lossy WebP at the given quality
// (encodeFeatureImageAsWebp uses 82) without resizing.
func EncodeWebP(data []byte, quality int) ([]byte, int, int, error) {
	src, err := decode(data)
	if err != nil {
		return nil, 0, 0, err
	}
	var out bytes.Buffer
	if err := webp.Encode(&out, src, &webp.Options{Quality: float32(quality)}); err != nil {
		return nil, 0, 0, fmt.Errorf("encode webp: %w", err)
	}
	return out.Bytes(), src.Bounds().Dx(), src.Bounds().Dy(), nil
}
