package mcp

import (
	"errors"
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

	if err := l.Allow("local", true); err != nil {
		t.Fatalf("first change: %v", err)
	}
	if err := l.Allow("local", true); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("second change within 1s on the same machine: %v, want ErrRateLimited", err)
	}
	if err := l.Allow("prod", true); err != nil {
		t.Fatalf("a change on another machine: %v", err)
	}
	if err := l.Allow("local", false); err != nil {
		t.Fatalf("a read right after a change: %v", err)
	}
	now = now.Add(time.Second)
	if err := l.Allow("local", true); err != nil {
		t.Fatalf("a change 1s later: %v", err)
	}

	// 4 calls so far; 26 more reach 30, the 31st is refused.
	for i := 0; i < 26; i++ {
		if err := l.Allow("local", false); err != nil {
			t.Fatalf("call %d: %v", i+5, err)
		}
	}
	err := l.Allow("local", false)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("31st call within 10s: %v, want ErrRateLimited", err)
	}
	now = now.Add(10 * time.Second)
	if err := l.Allow("local", false); err != nil {
		t.Fatalf("after the window: %v", err)
	}
}
