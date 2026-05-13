package framestream

import (
	"bytes"
	"image"
	_ "image/png"
)

// changeDetector compares consecutive frames using downscaled grayscale thumbnails.
type changeDetector struct {
	thumbSize int
	lastThumb []uint8
	hasLast   bool
}

func newChangeDetector(thumbSize int) *changeDetector {
	return &changeDetector{thumbSize: thumbSize}
}

// Score returns a change score between 0 (identical) and 1 (completely different).
// First frame always returns 1.0.
func (d *changeDetector) Score(pngData []byte) float64 {
	img, _, err := image.Decode(bytes.NewReader(pngData))
	if err != nil {
		return 1.0
	}

	thumb := toGrayscaleThumb(img, d.thumbSize)

	if !d.hasLast {
		d.lastThumb = thumb
		d.hasLast = true
		return 1.0
	}

	score := pixelDiff(d.lastThumb, thumb)
	d.lastThumb = thumb
	return score
}

// toGrayscaleThumb downscales an image to a NxN grayscale thumbnail
// by sampling every Nth pixel.
func toGrayscaleThumb(img image.Image, size int) []uint8 {
	thumb := make([]uint8, size*size)
	bounds := img.Bounds()

	for y := 0; y < size; y++ {
		srcY := y * bounds.Dy() / size
		for x := 0; x < size; x++ {
			srcX := x * bounds.Dx() / size
			r, g, b, _ := img.At(srcX, srcY).RGBA()
			// Luminance: 0.299*R + 0.587*G + 0.114*B
			gray := uint8((299*uint16(r>>8) + 587*uint16(g>>8) + 114*uint16(b>>8)) / 1000)
			thumb[y*size+x] = gray
		}
	}
	return thumb
}

// pixelDiff computes mean absolute difference between two grayscale thumbnails.
func pixelDiff(a, b []uint8) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 1.0
	}
	totalDiff := 0
	for i := range a {
		if a[i] > b[i] {
			totalDiff += int(a[i] - b[i])
		} else {
			totalDiff += int(b[i] - a[i])
		}
	}
	return float64(totalDiff) / float64(len(a)) / 255.0
}
