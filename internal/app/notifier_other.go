//go:build !darwin || server

package app

import (
	"context"

	"github.com/touhidurabir/visor/internal/notify"
)

// noNotifier is the notification centre of builds without one: the server
// build (UI tests) and other platforms. It shows nothing.
type noNotifier struct{}

func newNativeNotifier() nativeNotifier { return noNotifier{} }

func (noNotifier) start(context.Context, func()) {}
func (noNotifier) send(notify.Message) error     { return notify.ErrUnavailable }
func (noNotifier) status() string                { return NotifyUnavailable }
