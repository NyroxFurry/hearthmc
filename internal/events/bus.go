// Package events is a small in-process pub/sub bus used to push live
// updates (console output, status, stats, player events) to WebSocket clients
// and to internal consumers like webhooks.
package events

import (
	"sync"
	"sync/atomic"
)

// Event types
const (
	Status     = "status"     // server state change
	Console    = "console"    // console lines (Data: []string)
	Stats      = "stats"      // resource usage
	Players    = "players"    // online players changed
	Install    = "install"    // install progress
	Backup     = "backup"     // backup progress / finished
	MCEvent    = "mcevent"    // join/leave/death/advancement/chat
	ServerMeta = "servermeta" // server created/updated/deleted
	Map        = "map"        // map render progress
	Task       = "task"       // long running task progress (java download, plugin install ...)
)

type Event struct {
	Type   string `json:"t"`
	Server string `json:"s,omitempty"`
	Data   any    `json:"d,omitempty"`
}

type Sub struct {
	C        chan Event
	filter   func(Event) bool
	overflow atomic.Bool
	closed   atomic.Bool
}

// Overflowed reports whether events were dropped because the subscriber was too slow.
func (s *Sub) Overflowed() bool { return s.overflow.Load() }

// TakeOverflow reports and clears the overflow flag.
func (s *Sub) TakeOverflow() bool { return s.overflow.Swap(false) }

type Bus struct {
	mu   sync.RWMutex
	subs map[*Sub]struct{}
}

func NewBus() *Bus { return &Bus{subs: map[*Sub]struct{}{}} }

// Subscribe registers a subscriber. filter may be nil (receive everything).
func (b *Bus) Subscribe(buffer int, filter func(Event) bool) *Sub {
	s := &Sub{C: make(chan Event, buffer), filter: filter}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

func (b *Bus) Unsubscribe(s *Sub) {
	b.mu.Lock()
	if _, ok := b.subs[s]; ok {
		delete(b.subs, s)
		if s.closed.CompareAndSwap(false, true) {
			close(s.C)
		}
	}
	b.mu.Unlock()
}

// Publish delivers e to all matching subscribers without blocking.
// Slow subscribers lose events and get their overflow flag set.
func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		if s.filter != nil && !s.filter(e) {
			continue
		}
		select {
		case s.C <- e:
		default:
			s.overflow.Store(true)
		}
	}
}
