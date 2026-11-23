package usage

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// stat builds a /proc/<pid>/stat line: utime stime cutime cstime in ticks,
// rss in pages.
func stat(pid int, comm string, ppid, ut, st, cut, cst, rss int) string {
	f := []string{strconv.Itoa(pid), "(" + comm + ")", "S", strconv.Itoa(ppid)}
	at := map[int]int{14: ut, 15: st, 16: cut, 17: cst, 24: rss}
	for i := 5; i <= 52; i++ {
		f = append(f, strconv.Itoa(at[i]))
	}
	return strings.Join(f, " ") + "\n"
}

const tick = 10 * time.Millisecond

func TestParseStat(t *testing.T) {
	p, err := ParseStat(stat(1234, "php-fpm: pool (www)", 1, 100, 50, 20, 30, 25), tick, 4096)
	if err != nil {
		t.Fatal(err)
	}
	want := Proc{PID: 1234, PPID: 1, CPU: 200 * tick, RSS: 25 * 4096}
	if p != want {
		t.Fatalf("got %+v, want %+v", p, want)
	}
	if _, err := ParseStat("12 (x) S 1 2 3", tick, 4096); err == nil {
		t.Fatal("a short line parsed")
	}
	if _, err := ParseStat("garbage", tick, 4096); err == nil {
		t.Fatal("garbage parsed")
	}
}

func writeProc(t *testing.T, root string, pid int, line string) {
	t.Helper()
	d := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "stat"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadProc(t *testing.T) {
	root := t.TempDir()
	writeProc(t, root, 10, stat(10, "sh", 1, 1, 1, 0, 0, 10))
	writeProc(t, root, 11, stat(11, "php artisan", 10, 5, 5, 0, 0, 20))
	if err := os.MkdirAll(filepath.Join(root, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "99"), 0o755); err != nil { // vanished: no stat
		t.Fatal(err)
	}
	tab, err := ReadProc(root, tick, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if len(tab) != 2 || tab[11].PPID != 10 || tab[11].CPU != 10*tick {
		t.Fatalf("table %+v", tab)
	}
	if _, err := ReadProc(filepath.Join(root, "nope"), tick, 4096); err == nil {
		t.Fatal("a missing root read")
	}
}

func TestParseMemTotal(t *testing.T) {
	got, ok := ParseMemTotal([]byte("MemTotal:       16303512 kB\nMemFree:         1024 kB\n"))
	if !ok || got != 16303512*1024 {
		t.Fatalf("MemTotal: %d %v", got, ok)
	}
	for _, bad := range []string{"", "MemFree: 1 kB\n", "MemTotal: lots kB\n"} {
		if got, ok := ParseMemTotal([]byte(bad)); ok {
			t.Fatalf("%q parsed as %d", bad, got)
		}
	}
}
