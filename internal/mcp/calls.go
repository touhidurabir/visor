package mcp

import "sync"

// keepCalls is how many calls Recent calls keeps (spec §6).
const keepCalls = 100

// calls is the last 100 tool calls, in memory for this session only.
type calls struct {
	mu   sync.Mutex
	list []Call // oldest first
}

func (c *calls) add(call Call) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.list = append(c.list, call)
	if over := len(c.list) - keepCalls; over > 0 {
		c.list = append(c.list[:0:0], c.list[over:]...)
	}
}

// newest returns the calls, newest first.
func (c *calls) newest() []Call {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Call, len(c.list))
	for i, call := range c.list {
		out[len(c.list)-1-i] = call
	}
	return out
}
