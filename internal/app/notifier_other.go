//go:build (!darwin && !linux) || server

package app

import (
	"context"

	"github.com/touhidurabir/visor/internal/notify"
)

// noNotifier is the notification centre of builds without one: the server
// build (UI tests) and platforms other than macOS and Linux. It shows
// nothing.
type noNotifier struct{}

func newNativeNotifier() nativeNotifier { return noNotifier{} }

func (noNotifier) start(context.Context, func(string)) {}
func (noNotifier) send(notify.Message) error           { return notify.ErrUnavailable }
func (noNotifier) status() string                      { return NotifyUnavailable }
