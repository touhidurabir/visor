package usage

import (
	"testing"
	"time"
)

func TestTree(t *testing.T) {
	tab := Table{
		10: {PID: 10, PPID: 1, CPU: time.Second, RSS: 100},
		11: {PID: 11, PPID: 10, CPU: 2 * time.Second, RSS: 200},
		12: {PID: 12, PPID: 10, CPU: 3 * time.Second, RSS: 300},
		13: {PID: 13, PPID: 12, CPU: 4 * time.Second, RSS: 400},
		20: {PID: 20, PPID: 1, CPU: time.Hour, RSS: 9999},
		30: {PID: 30, PPID: 31},
		31: {PID: 31, PPID: 30},
	}
	got, ok := tab.Tree(10)
	if !ok || got != (Totals{CPU: 10 * time.Second, RSS: 1000, Procs: 4}) {
		t.Fatalf("tree 10: %+v %v", got, ok)
	}
	if got, ok := tab.Tree(30); !ok || got.Procs != 2 {
		t.Fatalf("cycle: %+v %v", got, ok)
	}
	if _, ok := tab.Tree(77); ok {
		t.Fatal("a missing root has a tree")
	}
	all := tab.Trees([]int{10, 77, 20})
	if len(all) != 2 || all[20].Procs != 1 {
		t.Fatalf("trees %+v", all)
	}
}
