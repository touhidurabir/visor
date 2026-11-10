package trayicon

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"strconv"

	"github.com/touhidurabir/visor/internal/monitor"
)

// Coloured returns the 44x44 PNG tray icon for Linux, whose panels don't
// recolour template images. It is the app icon's lens: the iridescent ring
// around dark glass with the mint pulse, which reads on a dark panel
// (GNOME's) and a light one (KDE's light themes) alike, so one icon serves
// both. Wails' Linux tray can't switch icons with the theme anyway.
//
// unread > 0 draws the count in a badge at the top right (1–9, then "9+"),
// in the health colour: red for an error, orange for a warning, blue
// otherwise. With nothing unread, a warning or an error is a dot there in
// its colour. Unknown health (not connected yet) greys the ring and drops
// the pulse.
func Coloured(h monitor.Health, unread int) []byte {
	b := badgeFor(h, unread)
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var r, g, bl, a float64 // premultiplied sums over the samples
			for sy := 0; sy < samples; sy++ {
				for sx := 0; sx < samples; sx++ {
					x := float64(px) + (float64(sx)+0.5)/samples
					y := float64(py) + (float64(sy)+0.5)/samples
					if c, ok := colouredAt(x, y, h, b); ok {
						r += float64(c.R)
						g += float64(c.G)
						bl += float64(c.B)
						a += 255
					}
				}
			}
			if a == 0 {
				continue
			}
			n := float64(samples * samples)
			img.SetNRGBA(px, py, color.NRGBA{
				R: uint8(math.Round(r * 255 / a)), G: uint8(math.Round(g * 255 / a)), B: uint8(math.Round(bl * 255 / a)),
				A: uint8(math.Round(a / n)),
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err) // encoding to memory cannot fail for a valid image
	}
	return buf.Bytes()
}

var (
	glass  = color.NRGBA{0x16, 0x20, 0x2E, 0xFF}
	mint   = color.NRGBA{0x7E, 0xE0, 0xC3, 0xFF}
	grey   = color.NRGBA{0x8A, 0x94, 0xA6, 0xFF}
	white  = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	red    = color.NRGBA{0xE5, 0x48, 0x4D, 0xFF}
	orange = color.NRGBA{0xE8, 0x8A, 0x0C, 0xFF}
	blue   = color.NRGBA{0x2F, 0x6F, 0xD8, 0xFF}
	// The ring's stops, clockwise from the top: the app icon's gradient.
	ringStops = []color.NRGBA{
		{0x19, 0xB5, 0xC8, 0xFF}, {0x5B, 0x7C, 0xFF, 0xFF}, {0x9B, 0x5C, 0xFF, 0xFF},
		{0xE8, 0x6B, 0xB8, 0xFF}, {0xFF, 0xB5, 0x47, 0xFF},
	}
)

// badge is what sits at the top right: nothing, a dot, or a count.
type badge struct {
	show       bool
	text       string // "" for a dot
	fill       color.NRGBA
	cx, cy     float64 // centre
	halfW, rad float64 // a pill: straight part's half width, end radius
}

const (
	countR    = 12.0 // the count badge's height is 2*countR: half the icon, so it reads at panel size
	digitCell = 3.6  // one pixel of the 3x5 digit font, in icon px
)

func badgeFor(h monitor.Health, unread int) badge {
	fill := blue
	switch h {
	case monitor.HealthError:
		fill = red
	case monitor.HealthWarning:
		fill = orange
	}
	switch {
	case unread > 0:
		text := strconv.Itoa(unread)
		if unread > 9 {
			text = "9+"
		}
		w := textWidth(text)
		halfW := math.Max(0, (w+4-2*countR)/2)
		return badge{show: true, text: text, fill: fill, cx: size - countR - halfW, cy: countR, halfW: halfW, rad: countR}
	case h == monitor.HealthError || h == monitor.HealthWarning:
		return badge{show: true, fill: fill, cx: badgeX, cy: badgeY, rad: badgeR}
	}
	return badge{}
}

// inPill reports whether (x, y) is within d of the badge's pill shape.
func (b badge) inPill(x, y, grow float64) bool {
	dx := math.Max(0, math.Abs(x-b.cx)-b.halfW)
	return dx*dx+(y-b.cy)*(y-b.cy) <= (b.rad+grow)*(b.rad+grow)
}

// colouredAt is the colour of the topmost layer at (x, y), if any.
func colouredAt(x, y float64, h monitor.Health, b badge) (color.NRGBA, bool) {
	if b.show {
		if b.inPill(x, y, 0) {
			if b.text != "" && inText(x, y, b) {
				return white, true
			}
			return b.fill, true
		}
		if b.inPill(x, y, badgeGap) {
			return color.NRGBA{}, false // transparent gap around the badge
		}
	}
	d := math.Sqrt(dist2(x, y, centre, centre))
	switch {
	case h != monitor.HealthUnknown && onPulse(x, y):
		return mint, true
	case math.Abs(d-ringR) <= ringHalf:
		if h == monitor.HealthUnknown {
			return grey, true
		}
		return ringColour(x, y), true
	case d < ringR:
		return glass, true
	}
	return color.NRGBA{}, false
}

// ringColour follows the gradient clockwise from the top of the ring.
func ringColour(x, y float64) color.NRGBA {
	turn := math.Atan2(x-centre, centre-y) / (2 * math.Pi) // 0 at the top, clockwise
	if turn < 0 {
		turn++
	}
	pos := turn * float64(len(ringStops)) // wraps back to the first stop
	i := int(pos) % len(ringStops)
	j := (i + 1) % len(ringStops)
	t := pos - math.Floor(pos)
	a, c := ringStops[i], ringStops[j]
	mix := func(p, q uint8) uint8 { return uint8(math.Round(float64(p) + (float64(q)-float64(p))*t)) }
	return color.NRGBA{mix(a.R, c.R), mix(a.G, c.G), mix(a.B, c.B), 0xFF}
}

// font is a 3x5 bitmap per character, rows top to bottom, bit 2 = left.
var font = map[rune][5]uint8{
	'0': {7, 5, 5, 5, 7}, '1': {2, 6, 2, 2, 7}, '2': {7, 1, 7, 4, 7}, '3': {7, 1, 3, 1, 7},
	'4': {5, 5, 7, 1, 1}, '5': {7, 4, 7, 1, 7}, '6': {7, 4, 7, 5, 7}, '7': {7, 1, 2, 2, 2},
	'8': {7, 5, 7, 5, 7}, '9': {7, 5, 7, 1, 7}, '+': {0, 2, 7, 2, 0},
}

const glyphGap = 1.0 // between characters, in icon px

func textWidth(s string) float64 {
	n := float64(len(s))
	return n*3*digitCell + (n-1)*glyphGap
}

// inText reports whether (x, y) is on the badge's text, centred in it.
func inText(x, y float64, b badge) bool {
	left := b.cx - textWidth(b.text)/2
	top := b.cy - 5*digitCell/2
	row := int(math.Floor((y - top) / digitCell))
	if row < 0 || row > 4 {
		return false
	}
	for _, ch := range b.text {
		if x >= left && x < left+3*digitCell {
			col := int((x - left) / digitCell)
			return font[ch][row]&(4>>col) != 0
		}
		left += 3*digitCell + glyphGap
	}
	return false
}
