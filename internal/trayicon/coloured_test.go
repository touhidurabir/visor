package trayicon

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/touhidurabir/visor/internal/monitor"
)

var update = flag.Bool("update", false, "rewrite the golden tray icons in testdata/")

var counts = []int{0, 1, 9, 10}

// The Linux tray icons match the reviewed images in testdata/ (look at them
// after changing the drawing, then run go test -run Golden -update).
func TestColouredGolden(t *testing.T) {
	for _, h := range allHealth {
		for _, n := range counts {
			name := filepath.Join("testdata", fmt.Sprintf("coloured-%s-%d.png", h, n))
			got := Coloured(h, n)
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(name, got, 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(name)
			if err != nil {
				t.Fatalf("%s: %v (run with -update to create it)", name, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s: the icon changed", name)
			}
		}
	}
}

func colouredImage(t *testing.T, h monitor.Health, n int) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(Coloured(h, n)))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func rgba(img image.Image, x, y int) (r, g, b, a uint32) {
	r, g, b, a = img.At(x, y).RGBA()
	return r >> 8, g >> 8, b >> 8, a >> 8
}

func TestColouredLayers(t *testing.T) {
	ok := colouredImage(t, monitor.HealthOK, 0)
	if b := ok.Bounds(); b.Dx() != 44 || b.Dy() != 44 {
		t.Fatalf("size %v", b)
	}
	// Dark glass in the middle, off the pulse line.
	if r, g, b, a := rgba(ok, 22, 30); a != 255 || r != 0x16 || g != 0x20 || b != 0x2E {
		t.Errorf("glass = %d,%d,%d,%d", r, g, b, a)
	}
	// Corners stay transparent, so the panel shows through.
	if _, _, _, a := rgba(ok, 0, 43); a != 0 {
		t.Errorf("corner alpha = %d", a)
	}
	// With nothing to say, the top right is empty; an error puts a red dot there.
	if _, _, _, a := rgba(ok, 38, 6); a != 0 {
		t.Errorf("OK badge area alpha = %d", a)
	}
	if r, g, b, _ := rgba(colouredImage(t, monitor.HealthError, 0), 38, 6); r != 0xE5 || g != 0x48 || b != 0x4D {
		t.Errorf("error dot = %d,%d,%d", r, g, b)
	}
	// Unknown has no pulse: the glass shows where the pulse line runs.
	if r, g, b, _ := rgba(colouredImage(t, monitor.HealthUnknown, 0), 22, 22); r != 0x16 || g != 0x20 || b != 0x2E {
		t.Errorf("unknown centre = %d,%d,%d, want glass", r, g, b)
	}
}

func TestColouredCountBadge(t *testing.T) {
	for _, n := range []int{1, 9, 10, 250} {
		img := colouredImage(t, monitor.HealthWarning, n)
		white := 0
		for y := 0; y < 18; y++ {
			for x := 22; x < 44; x++ {
				if r, g, b, a := rgba(img, x, y); a == 255 && r == 255 && g == 255 && b == 255 {
					white++
				}
			}
		}
		if white < 20 {
			t.Errorf("%d unread: %d white text pixels in the badge", n, white)
		}
	}
	if !bytes.Equal(Coloured(monitor.HealthOK, 10), Coloured(monitor.HealthOK, 99)) {
		t.Error("10 and 99 should both read 9+")
	}
	seen := map[string]bool{}
	for _, h := range allHealth {
		for _, n := range counts {
			k := string(Coloured(h, n))
			if seen[k] {
				t.Errorf("%v with %d unread looks like another icon", h, n)
			}
			seen[k] = true
		}
	}
}
