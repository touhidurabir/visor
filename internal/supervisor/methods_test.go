package supervisor

import (
	"context"
	"errors"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder answers every call with the next canned response and records calls.
type recorder struct {
	mu        sync.Mutex
	calls     []rpcCall
	responses []func(http.ResponseWriter)
}

func newRecorder(t *testing.T, responses ...func(http.ResponseWriter)) (*Client, *recorder) {
	rec := &recorder{responses: responses}
	path := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		call := parseCall(t, r)
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.calls = append(rec.calls, call)
		if len(rec.responses) == 0 {
			t.Errorf("unexpected call %s", call.Method)
			http.Error(w, "no response", http.StatusInternalServerError)
			return
		}
		next := rec.responses[0]
		rec.responses = rec.responses[1:]
		next(w)
	})
	return New(UnixTransport{Path: path}), rec
}

func value(v string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) { respond(w, v) }
}

func fault(code int, msg string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) { respondFault(w, code, msg) }
}

func (r *recorder) assertCalls(t *testing.T, want ...string) {
	t.Helper()
	var got []string
	for _, c := range r.calls {
		got = append(got, c.Method+"("+strings.Join(c.Params, ",")+")")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("calls\n got %q\nwant %q", got, want)
	}
}

const (
	str0 = "<string>web</string>"
	yes  = "<boolean>1</boolean>"
	no   = "<boolean>0</boolean>"
)

func TestGetAllProcessInfo(t *testing.T) {
	fixture, err := os.ReadFile("xmlrpc/testdata/getAllProcessInfo.xml")
	if err != nil {
		t.Fatal(err)
	}
	c, rec := newRecorder(t, func(w http.ResponseWriter) { w.Write(fixture) })
	procs, err := c.GetAllProcessInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rec.assertCalls(t, "supervisor.getAllProcessInfo()")
	if len(procs) != 3 {
		t.Fatalf("want 3 processes, got %d", len(procs))
	}
	web := procs[0]
	if web.Name != "web" || web.Group != "web" || web.FullName() != "web:web" {
		t.Errorf("names: %+v", web)
	}
	if web.State != StateRunning || web.StateName != "RUNNING" || web.State.String() != "RUNNING" {
		t.Errorf("state: %v %q", web.State, web.StateName)
	}
	if web.PID != 4242 || web.ExitStatus != 0 || web.Description != "pid 4242, uptime 6 days, 2:57:19" {
		t.Errorf("pid/exit/description: %+v", web)
	}
	if !web.Start.Equal(time.Unix(1792748139, 0)) || !web.Stop.IsZero() || web.Now.IsZero() {
		t.Errorf("times: start=%v stop=%v now=%v", web.Start, web.Stop, web.Now)
	}
	if web.StdoutLogfile != "/var/log/example/web.log" || web.Logfile != web.StdoutLogfile {
		t.Errorf("logfiles: %+v", web)
	}
	if reverb := procs[1]; reverb.State != StateFatal || reverb.SpawnErr != reverb.Description || reverb.Description != "Exited too quickly (process log may have details)" {
		t.Errorf("reverb: %+v", reverb)
	}
	if queue := procs[2]; queue.State != StateStopped || !queue.Start.IsZero() {
		t.Errorf("queue: %+v", queue)
	}
}

func TestProcessInfoRejectsBadShape(t *testing.T) {
	c, _ := newRecorder(t,
		value(`<array><data><value><struct><member><name>name</name><value>web</value></member></struct></value></data></array>`),
		value(`<array><data><value><int>1</int></value></data></array>`),
	)
	if _, err := c.GetAllProcessInfo(context.Background()); err == nil {
		t.Error("want error for struct missing fields")
	}
	if _, err := c.GetAllProcessInfo(context.Background()); err == nil {
		t.Error("want error for non-struct element")
	}
}

func TestProcessStateString(t *testing.T) {
	cases := map[ProcessState]string{
		StateStopped: "STOPPED", StateStarting: "STARTING", StateRunning: "RUNNING",
		StateBackoff: "BACKOFF", StateStopping: "STOPPING", StateExited: "EXITED",
		StateFatal: "FATAL", StateUnknown: "UNKNOWN", ProcessState(999): "UNKNOWN",
	}
	for s, want := range cases {
		if s.String() != want {
			t.Errorf("%d.String() = %q, want %q", int(s), s.String(), want)
		}
	}
}

