package doctor

import (
	"strings"
	"testing"
)

func status(rows []ReportRow, id string) ReportRow {
	for _, r := range rows {
		if r.ID == id {
			return r
		}
	}
	return ReportRow{Status: "missing"}
}

func TestRemoteReportAllGood(t *testing.T) {
	rows := RemoteReport(RemoteFacts{Login: "deploy@h:22", HostKey: "SHA256:x", BridgeUser: "deploy",
		ConfigPath: "/etc/supervisor/supervisord.conf", Via: "forward", Socket: "unix:/run/supervisor.sock",
		IncludeDir: "/etc/supervisor/conf.d", Files: 7, Version: "4.2.5"})
	if Blocked(rows) {
		t.Fatalf("%+v", rows)
	}
	for _, id := range []string{"ssh", "config", "socket", "read", "version"} {
		if status(rows, id).Status != RowOK {
			t.Errorf("%s: %+v", id, status(rows, id))
		}
	}
	if status(rows, "sudo").Status != RowInfo || status(rows, "write").Status != RowInfo {
		t.Error("sudo off and no include check are info")
	}
	if !strings.Contains(status(rows, "read").Detail, "7 files") {
		t.Error(status(rows, "read").Detail)
	}
}

func TestRemoteReportSocketDeniedFix(t *testing.T) {
	rows := RemoteReport(RemoteFacts{Login: "deploy@h:22", ConfigPath: "/etc/supervisor/supervisord.conf", Service: "supervisor",
		Socket: "unix:/var/run/supervisor.sock", SocketDenied: true, SocketOwner: "root", SocketGroup: "root", SocketMode: "0700"})
	r := status(rows, "socket")
	if r.Status != RowError || len(r.Fix) < 3 {
		t.Fatalf("%+v", r)
	}
	var cmds []string
	for _, f := range r.Fix {
		cmds = append(cmds, f.Command)
	}
	all := strings.Join(cmds, "\n")
	if !strings.Contains(all, "chown=root:supervisor") || !strings.Contains(all, "usermod -aG supervisor deploy") || !strings.Contains(all, "systemctl restart supervisor") {
		t.Fatalf("fix: %s", all)
	}
	// A daemon run by another user keeps that user as the socket's owner.
	rows = RemoteReport(RemoteFacts{Login: "ana@h:22", ConfigPath: "/etc/supervisor/supervisord.conf", Service: "supervisor",
		Socket: "unix:/home/deploy/s.sock", SocketDenied: true, SocketOwner: "deploy", SocketGroup: "deploy", SocketMode: "0700", Chown: "deploy:deploy"})
	if !strings.Contains(status(rows, "socket").Fix[1].Command, "chown=deploy:supervisor") {
		t.Fatalf("%+v", status(rows, "socket").Fix)
	}
	// The group is there but the user isn't in it: add them, then check
	// again (a new SSH login has the group; no logging out on a server).
	rows = RemoteReport(RemoteFacts{Login: "abir@h:22", ConfigPath: "/etc/supervisor/supervisord.conf", Service: "supervisor",
		Socket: "unix:/var/run/supervisor.sock", SocketDenied: true, SocketOwner: "root", SocketGroup: "supervisor", SocketMode: "0770", Chown: "root:supervisor"})
	fix := status(rows, "socket").Fix
	if len(fix) != 3 || fix[1].Text != "On the server, add abir to that group." || !strings.HasPrefix(fix[2].Text, "Then check again") {
		t.Fatalf("%+v", fix)
	}
}

func TestRemoteReportStopsEarly(t *testing.T) {
	rows := RemoteReport(RemoteFacts{SSHError: "refused"})
	if status(rows, "ssh").Status != RowError || status(rows, "version").Status != RowSkipped || !Blocked(rows) {
		t.Fatalf("%+v", rows)
	}
	rows = RemoteReport(RemoteFacts{Login: "u@h:22", Sudo: true, SudoResult: "refused"})
	if status(rows, "sudo").Status != RowError || status(rows, "config").Status != RowSkipped {
		t.Fatalf("%+v", rows)
	}
	rows = RemoteReport(RemoteFacts{HostKeyChanged: true, KnownHostsFix: "ssh-keygen -R h"})
	if status(rows, "ssh").Fix[0].Command != "ssh-keygen -R h" {
		t.Fatalf("%+v", rows)
	}
}

func TestRemoteReportWriteRow(t *testing.T) {
	base := RemoteFacts{Login: "deploy@h:22", HostKey: "SHA256:x", BridgeUser: "deploy",
		ConfigPath: "/etc/supervisor/supervisord.conf", Via: "forward", Socket: "unix:/run/supervisor.sock",
		IncludeDir: "/etc/supervisor/conf.d", IncludeChecked: true, Version: "4.2.5"}

	f := base
	f.IncludeWritable = true
	if r := status(RemoteReport(f), "write"); r.Status != RowOK || !strings.Contains(r.Detail, "deploy can add and change configs") {
		t.Errorf("writable: %+v", r)
	}

	f = base
	f.IncludeGroup = "root"
	r := status(RemoteReport(f), "write")
	if r.Status != RowWarning || !strings.Contains(r.Detail, "deploy can't write /etc/supervisor/conf.d") {
		t.Fatalf("not writable: %+v", r)
	}
	var cmds []string
	for _, s := range r.Fix {
		cmds = append(cmds, s.Command)
	}
	all := strings.Join(cmds, "\n")
	if !strings.Contains(r.Fix[0].Text, "Use sudo") || !strings.Contains(all, "usermod -aG supervisor deploy") ||
		!strings.Contains(all, "chgrp supervisor /etc/supervisor/conf.d && sudo chmod g+w") {
		t.Fatalf("fix: %+v", r.Fix)
	}
	if Blocked(RemoteReport(f)) {
		t.Error("a read-only include folder must not block the tab")
	}

	// Already in the folder's group: only the chmod, no reconnect.
	f.IncludeGroup, f.Groups = "deploy", []string{"deploy", "www-data"}
	r = status(RemoteReport(f), "write")
	if len(r.Fix) != 2 || !strings.Contains(r.Fix[1].Command, "chgrp deploy") {
		t.Fatalf("member fix: %+v", r.Fix)
	}

	f = base
	f.IncludeMissing = true
	if r := status(RemoteReport(f), "write"); r.Status != RowWarning || !strings.Contains(r.Fix[0].Command, "mkdir -p") {
		t.Errorf("missing: %+v", r)
	}
}
