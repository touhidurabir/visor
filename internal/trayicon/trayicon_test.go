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
	_, _, _, a = decode(t, monitor.HealthOK).At(36, 8).RGBA()
	if a != 0 {
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

func TestWarningIsLeftHalf(t *testing.T) {
	img := decode(t, monitor.HealthWarning)
	_, _, _, l := img.At(18, 22).RGBA()
	_, _, _, r := img.At(26, 22).RGBA()
	if l == 0 || r != 0 {
		t.Errorf("left alpha %d, right alpha %d; want left filled, right empty", l, r)
	}
}
