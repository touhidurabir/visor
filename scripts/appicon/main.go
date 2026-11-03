// Command appicon draws Visor's app icon, the "visor lens" from the first
// launch screen (canvas board 1): an iridescent conic ring around a dark
// glass disc with a highlight. SVG has no conic gradient, so it is drawn
// pixel by pixel.
//
//	go run ./scripts/appicon
//
// writes build/appicon.png (the whole icon on a light tile, for .icns and
// Windows) and build/appicon.icon/Assets/lens.png (the lens alone, a layer
// for Icon Composer's Assets.car on macOS 26). Then run
// `wails3 task common:generate:icons`.
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

const size = 1024

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

// The board's conic-gradient(from 200deg, …): stops evenly round the circle.
var ring = []rgba{hex(0x19B5C8), hex(0x5B7CFF), hex(0x9B5CFF), hex(0xE86BB8), hex(0xFFB547), hex(0x7EE0C3), hex(0x19B5C8)}

func conic(dx, dy float64) rgba {
	deg := math.Atan2(dx, -dy) * 180 / math.Pi // clockwise from up, like CSS
	t := math.Mod(deg-200+720, 360) / 360 * float64(len(ring)-1)
	i := int(t)
	if i >= len(ring)-1 {
		return ring[len(ring)-1]
	}
	return lerp(ring[i], ring[i+1], t-float64(i))
}

// lens is the lens centred at (cx, cy) with radius r, or transparent.
// The proportions follow the board: lens 280 px, glass 252 px.
func lens(x, y, cx, cy, r float64) rgba {
	dx, dy := x-cx, y-cy
	d := math.Hypot(dx, dy)
	out := rgba{}
	// Soft shadow under the lens (box-shadow 0 30px 60px -20px).
	sd := math.Hypot(dx, dy-0.214*r)
	if sh := 0.45 * (1 - smoothstep(0.857*r-0.25*r, 0.857*r+0.35*r, sd)); sh > 0 {
		out = rgba{43.0 / 255, 58.0 / 255, 120.0 / 255, sh}
	}
	edge := clamp01(r - d + 0.5) // anti-aliased rim
	if edge <= 0 {
		return out
	}
	c := conic(dx, dy)
	// 1px-ish white rim (inset 0 0 0 1px rgba(255,255,255,.5)).
	rim := clamp01((d - (r - 0.012*r)) / (0.012 * r))
	c = over(rgba{1, 1, 1, 0.5 * rim}, c)

	g := 0.9 * r
	if gEdge := clamp01(g - d + 0.5); gEdge > 0 {
		glass := hex(0x14202E)
		// Purple glow from below: radial at (50%, 120%) of the glass box.
		pd := math.Hypot(dx, dy-1.4*g)
		glass = over(rgba{123.0 / 255, 92.0 / 255, 1, 0.55 * clamp01(1-pd/(1.43*g))}, glass)
		// Highlight: radial at (34%, 26%), white .38 fading out by 30%.
		hd := math.Hypot(dx+0.32*g, dy+0.48*g)
		glass = over(rgba{1, 1, 1, 0.38 * clamp01(1-hd/(0.595*g))}, glass)
		c = lerp(c, glass, gEdge)
	}
	c.a = edge
	return over(c, out)
}

// tile is the macOS icon tile: a rounded square with a light gradient and
// a soft shadow, or transparent outside it.
func tile(x, y float64) rgba {
	const inset, side, radius = 100.0, 824.0, 185.0
	// Signed distance to a rounded rectangle.
	qx := math.Abs(x-size/2) - (side/2 - radius)
	qy := math.Abs(y-size/2) - (side/2 - radius)
	dist := math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - radius
	shadowDist := func() float64 {
		qy := math.Abs(y-size/2-12) - (side/2 - radius)
		return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - radius
	}()
	out := rgba{0, 0, 0, 0.22 * (1 - smoothstep(-6, 28, shadowDist))}
	if a := clamp01(0.5 - dist); a > 0 {
		t := clamp01((y - inset) / side)
		bg := lerp(hex(0xFFFFFF), hex(0xE3E9F1), t)
		bg.a = a
		out = over(bg, out)
	}
	return out
}

func render(px func(x, y float64) rgba) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const ss = 3 // supersampling per axis
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var acc rgba
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					c := px(float64(x)+(float64(sx)+0.5)/ss, float64(y)+(float64(sy)+0.5)/ss)
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
		return over(lens(x, y, size/2, size/2-8, 290), tile(x, y))
	}))
	write("build/appicon.icon/Assets/lens.png", render(func(x, y float64) rgba {
		return lens(x, y, size/2, size/2-20, 420)
	}))
}
