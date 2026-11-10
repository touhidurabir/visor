package app

import (
	"strings"
	"testing"

	"github.com/touhidurabir/visor/internal/notify"
	"github.com/touhidurabir/visor/internal/platform"
)

func TestLinuxOptionsEscapeTheBody(t *testing.T) {
	o := linuxOptions(notify.Message{
		ID: "n1", IssueID: "i1", Thread: "/etc/supervisor/conf.d/web.ini",
		Title: "🔴 web can't start",
		Body:  "web.ini: sh -c 'a && b' exited <group> 2",
	})
	if o.Body != "web.ini: sh -c 'a &amp;&amp; b' exited &lt;group&gt; 2" {
		t.Errorf("body = %q", o.Body)
	}
	if o.Title != "🔴 web can't start" || o.ID != "n1" || o.ThreadID != "/etc/supervisor/conf.d/web.ini" || o.Data["issueId"] != "i1" {
		t.Errorf("options = %+v", o)
	}
	if o.Subtitle != "" {
		t.Errorf("Linux has no subtitle: %q", o.Subtitle)
	}
	if got := linuxOptions(notify.Message{Subtitle: "a & b", Body: "c"}).Body; got != "a &amp; b\nc" {
		t.Errorf("subtitle joins the body: %q", got)
	}
}

func TestUnavailableReasonPerBuild(t *testing.T) {
	if err := unavailableReason(true, platform.Linux); err.Error() != "notifications aren't available in this build" {
		t.Errorf("server = %v", err)
	}
	if err := unavailableReason(false, platform.Darwin); !strings.Contains(err.Error(), "Visor.app") {
		t.Errorf("macOS = %v", err)
	}
	if err := unavailableReason(false, platform.Linux); !strings.Contains(err.Error(), "no desktop notification service") {
		t.Errorf("Linux = %v", err)
	}
}
