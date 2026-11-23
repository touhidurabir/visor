package usage

import (
	"context"
	"os"
	"time"
)

// userHZ is Linux's clock tick for /proc times (USER_HZ), 100 on every
// architecture Go supports.
const userHZ = 100

// Local measures this computer's processes from /proc.
func Local() Sampler {
	return SamplerFunc(func(_ context.Context, pids []int) (map[int]Totals, error) {
		t, err := ReadProc("/proc", time.Second/userHZ, uint64(os.Getpagesize()))
		if err != nil {
			return nil, err
		}
		return t.Trees(pids), nil
	})
}