func TestMethods(t *testing.T) {
	ctx := context.Background()
	results := `<array><data><value><struct>
<member><name>name</name><value><string>web_00</string></value></member>
<member><name>group</name><value><string>web</string></value></member>
<member><name>status</name><value><int>80</int></value></member>
<member><name>description</name><value><string>OK</string></value></member>
</struct></value></data></array>`
	wantResults := []ProcessResult{{Name: "web_00", Group: "web", Status: 80, Description: "OK"}}

	cases := []struct {
		name     string
		response string
		call     func(c *Client) (any, error)
		wantCall string
		want     any
	}{
		{"GetState", `<struct><member><name>statecode</name><value><int>1</int></value></member><member><name>statename</name><value><string>RUNNING</string></value></member></struct>`,
			func(c *Client) (any, error) { return c.GetState(ctx) },
			"supervisor.getState()", SupervisorState{Code: 1, Name: "RUNNING"}},
		{"GetProcessInfo", `<struct>` + processMembers("web", 20) + `</struct>`,
			func(c *Client) (any, error) { p, err := c.GetProcessInfo(ctx, "web:web"); return p.FullName(), err },
			"supervisor.getProcessInfo(<string>web:web</string>)", "web:web"},
		{"ListMethods", `<array><data><value><string>system.listMethods</string></value><value><string>supervisor.getState</string></value></data></array>`,
			func(c *Client) (any, error) { return c.ListMethods(ctx) },
			"system.listMethods()", []string{"system.listMethods", "supervisor.getState"}},
		{"StartProcess", yes,
			func(c *Client) (any, error) { return nil, c.StartProcess(ctx, "web", true) },
			"supervisor.startProcess(" + str0 + "," + yes + ")", nil},
		{"StopProcess", yes,
			func(c *Client) (any, error) { return nil, c.StopProcess(ctx, "web", false) },
			"supervisor.stopProcess(" + str0 + "," + no + ")", nil},
		{"StartProcessGroup", results,
			func(c *Client) (any, error) { return c.StartProcessGroup(ctx, "web", true) },
			"supervisor.startProcessGroup(" + str0 + "," + yes + ")", wantResults},
		{"StopProcessGroup", results,
			func(c *Client) (any, error) { return c.StopProcessGroup(ctx, "web", true) },
			"supervisor.stopProcessGroup(" + str0 + "," + yes + ")", wantResults},
		{"StartAllProcesses", results,
			func(c *Client) (any, error) { return c.StartAllProcesses(ctx, false) },
			"supervisor.startAllProcesses(" + no + ")", wantResults},
		{"StopAllProcesses", `<array><data></data></array>`,
			func(c *Client) (any, error) { return c.StopAllProcesses(ctx, true) },
			"supervisor.stopAllProcesses(" + yes + ")", []ProcessResult{}},
		{"SignalProcess", yes,
			func(c *Client) (any, error) { return nil, c.SignalProcess(ctx, "web", "HUP") },
			"supervisor.signalProcess(" + str0 + ",<string>HUP</string>)", nil},
		{"TailProcessStdoutLog", `<array><data><value><string>line</string></value><value><int>1234</int></value><value><boolean>1</boolean></value></data></array>`,
			func(c *Client) (any, error) { return c.TailProcessStdoutLog(ctx, "web", 100, 4096) },
			"supervisor.tailProcessStdoutLog(" + str0 + ",<int>100</int>,<int>4096</int>)", TailResult{Bytes: "line", Offset: 1234, Overflow: true}},
		{"TailProcessStderrLog", `<array><data><value><string></string></value><value><int>0</int></value><value><boolean>0</boolean></value></data></array>`,
			func(c *Client) (any, error) { return c.TailProcessStderrLog(ctx, "web", 0, 10) },
			"supervisor.tailProcessStderrLog(" + str0 + ",<int>0</int>,<int>10</int>)", TailResult{}},
		{"ReadLog", `<string>main log</string>`,
			func(c *Client) (any, error) { return c.ReadLog(ctx, -100, 0) },
			"supervisor.readLog(<int>-100</int>,<int>0</int>)", "main log"},
		{"ClearProcessLogs", yes,
			func(c *Client) (any, error) { return nil, c.ClearProcessLogs(ctx, "web") },
			"supervisor.clearProcessLogs(" + str0 + ")", nil},
		{"ReloadConfig", `<array><data><value><array><data>
<value><array><data><value><string>new</string></value></data></array></value>
<value><array><data><value><string>web</string></value></data></array></value>
<value><array><data></data></array></value>
</data></array></value></data></array>`,
			func(c *Client) (any, error) { return c.ReloadConfig(ctx) },
			"supervisor.reloadConfig()", ConfigDiff{Added: []string{"new"}, Changed: []string{"web"}, Removed: []string{}}},
		{"AddProcessGroup", yes,
			func(c *Client) (any, error) { return nil, c.AddProcessGroup(ctx, "web") },
			"supervisor.addProcessGroup(" + str0 + ")", nil},
		{"RemoveProcessGroup", yes,
			func(c *Client) (any, error) { return nil, c.RemoveProcessGroup(ctx, "web") },
			"supervisor.removeProcessGroup(" + str0 + ")", nil},
		{"Restart", yes,
			func(c *Client) (any, error) { return nil, c.Restart(ctx) },
			"supervisor.restart()", nil},
		{"Shutdown", yes,
			func(c *Client) (any, error) { return nil, c.Shutdown(ctx) },
			"supervisor.shutdown()", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := newRecorder(t, value(tc.response))
			got, err := tc.call(c)
			if err != nil {
				t.Fatal(err)
			}
			rec.assertCalls(t, tc.wantCall)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestReloadConfigRejectsBadShape(t *testing.T) {
	c, _ := newRecorder(t, value(`<array><data><value><array><data></data></array></value></data></array>`))
	if _, err := c.ReloadConfig(context.Background()); err == nil {
		t.Error("want error for reloadConfig result without three lists")
	}
}

func TestRestartProcess(t *testing.T) {
	ctx := context.Background()
	stop := "supervisor.stopProcess(" + str0 + "," + yes + ")"
	start := "supervisor.startProcess(" + str0 + "," + yes + ")"

	t.Run("running", func(t *testing.T) {
		c, rec := newRecorder(t, value(yes), value(yes))
		if err := c.RestartProcess(ctx, "web"); err != nil {
			t.Fatal(err)
		}
		rec.assertCalls(t, stop, start)
	})
	t.Run("not running still starts", func(t *testing.T) {
		c, rec := newRecorder(t, fault(70, "NOT_RUNNING: web"), value(yes))
		if err := c.RestartProcess(ctx, "web"); err != nil {
			t.Fatal(err)
		}
		rec.assertCalls(t, stop, start)
	})
	t.Run("stop failure aborts", func(t *testing.T) {
		c, rec := newRecorder(t, fault(10, "BAD_NAME: web"))
		if err := c.RestartProcess(ctx, "web"); !errors.Is(err, ErrBadName) {
			t.Errorf("want ErrBadName, got %v", err)
		}
		rec.assertCalls(t, stop)
	})
}

// processMembers renders a complete getProcessInfo struct body.
func processMembers(name string, state int) string {
	m := func(k, v string) string { return "<member><name>" + k + "</name><value>" + v + "</value></member>" }
	return m("name", "<string>"+name+"</string>") + m("group", "<string>"+name+"</string>") +
		m("description", "<string></string>") + m("start", "<int>0</int>") + m("stop", "<int>0</int>") +
		m("now", "<int>0</int>") + m("state", "<int>"+strconv.Itoa(state)+"</int>") + m("statename", "<string>RUNNING</string>") +
		m("spawnerr", "<string></string>") + m("exitstatus", "<int>0</int>") + m("logfile", "<string></string>") +
		m("stdout_logfile", "<string></string>") + m("stderr_logfile", "<string></string>") + m("pid", "<int>0</int>")
}
