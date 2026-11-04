package trayicon

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/touhidurabir/visor/internal/monitor"
)

var allHealth = []monitor.Health{monitor.HealthOK, monitor.HealthWarning, monitor.HealthError, monitor.HealthUnknown}

func decode(t *testing.T, h monitor.Health) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(Template(h)))
	if err != nil {
		t.Fatalf("decode %v: %v", h, err)
	}
	return img
}

func TestTemplateDecodes(t *testing.T) {
	for _, h := range allHealth {
		b := decode(t, h).Bounds()
		if b.Dx() != 44 || b.Dy() != 44 {
			t.Errorf("%v: size %dx%d, want 44x44", h, b.Dx(), b.Dy())
		}
	}
}

func TestTemplateDiffers(t *testing.T) {
	for i, a := range allHealth {
		for _, b := range allHealth[i+1:] {
			if bytes.Equal(Template(a), Template(b)) {
				t.Errorf("%v and %v render identically", a, b)
			}
		}
	}
}

func TestErrorBadgeOpaque(t *testing.T) {
	_, _, _, a := decode(t, monitor.HealthError).At(36, 8).RGBA()
	if a>>8 <= 200 {
		t.Errorf("error badge alpha = %d, want > 200", a>>8)
	}
	// (36, 8) touches the ring's outer edge; (38, 6) is in the badge but
	// clear of the ring.
	if a := alpha(decode(t, monitor.HealthError), 38, 6); a != 255 {
		t.Errorf("error badge alpha at (38,6) = %d, want 255", a)
	}
	if a := alpha(decode(t, monitor.HealthOK), 38, 6); a != 0 {
		t.Errorf("OK alpha at badge = %d, want 0", a)
	}
}

func TestOnlyBlack(t *testing.T) {
	for _, h := range allHealth {
		img := decode(t, h)
		for y := 0; y < 44; y++ {
			for x := 0; x < 44; x++ {
				r, g, b, a := img.At(x, y).RGBA()
				if a == 0 {
					continue
				}
				if r != 0 || g != 0 || b != 0 {
					t.Fatalf("%v: pixel (%d,%d) not black: %d %d %d", h, x, y, r, g, b)
				}
			}
		}
	}
}

func alpha(img image.Image, x, y int) uint32 {
	_, _, _, a := img.At(x, y).RGBA()
	return a >> 8
}

// The pulse's flat runs sit at y 22 on the 44 px canvas: the left one
// around x 14, the right one around x 30.
func TestOKHasWholePulse(t *testing.T) {
	img := decode(t, monitor.HealthOK)
	if l, r := alpha(img, 14, 22), alpha(img, 30, 22); l != 255 || r != 255 {
		t.Errorf("left alpha %d, right alpha %d; want both 255", l, r)
	}
}

func TestWarningBadgeIsHollow(t *testing.T) {
	img := decode(t, monitor.HealthWarning)
	if l, r := alpha(img, 14, 22), alpha(img, 30, 22); l != 255 || r != 255 {
		t.Errorf("pulse alpha left %d, right %d; want the whole pulse", l, r)
	}
	if a := alpha(img, 36, 8); a != 0 {
		t.Errorf("badge centre alpha = %d, want 0 (hollow)", a)
	}
	// The badge ring (radius 4 to 6 around (36, 8)): pixels (41, 8) and
	// (36, 3) sit inside the stroke, clear of the main ring.
	for _, p := range [][2]int{{41, 8}, {36, 3}} {
		if a := alpha(img, p[0], p[1]); a != 255 {
			t.Errorf("badge ring alpha at %v = %d, want 255", p, a)
		}
		if a := alpha(decode(t, monitor.HealthOK), p[0], p[1]); a != 0 {
			t.Errorf("OK alpha at %v = %d, want 0", p, a)
		}
	}
}

func TestErrorBadgeIsFilled(t *testing.T) {
	if a := alpha(decode(t, monitor.HealthError), 36, 8); a != 255 {
		t.Errorf("error badge centre alpha = %d, want 255", a)
	}
}

func TestUnknownIsRingOnly(t *testing.T) {
	img := decode(t, monitor.HealthUnknown)
	if a := alpha(img, 14, 22); a != 0 {
		t.Errorf("pulse alpha %d, want 0", a)
	}
	// The ring (radius 17.6, stroke 3.9) crosses the vertical axis at y 4.4.
	if a := alpha(img, 22, 4); a != 255 {
		t.Errorf("ring alpha at top = %d, want 255", a)
	}
}
