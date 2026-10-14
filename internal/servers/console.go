package servers

import (
	"sync"
	"time"
)

// Console is a ring buffer of recent output lines that batches new lines
// for publishing (one event per ~40ms instead of one per line).
type Console struct {
	mu      sync.Mutex
	buf     []string
	head    int
	size    int
	pending []string
	timer   *time.Timer
	emit    func([]string)
}

func NewConsole(capacity int, emit func([]string)) *Console {
	return &Console{buf: make([]string, capacity), emit: emit}
}

const maxLineLen = 8192

func (c *Console) Add(line string) {
	if len(line) > maxLineLen {
		line = line[:maxLineLen] + "…"
	}
	c.mu.Lock()
	if c.size < len(c.buf) {
		c.buf[(c.head+c.size)%len(c.buf)] = line
		c.size++
	} else {
		c.buf[c.head] = line
		c.head = (c.head + 1) % len(c.buf)
	}
	c.pending = append(c.pending, line)
	if c.timer == nil {
		c.timer = time.AfterFunc(40*time.Millisecond, c.flush)
	}
	c.mu.Unlock()
}

func (c *Console) flush() {
	c.mu.Lock()
	p := c.pending
	c.pending = nil
	c.timer = nil
	c.mu.Unlock()
	if len(p) > 0 && c.emit != nil {
		c.emit(p)
	}
}

// Lines returns up to max of the most recent lines (oldest first).
func (c *Console) Lines(max int) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.size
	if max > 0 && max < n {
		n = max
	}
	out := make([]string, n)
	start := c.head + c.size - n
	for i := range n {
		out[i] = c.buf[(start+i)%len(c.buf)]
	}
	return out
}

func (c *Console) Clear() {
	c.mu.Lock()
	c.head, c.size = 0, 0
	c.mu.Unlock()
}
