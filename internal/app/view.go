package app

import (
	"time"

	"github.com/touhidurabir/visor/internal/supervisor"
)

// ProcessView is the frontend's view of one supervised process. Times are
// Unix milliseconds, 0 when supervisord reports none.
type ProcessView struct {
	Name        string
	Group       string
	FullName    string
	State       string
	Description string
	SpawnErr    string
	PID         int
	ExitStatus  int
	StartMs     int64
	StopMs      int64
	NowMs       int64
}

func toProcessView(p supervisor.ProcessInfo) ProcessView {
	return ProcessView{
		Name:        p.Name,
		Group:       p.Group,
		FullName:    p.FullName(),
		State:       p.State.String(),
		Description: p.Description,
		SpawnErr:    p.SpawnErr,
		PID:         p.PID,
		ExitStatus:  p.ExitStatus,
		StartMs:     ms(p.Start),
		StopMs:      ms(p.Stop),
		NowMs:       ms(p.Now),
	}
}

func ms(t time.Time) int64 {
	if t.IsZero() || t.UnixMilli() == 0 {
		return 0
	}
	return t.UnixMilli()
}
