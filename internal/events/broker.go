// Package events delivers committed domain events to live subscribers.
//
// Durability is not this package's job: every event is written to the event
// log in the same transaction as the change it describes, and only published
// here after commit. The broker is therefore allowed to be lossy. A subscriber
// that falls behind is disconnected rather than allowed to block publishers,
// and is expected to catch up from the event log using the last Seq it saw.
package events

import (
	"sync"

	"devboard/internal/domain"
)

// Publisher fans committed events out to live subscribers.
type Publisher interface {
	Publish(events ...domain.Event)
}

// Subscriber hands out live subscriptions.
type Subscriber interface {
	Subscribe(buffer int) *Subscription
}

// Broker is an in-process Publisher and Subscriber.
type Broker struct {
	mu     sync.Mutex
	subs   map[*Subscription]struct{}
	closed bool
}

var (
	_ Publisher  = (*Broker)(nil)
	_ Subscriber = (*Broker)(nil)
)

// NewBroker returns an empty broker.
func NewBroker() *Broker {
	return &Broker{subs: make(map[*Subscription]struct{})}
}

// Subscription receives events on C until it is closed. C is closed when the
// subscriber calls Close, when the broker shuts down, or when the subscriber
// falls more than buffer events behind (Dropped then reports true).
type Subscription struct {
	C <-chan domain.Event

	ch      chan domain.Event
	broker  *Broker
	dropped bool // guarded by broker.mu
}

// Subscribe registers a new subscription with the given channel buffer.
func (b *Broker) Subscribe(buffer int) *Subscription {
	if buffer < 1 {
		buffer = 1
	}
	ch := make(chan domain.Event, buffer)
	s := &Subscription{C: ch, ch: ch, broker: b}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		close(ch)
		return s
	}
	b.subs[s] = struct{}{}
	return s
}

// Publish delivers events to every subscriber without blocking.
func (b *Broker) Publish(events ...domain.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs {
		for _, e := range events {
			select {
			case s.ch <- e:
			default:
				s.dropped = true
				b.removeLocked(s)
			}
			if s.dropped {
				break
			}
		}
	}
}

// Close disconnects all subscribers. Later Subscribe calls get a closed
// subscription and Publish becomes a no-op.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for s := range b.subs {
		b.removeLocked(s)
	}
}

// Len reports the number of live subscriptions.
func (b *Broker) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

func (b *Broker) removeLocked(s *Subscription) {
	if _, ok := b.subs[s]; ok {
		delete(b.subs, s)
		close(s.ch)
	}
}

// Close unsubscribes. It is safe to call more than once.
func (s *Subscription) Close() {
	s.broker.mu.Lock()
	defer s.broker.mu.Unlock()
	s.broker.removeLocked(s)
}

// Dropped reports whether the subscription was closed for falling behind.
func (s *Subscription) Dropped() bool {
	s.broker.mu.Lock()
	defer s.broker.mu.Unlock()
	return s.dropped
}
