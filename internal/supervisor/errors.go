package supervisor

import (
	"errors"

	"github.com/touhidurabir/visor/internal/supervisor/xmlrpc"
)

// Fault is a fault returned by supervisord. Match it against the Err*
// sentinels below with errors.Is; the comparison is by Code.
type Fault = xmlrpc.Fault

// Fault codes from supervisor/xmlrpc.py.
var (
	ErrUnknownMethod       = &Fault{Code: 1, String: "UNKNOWN_METHOD"}
	ErrIncorrectParameters = &Fault{Code: 2, String: "INCORRECT_PARAMETERS"}
	ErrBadArguments        = &Fault{Code: 3, String: "BAD_ARGUMENTS"}
	ErrShutdownState       = &Fault{Code: 6, String: "SHUTDOWN_STATE"}
	ErrBadName             = &Fault{Code: 10, String: "BAD_NAME"}
	ErrBadSignal           = &Fault{Code: 11, String: "BAD_SIGNAL"}
	ErrNoFile              = &Fault{Code: 20, String: "NO_FILE"}
	ErrNotExecutable       = &Fault{Code: 21, String: "NOT_EXECUTABLE"}
	ErrFailed              = &Fault{Code: 30, String: "FAILED"}
	ErrAbnormalTermination = &Fault{Code: 40, String: "ABNORMAL_TERMINATION"}
	ErrSpawnError          = &Fault{Code: 50, String: "SPAWN_ERROR"}
	ErrAlreadyStarted      = &Fault{Code: 60, String: "ALREADY_STARTED"}
	ErrNotRunning          = &Fault{Code: 70, String: "NOT_RUNNING"}
	ErrAlreadyAdded        = &Fault{Code: 90, String: "ALREADY_ADDED"}
	ErrStillRunning        = &Fault{Code: 91, String: "STILL_RUNNING"}
	ErrCantReread          = &Fault{Code: 92, String: "CANT_REREAD"}
)

var (
	// ErrUnauthorized means supervisord rejected the credentials (HTTP 401).
	ErrUnauthorized = errors.New("supervisord: unauthorized")
	// ErrUnreachable means no connection could be made: supervisord is not
	// running, the socket path is wrong, or permissions deny access.
	ErrUnreachable = errors.New("supervisord: unreachable")
)
