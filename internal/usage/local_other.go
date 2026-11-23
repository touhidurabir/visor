//go:build !linux && !darwin

package usage

// Local is nil where Visor can't measure processes: nothing is sampled.
func Local() Sampler { return nil }
