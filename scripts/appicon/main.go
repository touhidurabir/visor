// Command appicon draws Visor's app icon, option C "Pulse" from the icon
// sheet: an iridescent ring around a dark glass disc with a mint pulse line,
// on a light rounded tile. It follows the source SVG's 1024 grid exactly
// (geometry, gradient stops and colours) and draws it pixel by pixel, so it
// needs nothing outside the standard library.
//
//	go run ./scripts/appicon
//
// writes build/appicon.png (the whole icon on the light tile, for .icns),
// build/linux/icons/<n>.png (the same at the Linux hicolor sizes) and
// build/appicon.icon/Assets/lens.png (the ring, glass and pulse alone, a
// layer for Icon Composer's Assets.car on macOS 26). Then run
// `wails3 task common:generate:icons`.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

const size = 1024

// Icon Composer scales the lens.png layer by this much (build/appicon.icon's
// icon.json); the layer is drawn 1/layerScale larger so it lands on the grid.
const layerScale = 0.72

type rgba struct{ r, g, b, a float64 } // straight alpha, 0..1

func hex(v uint32) rgba {
	return rgba{float64(v>>16&0xff) / 255, float64(v>>8&0xff) / 255, float64(v&0xff) / 255, 1}
}

// over composites top over bottom.
func over(top, bottom rgba) rgba {
	a := top.a + bottom.a*(1-top.a)
	if a == 0 {
		return rgba{}
	}
	mix := func(t, b float64) float64 { return (t*top.a + b*bottom.a*(1-top.a)) / a }
	return rgba{mix(top.r, bottom.r), mix(top.g, bottom.g), mix(top.b, bottom.b), a}
}

func lerp(a, b rgba, t float64) rgba {
	return rgba{a.r + (b.r-a.r)*t, a.g + (b.g-a.g)*t, a.b + (b.b-a.b)*t, a.a + (b.a-a.a)*t}
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func smoothstep(e0, e1, x float64) float64 {
	t := clamp01((x - e0) / (e1 - e0))
	return t * t * (3 - 2*t)
}

// withAlpha returns c with its alpha multiplied by a (a coverage value).
func withAlpha(c rgba, a float64) rgba {
	c.a *= a
	return c
}

// coverage turns a signed distance (negative inside) into an anti-aliased
// alpha over about one sample.
func coverage(dist float64) float64 { return clamp01(0.5 - dist) }

type stop struct {
	at float64
	c  rgba
}

// gradient samples an SVG gradient's stops at t (spreadMethod pad), mixing
// in sRGB as SVG does.
func gradient(stops []stop, t float64) rgba {
	if t <= stops[0].at {
		return stops[0].c
	}
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].at {
			a, b := stops[i-1], stops[i]
			return lerp(a.c, b.c, (t-a.at)/(b.at-a.at))
		}
	}
	return stops[len(stops)-1].c
}

// The SVG's three gradients.
var (
	// bg: linear, top to bottom of the tile.
	bgStops = []stop{{0, hex(0xFAFBFD)}, {1, hex(0xE3E9F1)}}
	// iri: linear, top left to bottom right of the ring's box.
	iriStops = []stop{{0, hex(0x19B5C8)}, {.28, hex(0x5B7CFF)}, {.5, hex(0x9B5CFF)}, {.72, hex(0xE86BB8)}, {1, hex(0xFFB547)}}
	// glass: radial, centre (.38, .3) of the glass's box, radius .9 of it.
	glassStops = []stop{{0, hex(0x4A5568)}, {.45, hex(0x16202E)}, {1, hex(0x2E2A6B)}}
	mint       = hex(0x7EE0C3)
)

// The geometry, in the SVG's 1024 grid.
const (
	centre     = 512.0
	ringR      = 296.0
	glassR     = 262.0
	pulseWidth = 30.0
)

// pulse is the path "M290 520 H420 L462 430 L522 620 L566 490 L598 520 H734".
var pulse = [][2]float64{{290, 520}, {420, 520}, {462, 430}, {522, 620}, {566, 490}, {598, 520}, {734, 520}}

// segDist is the distance from (x, y) to the segment a-b.
func segDist(x, y float64, a, b [2]float64) float64 {
	dx, dy := b[0]-a[0], b[1]-a[1]
	t := clamp01(((x-a[0])*dx + (y-a[1])*dy) / (dx*dx + dy*dy))
	return math.Hypot(x-a[0]-t*dx, y-a[1]-t*dy)
}

