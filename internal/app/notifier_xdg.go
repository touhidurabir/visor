package app

import (
	"strings"

	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/touhidurabir/visor/internal/notify"
)

// linuxOptions is m as a freedesktop notification. There is no subtitle on
// Linux (the severity icon already leads the title). A server that
// advertises body-markup (GNOME, Plasma) reads the body as markup, so for
// it (markup) the body's &, < and > are escaped: a command line such as
// "a && b" or a "<group>" would otherwise vanish or break the banner.
func linuxOptions(m notify.Message, markup bool) notifications.NotificationOptions {
	body := m.Body
	if m.Subtitle != "" {
		body = m.Subtitle + "\n" + body
	}
	data := map[string]any{}
	if m.IssueID != "" {
		data["issueId"] = m.IssueID
	}
	o := notifications.NotificationOptions{
		ID:       m.ID,
		Title:    m.Title,
		Body:     body,
		Data:     data,
		ThreadID: m.Thread,
	}
	if markup {
		o.Body = escapeMarkup(body)
	}
	return o
}

var markupEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func escapeMarkup(s string) string { return markupEscaper.Replace(s) }
