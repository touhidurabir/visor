package validate

import "strings"

// knownKeys is every [program:x] option supervisord reads, in the order
// "Did you mean" prefers on ties.
var knownKeys = []string{
	"command", "process_name", "numprocs", "numprocs_start", "priority",
	"autostart", "startsecs", "startretries", "autorestart", "exitcodes",
	"stopsignal", "stopwaitsecs", "stopasgroup", "killasgroup", "user",
	"redirect_stderr",
	"stdout_logfile", "stdout_logfile_maxbytes", "stdout_logfile_backups",
	"stdout_capture_maxbytes", "stdout_events_enabled", "stdout_syslog",
	"stderr_logfile", "stderr_logfile_maxbytes", "stderr_logfile_backups",
	"stderr_capture_maxbytes", "stderr_events_enabled", "stderr_syslog",
	"environment", "directory", "umask", "serverurl",
}

type keyType int

const (
	typeString keyType = iota
	typeInt
	typeBytes
	typeBool
)

func typeOf(key string) keyType {
	switch {
	case key == "numprocs", key == "numprocs_start", key == "priority",
		key == "startsecs", key == "startretries", key == "stopwaitsecs",
		strings.HasSuffix(key, "_logfile_backups"):
		return typeInt
	case strings.HasSuffix(key, "_logfile_maxbytes"), strings.HasSuffix(key, "_capture_maxbytes"):
		return typeBytes
	case key == "autostart", key == "stopasgroup", key == "killasgroup",
		key == "redirect_stderr", strings.HasSuffix(key, "_events_enabled"),
		strings.HasSuffix(key, "_syslog"):
		return typeBool
	}
	return typeString
}

func isKnown(key string) bool {
	for _, k := range knownKeys {
		if k == key {
			return true
		}
	}
	return false
}

// closest returns the known key nearest to key and its edit distance.
func closest(key string) (string, int) {
	best, bd := "", 1<<30
	for _, k := range knownKeys {
		if d := levenshtein(key, k); d < bd {
			best, bd = k, d
		}
	}
	return best, bd
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
