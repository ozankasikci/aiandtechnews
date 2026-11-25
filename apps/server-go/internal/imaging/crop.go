package imaging

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
)

// CropRect is the largest rectangle of the given aspect ratio (width over
// height) inside a width by height image, placed so that the focus point is
// as close to its centre as the image's edges allow. The focus is given as
// fractions of the width and height; values outside 0..1 are clamped.
func CropRect(width, height int, ratio, focusX, focusY float64) image.Rectangle {
	if width <= 0 || height <= 0 || ratio <= 0 {
		return image.Rect(0, 0, max(width, 0), max(height, 0))
	}
	cropW, cropH := width, int(math.Round(float64(width)/ratio))
	if cropH > height {
		cropH = height
		cropW = int(math.Round(float64(height) * ratio))
	}
	cropW, cropH = max(1, min(cropW, width)), max(1, min(cropH, height))
	fraction := func(value float64) float64 { return math.Min(math.Max(value, 0), 1) }
	left := int(math.Round(fraction(focusX)*float64(width))) - cropW/2
	top := int(math.Round(fraction(focusY)*float64(height))) - cropH/2
	left, top = min(max(left, 0), width-cropW), min(max(top, 0), height-cropH)
	return image.Rect(left, top, left+cropW, top+cropH)
}

// CropToAspect crops an image to the aspect ratio around the focus point and
// returns it as PNG. An image that already has the ratio is returned as it
// came, byte for byte.
func CropToAspect(data []byte, ratio, focusX, focusY float64) ([]byte, error) {
	src, err := decode(data)
	if err != nil {
		return nil, err
	}
	bounds := src.Bounds()
	rect := CropRect(bounds.Dx(), bounds.Dy(), ratio, focusX, focusY)
	if rect.Dx() == bounds.Dx() && rect.Dy() == bounds.Dy() {
		return data, nil
	}
	canvas := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(canvas, canvas.Bounds(), src, bounds.Min.Add(rect.Min), draw.Src)
	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		return nil, fmt.Errorf("encode cropped image: %w", err)
	}
	return out.Bytes(), nil
}
