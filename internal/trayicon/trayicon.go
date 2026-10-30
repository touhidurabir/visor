// Package trayicon draws the menu-bar template icons for each health state.
// It uses only the standard library so it stays reusable outside Wails.
package trayicon

import (
	"bytes"
	"image"
	"image/color"
	"image/png"

	"github.com/touhidurabir/visor/internal/monitor"
)

const (
	size    = 44 // 22pt at 2x
	samples = 4  // supersampling per axis, per pixel
	centre  = 22.0

	outerR = 18.0
	innerR = 13.0
	discR  = 6.0

	badgeX, badgeY = 36.0, 8.0
	badgeR         = 6.0
	badgeGap       = 2.0
)

// Template returns a 44x44 PNG, black on transparent, for use as a macOS
// template image. Every health value yields a distinct icon: a lens outline
// (ring), plus a centre disc for OK, a left half disc for Warning, the OK
// icon with a badge dot at the top right for Error, and the ring alone for
// Unknown.
func Template(h monitor.Health) []byte {
	shape := func(x, y float64) bool { return ring(x, y) }
	switch h {
	case monitor.HealthOK:
		shape = func(x, y float64) bool { return ring(x, y) || disc(x, y) }
	case monitor.HealthWarning:
		// Left half: the half of the disc with x < centre.
		shape = func(x, y float64) bool { return ring(x, y) || (disc(x, y) && x < centre) }
	case monitor.HealthError:
		shape = func(x, y float64) bool {
			if dist2(x, y, badgeX, badgeY) <= badgeR*badgeR {
				return true
			}
			r := badgeR + badgeGap
			if dist2(x, y, badgeX, badgeY) <= r*r {
				return false // transparent gap around the badge
			}
			return ring(x, y) || disc(x, y)
		}
	case monitor.HealthUnknown:
	}
	return render(shape)
}

func dist2(x, y, cx, cy float64) float64 {
	return (x-cx)*(x-cx) + (y-cy)*(y-cy)
}

func ring(x, y float64) bool {
	d := dist2(x, y, centre, centre)
	return d <= outerR*outerR && d >= innerR*innerR
}

func disc(x, y float64) bool { return dist2(x, y, centre, centre) <= discR*discR }

// render turns a point-membership function into an anti-aliased PNG: each
// pixel's alpha is the fraction of its sub-samples that fall inside the shape.
func render(inside func(x, y float64) bool) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			n := 0
			for sy := 0; sy < samples; sy++ {
				for sx := 0; sx < samples; sx++ {
					x := float64(px) + (float64(sx)+0.5)/samples
					y := float64(py) + (float64(sy)+0.5)/samples
					if inside(x, y) {
						n++
					}
				}
			}
			if n > 0 {
				img.SetNRGBA(px, py, color.NRGBA{A: uint8((n*255 + samples*samples/2) / (samples * samples))})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err) // encoding to memory cannot fail for a valid image
	}
	return buf.Bytes()
}
