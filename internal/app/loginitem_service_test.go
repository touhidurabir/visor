package app

import (
	"errors"
	"testing"

	"github.com/touhidurabir/visor/internal/platform"
)

type fakeLogin struct {
	st  platform.LoginItemStatus
	err error
	set []bool
}

func (f *fakeLogin) Status() platform.LoginItemStatus { return f.st }
func (f *fakeLogin) Set(on bool) error {
	f.set = append(f.set, on)
	if f.err != nil {
		return f.err
	}
	if on {
		f.st = platform.LoginEnabled
	} else {
		f.st = platform.LoginDisabled
	}
	return nil
}

func TestLaunchAtLoginTurnsOnAndTells(t *testing.T) {
	li := &fakeLogin{st: platform.LoginDisabled}
	told := 0
	s := NewLoginItemService(li, func(platform.LoginItemStatus) { told++ })
	if v := s.LaunchAtLogin(); v.Status != "disabled" || v.On {
		t.Fatalf("before = %+v", v)
	}
	v, err := s.SetLaunchAtLogin(true)
	if err != nil || v.Status != "enabled" || !v.On || told != 1 {
		t.Fatalf("after = %+v, %v, told %d", v, err, told)
	}
}

func TestLaunchAtLoginNotMovedIsAnError(t *testing.T) {
	li := &fakeLogin{st: platform.LoginNotMoved, err: platform.ErrNotMoved}
	s := NewLoginItemService(li, nil)
	v, err := s.SetLaunchAtLogin(true)
	if !errors.Is(err, platform.ErrNotMoved) || v.Status != "not_moved" || v.On {
		t.Fatalf("got %+v, %v", v, err)
	}
}
