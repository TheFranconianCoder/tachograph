// Package stream fans status snapshots out to Server-Sent Events clients.
//
// Every subscriber keeps only the newest message: a status snapshot is
// self-contained, so a slow client never needs the frames it missed.
package stream

import "sync"

// Hub is an in-process broadcast hub.
type Hub struct {
	mu   sync.Mutex
	subs map[*Subscriber]struct{}
}

// NewHub creates an empty hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[*Subscriber]struct{})}
}

// Subscribe registers a client. The caller must Unsubscribe it.
func (h *Hub) Subscribe() *Subscriber {
	sub := &Subscriber{
		hub:  h,
		out:  make(chan []byte, 1),
		done: make(chan struct{}),
	}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

// Unsubscribe removes a client. Safe to call more than once.
func (h *Hub) Unsubscribe(sub *Subscriber) {
	sub.Close()
}

// Subscribers reports the current client count.
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Publish queues a message for every subscriber and never blocks.
func (h *Hub) Publish(message []byte) {
	if len(message) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		sub.push(message)
	}
}

// Subscriber is one SSE client.
type Subscriber struct {
	hub  *Hub
	out  chan []byte
	done chan struct{}
	once sync.Once
}

// Out yields the newest queued message.
func (s *Subscriber) Out() <-chan []byte {
	return s.out
}

// Done is closed when the subscriber is removed.
func (s *Subscriber) Done() <-chan struct{} {
	return s.done
}

// Close removes the subscriber from the hub.
func (s *Subscriber) Close() {
	s.once.Do(func() {
		close(s.done)
		s.hub.mu.Lock()
		delete(s.hub.subs, s)
		s.hub.mu.Unlock()
	})
}

// push replaces the queued message, so a burst collapses into the newest one.
func (s *Subscriber) push(message []byte) {
	select {
	case <-s.done:
		return
	default:
	}
	select {
	case s.out <- message:
		return
	default:
	}
	// The slot holds an older snapshot; replace it with the newer one.
	select {
	case <-s.out:
	default:
	}
	select {
	case s.out <- message:
	default:
	}
}