// pulseDist is the distance to the pulse's centre line. A stroke with round
// caps and joins covers exactly the points within half its width of it.
func pulseDist(x, y float64) float64 {
	d := math.Inf(1)
	for i := 1; i < len(pulse); i++ {
		d = math.Min(d, segDist(x, y, pulse[i-1], pulse[i]))
	}
	return d
}

// lens is the ring, glass and pulse, or transparent outside the ring.
func lens(x, y float64) rgba {
	d := math.Hypot(x-centre, y-centre)
	ringCov := coverage(d - ringR)
	if ringCov <= 0 {
		return rgba{}
	}
	// iri spans the ring's bounding box along its diagonal.
	box := centre - ringR
	t := ((x - box) + (y - box)) / (4 * ringR)
	out := withAlpha(gradient(iriStops, t), ringCov)

	if glassCov := coverage(d - glassR); glassCov > 0 {
		gx, gy := centre-glassR+0.38*2*glassR, centre-glassR+0.3*2*glassR
		g := gradient(glassStops, math.Hypot(x-gx, y-gy)/(0.9*2*glassR))
		out = over(withAlpha(g, glassCov), out)
	}
	if pc := coverage(pulseDist(x, y) - pulseWidth/2); pc > 0 {
		out = over(withAlpha(mint, pc), out)
	}
	return out
}

// tile is the macOS icon tile: the SVG's rounded square (x/y 100, 824,
// rx 185) with its light gradient, over a soft shadow, or transparent
// outside it.
func tile(x, y float64) rgba {
	const inset, side, radius = 100.0, 824.0, 185.0
	// Signed distance to a rounded rectangle centred at (cx, cy).
	rounded := func(cx, cy float64) float64 {
		qx := math.Abs(x-cx) - (side/2 - radius)
		qy := math.Abs(y-cy) - (side/2 - radius)
		return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - radius
	}
	out := rgba{0, 0, 0, 0.22 * (1 - smoothstep(-6, 28, rounded(size/2, size/2+12)))}
	if a := coverage(rounded(size/2, size/2)); a > 0 {
		out = over(withAlpha(gradient(bgStops, (y-inset)/side), a), out)
	}
	return out
}

func render(px func(x, y float64) rgba) *image.NRGBA { return renderAt(px, size) }

// renderAt draws the 1024 grid at n x n pixels, with enough sub-samples
// that small sizes stay smooth.
func renderAt(px func(x, y float64) rgba, n int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	ss := max(4, size/n/4) // supersampling per axis
	scale := float64(size) / float64(n)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			var acc rgba
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					c := px((float64(x)+(float64(sx)+0.5)/float64(ss))*scale, (float64(y)+(float64(sy)+0.5)/float64(ss))*scale)
					acc.r += c.r * c.a
					acc.g += c.g * c.a
					acc.b += c.b * c.a
					acc.a += c.a
				}
			}
			n := float64(ss * ss)
			a := acc.a / n
			if a == 0 {
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(math.Round(acc.r / acc.a * 255)),
				G: uint8(math.Round(acc.g / acc.a * 255)),
				B: uint8(math.Round(acc.b / acc.a * 255)),
				A: uint8(math.Round(a * 255)),
			})
		}
	}
	return img
}

// linuxSizes are the hicolor icon sizes the Linux packages install.
var linuxSizes = []int{16, 24, 32, 48, 64, 128, 256, 512}

func write(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
	log.Println("wrote", path)
}

func main() {
	write("build/appicon.png", render(func(x, y float64) rgba {
		return over(lens(x, y), tile(x, y))
	}))
	// Drawn larger by 1/layerScale, so Icon Composer's scale puts it back
	// on the SVG's grid.
	write("build/appicon.icon/Assets/lens.png", render(func(x, y float64) rgba {
		return lens(centre+(x-centre)*layerScale, centre+(y-centre)*layerScale)
	}))
	// Linux: the same icon at each hicolor size, for the .deb, the .rpm and
	// the AppImage (build/linux/icons/<n>.png).
	if err := os.MkdirAll("build/linux/icons", 0o755); err != nil {
		log.Fatal(err)
	}
	for _, n := range linuxSizes {
		write(fmt.Sprintf("build/linux/icons/%d.png", n), renderAt(func(x, y float64) rgba {
			return over(lens(x, y), tile(x, y))
		}, n))
	}
}
