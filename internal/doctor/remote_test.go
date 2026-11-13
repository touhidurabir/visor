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
		t.Error("sudo off and writable are info")
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
