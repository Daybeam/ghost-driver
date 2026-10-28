package vision

import (
	"bytes"
	"image"
	"image/color"
	_ "image/png"
	"math"
)

// RefinePoint takes a screenshot and a coarse coordinate, then zooms in and
// uses edge/contrast detection to find the exact center of a UI element.
// This implements the "Pixel-level refinement" from Phase 3.
func RefinePoint(pngData []byte, coarse Coord, viewportW, viewportH float64) Coord {
	img, _, err := image.Decode(bytes.NewReader(pngData))
	if err != nil {
		return coarse // Fallback to coarse if decoding fails
	}

	bounds := img.Bounds()
	imgW := float64(bounds.Dx())
	imgH := float64(bounds.Dy())

	// Scale coarse (0-1000) to pixel coordinates
	pxX := (coarse.X / 1000.0) * imgW
	pxY := (coarse.Y / 1000.0) * imgH

	// Look in a 60x60 window around the point
	windowSize := 30
	minX := int(math.Max(0, pxX-float64(windowSize)))
	maxX := int(math.Min(imgW-1, pxX+float64(windowSize)))
	minY := int(math.Max(0, pxY-float64(windowSize)))
	maxY := int(math.Min(imgH-1, pxY+float64(windowSize)))

	// Find the pixel with the highest local contrast (likely an edge or icon detail)
	bestX, bestY := pxX, pxY
	maxContrast := -1.0

	for y := minY + 1; y < maxY-1; y++ {
		for x := minX + 1; x < maxX-1; x++ {
			c := img.At(x, y)
			r, g, b, _ := c.RGBA()
			lum := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)

			// Simple Laplacian-ish contrast check
			neighbors := []color.Color{
				img.At(x-1, y), img.At(x+1, y),
				img.At(x, y-1), img.At(x, y+1),
			}

			localContrast := 0.0
			for _, n := range neighbors {
				nr, ng, nb, _ := n.RGBA()
				nlum := 0.299*float64(nr) + 0.587*float64(ng) + 0.114*float64(nb)
				localContrast += math.Abs(lum - nlum)
			}

			if localContrast > maxContrast {
				maxContrast = localContrast
				bestX, bestY = float64(x), float64(y)
			}
		}
	}

	// Convert back to normalized 0-1000 for consistency
	return Coord{
		X: (bestX / imgW) * 1000.0,
		Y: (bestY / imgH) * 1000.0,
	}
}
