package remote

import (
	"errors"
	"time"
)

// Keepalive settings: a request every KeepaliveInterval; the connection is
// closed when a request stays unanswered for KeepaliveMax intervals (45s by
// default, about a minute from the last answer).
var (
	KeepaliveInterval = 15 * time.Second
	KeepaliveMax      = 3
)

// ErrKeepalive means the server stopped answering keepalives.
var ErrKeepalive = errors.New("the server stopped answering")

// keepalive takes the settings as arguments, read when the connection is
// made, so changing them later never races with a running connection.
func (c *Conn) keepalive(interval time.Duration, most int) {
	t := time.NewTicker(interval)
	defer t.Stop()
	var pending chan error
	missed := 0
	for {
		select {
		case <-c.closed:
			return
		case err := <-pending:
			pending = nil
			if err != nil {
				c.drop(err)
				return
			}
			missed = 0
			continue
		case <-t.C:
		}
		if pending != nil {
			missed++
			if missed >= most {
				c.drop(ErrKeepalive)
				return
			}
			continue
		}
		pending = make(chan error, 1)
		go func(ch chan error) {
			_, _, err := c.client.SendRequest("keepalive@openssh.com", true, nil)
			ch <- err
		}(pending)
	}
}

func (c *Conn) drop(err error) {
	c.markClosed(err)
	_ = c.client.Close()
}
