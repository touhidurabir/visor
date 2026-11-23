package mcp

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEffective(t *testing.T) {
	cases := []struct {
		name     string
		m        Machine
		level    Level
		askFirst bool
	}{
		{"local control, ask off", Machine{ID: "local", Local: true, Level: Control}, Control, false},
		{"local configs, ask on", Machine{ID: "local", Local: true, Level: Configs, Ask: true}, Configs, true},
		{"dev off", Machine{Tag: "development", Level: Off, Ask: true}, Off, false},
		{"dev read never asks", Machine{Tag: "development", Level: Read, Ask: true}, Read, false},
		{"staging control, ask off", Machine{Tag: "staging", Level: Control}, Control, false},
		{"production read", Machine{Tag: "production", Level: Read}, Read, false},
		{"production control, ask off still asks", Machine{Tag: "production", Level: Control}, Control, true},
		{"production configs, ask off still asks", Machine{Tag: "production", Level: Configs}, Configs, true},
		{"read only at configs is read", Machine{Tag: "development", ReadOnly: true, Level: Configs, Ask: true}, Read, false},
		{"read only at control is read", Machine{ReadOnly: true, Level: Control}, Read, false},
		{"read only off stays off", Machine{ReadOnly: true, Level: Off}, Off, false},
		{"read only production", Machine{Tag: "production", ReadOnly: true, Level: Configs}, Read, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l, ask := Effective(c.m)
			if l != c.level || ask != c.askFirst {
				t.Fatalf("Effective = (%v, %v), want (%v, %v)", l, ask, c.level, c.askFirst)
			}
		})
	}
}

func TestDefault(t *testing.T) {
	if l, a := Default(true); l != Control || a {
		t.Errorf("Default(local) = (%v, %v), want (control, false)", l, a)
	}
	if l, a := Default(false); l != Off || !a {
		t.Errorf("Default(server) = (%v, %v), want (off, true)", l, a)
	}
}

func TestParseLevel(t *testing.T) {
	for _, l := range []Level{Off, Read, Control, Configs} {
		got, err := ParseLevel(l.String())
		if err != nil || got != l {
			t.Errorf("ParseLevel(%q) = %v, %v", l.String(), got, err)
		}
	}
	if _, err := ParseLevel("admin"); err == nil {
		t.Error("ParseLevel(admin) accepted an unknown level")
	}
	if Configs.Label() != "Change configs" || Off.Label() != "Off" {
		t.Errorf("labels: %q %q", Configs.Label(), Off.Label())
	}
}

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	l := NewLimiter(func() time.Time { return now })

	// The gap is waited out, not refused: a change the person allowed goes
	// ahead, one per second per machine.
	if w := l.Change("local"); w != 0 {
		t.Fatalf("first change waits %s", w)
	}
	if w := l.Change("local"); w != time.Second {
		t.Fatalf("second change at once waits %s, want 1s", w)
	}
	if w := l.Change("local"); w != 2*time.Second {
		t.Fatalf("third change at once waits %s, want 2s (its slot is after the second's)", w)
	}
	if w := l.Change("prod"); w != 0 {
		t.Fatalf("a change on another machine waits %s", w)
	}
	now = now.Add(3 * time.Second)
	if w := l.Change("local"); w != 0 {
		t.Fatalf("a change 3s later waits %s", w)
	}

	for i := 0; i < 30; i++ {
		if err := l.Allow(); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	err := l.Allow()
	if !errors.Is(err, ErrRateLimited) || !strings.Contains(err.Error(), "wait 10s") {
		t.Fatalf("31st call within 10s: %v, want ErrRateLimited with the wait", err)
	}
	now = now.Add(10 * time.Second)
	if err := l.Allow(); err != nil {
		t.Fatalf("after the window: %v", err)
	}
}
