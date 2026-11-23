package mcp

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// sessionTimeout closes a client's session after it has been idle that
// long (Visor runs for days; an agent reconnects when it comes back).
var sessionTimeout = 30 * time.Minute

// Handler serves srv over Streamable HTTP for local agents. Every request
// needs "Authorization: Bearer <token>" (token is read per request, so a new
// token cuts old clients off at once). A request with an Origin header comes
// from a web page and is refused: agents on this computer send none, and
// refusing it stops a page from reaching Visor through the browser. The
// SDK's own check refuses a non-localhost Host (DNS rebinding).
func Handler(srv *sdk.Server, token func() string) http.Handler {
	h := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return srv },
		&sdk.StreamableHTTPOptions{SessionTimeout: sessionTimeout})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" {
			http.Error(w, "Visor's MCP server doesn't accept requests from web pages", http.StatusForbidden)
			return
		}
		if !authorized(r.Header.Get("Authorization"), token()) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="visor"`)
			http.Error(w, "missing or wrong token: copy it from Visor's MCP server page", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func authorized(header, token string) bool {
	got, ok := strings.CutPrefix(header, "Bearer ")
	return ok && token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}
