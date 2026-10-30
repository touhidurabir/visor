package monitor

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/touhidurabir/visor/internal/supervisor"
)

// Source is what the monitor polls. *supervisor.Client satisfies it.
type Source interface {
	GetState(ctx context.Context) (supervisor.SupervisorState, error)
	GetAllProcessInfo(ctx context.Context) ([]supervisor.ProcessInfo, error)
}

// Update is one poll's snapshot and the events since the previous one.
type Update struct {
	Snapshot Snapshot
	Events   []Event
}

const (
	defaultInterval = 2 * time.Second
	minCallTimeout  = time.Second
	// maxPendingEvents bounds the events carried over for a subscriber that
	// is not reading, so a stuck subscriber cannot grow memory forever.
	maxPendingEvents = 256
)

// Monitor polls a Source on a fixed interval and publishes an Update to
// every subscriber after each poll. All polling happens in Run's goroutine;
// subscribers can never block it.
type Monitor struct {
	src      Source
	interval time.Duration
	poke     chan struct{}
	started  atomic.Bool

	mu     sync.Mutex
	latest Update
	subs   map[chan Update]struct{}
	done   bool // Run has returned; subscriptions are closed
}

// New returns a monitor that polls src every interval (2s if interval <= 0).
// Nothing is polled until Run is called.
func New(src Source, interval time.Duration) *Monitor {
	if interval <= 0 {
		interval = defaultInterval
	}
	return &Monitor{
		src:      src,
		interval: interval,
		poke:     make(chan struct{}, 1),
		subs:     make(map[chan Update]struct{}),
	}
}

// Run polls immediately, then on every tick or Poke, until ctx is done.
// When it returns, every subscription channel is closed. Run may be
// called only once per Monitor.
func (m *Monitor) Run(ctx context.Context) {
	if !m.started.CompareAndSwap(false, true) {
		panic("monitor: Run called more than once")
	}
	defer m.closeAll()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	var (
		prev  Snapshot                 // last published snapshot
		known []supervisor.ProcessInfo // processes from the last reachable poll
	)
	for {
		next := m.poll(ctx)
		if ctx.Err() != nil {
			return // the poll was cut short by our own cancellation
		}
		// Diff across an outage against the processes seen before it, so a
		// reconnect reports what changed instead of re-adding everything.
		base := prev
		if !base.Reachable {
			base.Processes = known
		}
		events := Diff(base, next)
		if next.Reachable {
			known = next.Processes
		}
		prev = next
		m.publish(Update{Snapshot: next, Events: events})

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-m.poke:
			ticker.Reset(m.interval)
		}
	}
}

// poll takes one snapshot. Each call gets its own timeout of twice the
// interval (at least 1s), so a hung supervisord cannot stall the loop.
func (m *Monitor) poll(ctx context.Context) Snapshot {
	timeout := max(2*m.interval, minCallTimeout)

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	state, err := m.src.GetState(callCtx)
	cancel()
	if err != nil {
		return Snapshot{At: time.Now(), Err: err}
	}
	callCtx, cancel = context.WithTimeout(ctx, timeout)
	procs, err := m.src.GetAllProcessInfo(callCtx)
	cancel()
	if err != nil {
		return Snapshot{At: time.Now(), Err: err}
	}
	slices.SortFunc(procs, func(a, b supervisor.ProcessInfo) int {
		return cmp.Compare(a.FullName(), b.FullName())
	})
	return Snapshot{At: time.Now(), Reachable: true, State: state, Processes: procs}
}

// Subscribe returns a channel that receives an Update after every poll,
// and a func that ends the subscription (safe to call more than once).
// The channel holds one Update: a subscriber that falls behind gets the
// latest snapshot, with the events of the updates it missed merged in front
// (up to maxPendingEvents of them). The channel is closed when Run returns,
// or straight away if Run has already returned.
func (m *Monitor) Subscribe() (<-chan Update, func()) {
	ch := make(chan Update, 1)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.done {
		close(ch)
		return ch, func() {}
	}
	m.subs[ch] = struct{}{}
	return ch, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if _, ok := m.subs[ch]; ok {
			delete(m.subs, ch)
			close(ch)
		}
	}
}

// Poke asks for a poll now, e.g. right after a user action. It never
// blocks; pokes made while a poll is pending collapse into one.
func (m *Monitor) Poke() {
	select {
	case m.poke <- struct{}{}:
	default:
	}
}

// Latest returns the most recent Update, or the zero Update before the
// first poll (whose health is HealthUnknown).
func (m *Monitor) Latest() Update {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.latest.clone()
}

// publish records u and hands each subscriber its own copy without
// blocking. Sends happen only here, under mu, so after the buffer is
// drained the final send always has room.
func (m *Monitor) publish(u Update) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latest = u
	for ch := range m.subs {
		out := u.clone()
		select {
		case ch <- out:
			continue
		default:
		}
		select {
		case old := <-ch: // drop the stale update but keep its events
			out.Events = mergeEvents(old.Events, out.Events)
		default: // the subscriber took it meanwhile
		}
		select {
		case ch <- out:
		default:
		}
	}
}

func (m *Monitor) closeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.done = true
	for ch := range m.subs {
		close(ch)
	}
	clear(m.subs)
}

// mergeEvents appends newer to older, dropping the oldest of older so the
// result has at most maxPendingEvents (newer is always kept whole).
func mergeEvents(older, newer []Event) []Event {
	keep := max(maxPendingEvents-len(newer), 0)
	if len(older) > keep {
		older = older[len(older)-keep:]
	}
	out := make([]Event, 0, len(older)+len(newer))
	return append(append(out, older...), newer...)
}

// clone copies the slices so no two holders share backing arrays.
func (u Update) clone() Update {
	u.Snapshot.Processes = slices.Clone(u.Snapshot.Processes)
	u.Events = slices.Clone(u.Events)
	return u
}
