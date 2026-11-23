package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const initBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`

func post(t *testing.T, url string, h map[string]string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(initBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range h {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func TestHandlerAuth(t *testing.T) {
	var token atomic.Value
	token.Store("s3cret")
	srv := sdk.NewServer(&sdk.Implementation{Name: "visor", Version: "test"}, nil)
	ts := httptest.NewServer(Handler(srv, func() string { return token.Load().(string) }))
	defer ts.Close()
	url := ts.URL + "/mcp"

	for _, c := range []struct {
		name string
		h    map[string]string
		want int
	}{
		{"no token", nil, http.StatusUnauthorized},
		{"wrong token", map[string]string{"Authorization": "Bearer nope"}, http.StatusUnauthorized},
		{"not bearer", map[string]string{"Authorization": "s3cret"}, http.StatusUnauthorized},
		{"right token", map[string]string{"Authorization": "Bearer s3cret"}, http.StatusOK},
		{"from a web page", map[string]string{"Authorization": "Bearer s3cret", "Origin": "http://evil.example"}, http.StatusForbidden},
		{"rebinding host", map[string]string{"Authorization": "Bearer s3cret", "Host": "evil.example"}, http.StatusForbidden},
	} {
		if got := post(t, url, c.h); got != c.want {
			t.Errorf("%s: status %d, want %d", c.name, got, c.want)
		}
	}

	token.Store("n3w")
	if got := post(t, url, map[string]string{"Authorization": "Bearer s3cret"}); got != http.StatusUnauthorized {
		t.Errorf("old token after regenerate: %d, want 401", got)
	}
	if got := post(t, url, map[string]string{"Authorization": "Bearer n3w"}); got != http.StatusOK {
		t.Errorf("new token: %d, want 200", got)
	}

	token.Store("")
	if got := post(t, url, map[string]string{"Authorization": "Bearer "}); got != http.StatusUnauthorized {
		t.Errorf("empty token: %d, want 401", got)
	}
}

func TestIdleSessionsExpire(t *testing.T) {
	old := sessionTimeout
	sessionTimeout = 300 * time.Millisecond
	t.Cleanup(func() { sessionTimeout = old })
	srv := sdk.NewServer(&sdk.Implementation{Name: "visor", Version: "test"}, nil)
	ts := httptest.NewServer(Handler(srv, func() string { return "t" }))
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(initBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer t")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	id := res.Header.Get("Mcp-Session-Id")
	if id == "" {
		t.Fatal("no session")
	}
	time.Sleep(time.Second)
	ping, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"ping"}`))
	ping.Header.Set("Content-Type", "application/json")
	ping.Header.Set("Accept", "application/json, text/event-stream")
	ping.Header.Set("Authorization", "Bearer t")
	ping.Header.Set("Mcp-Session-Id", id)
	res, err = http.DefaultClient.Do(ping)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("an idle session after its timeout: %d, want 404", res.StatusCode)
	}
}
