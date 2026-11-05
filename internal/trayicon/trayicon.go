// Package trayicon draws the menu-bar template icons for each health state.
// It uses only the standard library so it stays reusable outside Wails.
package trayicon

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"

	"github.com/touhidurabir/visor/internal/monitor"
)

const (
	size    = 44 // 22pt at 2x
	samples = 4  // supersampling per axis, per pixel
	centre  = 22.0

	// The glyph is the app icon's "Pulse" drawn on an 18x18 grid (a ring of
	// radius 7.2, stroke 1.6, around a pulse line, stroke 1.5), scaled to
	// the 44 px canvas.
	unit      = size / 18.0
	ringR     = 7.2 * unit
	ringHalf  = 0.8 * unit
	pulseHalf = 0.75 * unit

	badgeX, badgeY = 36.0, 8.0
	badgeR         = 6.0
	badgeGap       = 2.0
	badgeInnerR    = 4.0 // the Warning badge is a ring from here to badgeR
)

// pulse is the glyph's path "M4.3 9.2 H7 L8 7 L9.6 11.6 L10.7 8.6 L11.4 9.2
// H13.7" on the 18 grid; stroked with round caps and joins.
var pulse = [][2]float64{{4.3, 9.2}, {7, 9.2}, {8, 7}, {9.6, 11.6}, {10.7, 8.6}, {11.4, 9.2}, {13.7, 9.2}}

// Template returns a 44x44 PNG, black on transparent, for use as a macOS
// template image. Every health value yields a distinct icon: a ring around
// a pulse line for OK, the OK icon with a hollow badge ring at the top right
// for Warning and a filled badge dot there for Error, and the ring alone for
// Unknown.
func Template(h monitor.Health) []byte {
	shape := func(x, y float64) bool { return ring(x, y) }
	switch h {
	case monitor.HealthOK:
		shape = func(x, y float64) bool { return ring(x, y) || onPulse(x, y) }
	case monitor.HealthWarning:
		shape = withBadge(func(x, y float64) bool { return dist2(x, y, badgeX, badgeY) >= badgeInnerR*badgeInnerR })
	case monitor.HealthError:
		shape = withBadge(func(float64, float64) bool { return true })
	case monitor.HealthUnknown:
	}
	return render(shape)
}

// withBadge is the OK glyph with a badge at the top right, cut out of the
// glyph with a transparent gap. inBadge picks which points of the badge disc
// are inked: all of them (filled) or an outer ring (hollow).
func withBadge(inBadge func(x, y float64) bool) func(x, y float64) bool {
	return func(x, y float64) bool {
		if dist2(x, y, badgeX, badgeY) <= badgeR*badgeR {
			return inBadge(x, y)
		}
		r := badgeR + badgeGap
		if dist2(x, y, badgeX, badgeY) <= r*r {
			return false // transparent gap around the badge
		}
		return ring(x, y) || onPulse(x, y)
	}
}

func dist2(x, y, cx, cy float64) float64 {
	return (x-cx)*(x-cx) + (y-cy)*(y-cy)
}

func ring(x, y float64) bool {
	return math.Abs(math.Sqrt(dist2(x, y, centre, centre))-ringR) <= ringHalf
}

// onPulse reports whether (x, y) is under the pulse stroke: within half the
// stroke width of one of its segments, which is what round caps and joins
// cover.
func onPulse(x, y float64) bool {
	for i := 1; i < len(pulse); i++ {
		ax, ay := pulse[i-1][0]*unit, pulse[i-1][1]*unit
		bx, by := pulse[i][0]*unit, pulse[i][1]*unit
		dx, dy := bx-ax, by-ay
		t := math.Max(0, math.Min(1, ((x-ax)*dx+(y-ay)*dy)/(dx*dx+dy*dy)))
		if dist2(x, y, ax+t*dx, ay+t*dy) <= pulseHalf*pulseHalf {
			return true
		}
	}
	return false
}

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
