//go:build !linux && !darwin

package usage

import "errors"

// Local is nil where Visor can't measure processes: nothing is sampled.
func Local() Sampler { return nil }

// LocalMachine is unknown where Visor can't measure processes.
func LocalMachine() (Machine, error) { return Machine{}, errors.ErrUnsupported }
