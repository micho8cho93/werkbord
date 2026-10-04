package agent

import (
	"fmt"
	"sync"

	"devboard/internal/domain"
)

// defaultQueueLimit is how many undelivered output events an adapter keeps
// for a consumer that has fallen behind. It bounds memory if the controller
// stalls (a slow disk) while an agent is chatty.
const defaultQueueLimit = 4096

// eventQueue connects an adapter's reader goroutines to the consumer of
// Session.Events. Pushing never blocks, so an adapter can emit from anywhere
// (including from inside Respond, which the controller calls while holding its
// own lock) without risking a deadlock, and the agent's stdout keeps draining.
//
// Order is preserved. When the consumer is so far behind that limit events are
// waiting, further output is dropped and one notice, counting what was lost,
// is queued when there is room again. Control events (questions, turn ends,
// session refs) are never dropped: losing one would leave the run stuck.
type eventQueue struct {
	mu      sync.Mutex
	cond    *sync.Cond
	items   []Event
	limit   int
	dropped int
	closed  bool
	out     chan Event
}

func newEventQueue(limit int) *eventQueue {
	if limit <= 0 {
		limit = defaultQueueLimit
	}
	q := &eventQueue{limit: limit, out: make(chan Event, 64)}
	q.cond = sync.NewCond(&q.mu)
	go q.pump()
	return q
}

func (q *eventQueue) events() <-chan Event { return q.out }

// push queues ev. It reports false if the queue is closed.
func (q *eventQueue) push(ev Event) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}
	if ev.Kind == KindOutput {
		if len(q.items) >= q.limit {
			q.dropped++
			return true
		}
		if q.dropped > 0 {
			q.items = append(q.items, Event{Kind: KindOutput, Stream: domain.StreamSystem,
				Text: fmt.Sprintf("%d lines of output were dropped because the controller fell behind", q.dropped)})
			q.dropped = 0
		}
	}
	q.items = append(q.items, ev)
	q.cond.Signal()
	return true
}

// close ends the queue: what is queued is still delivered, then Events closes.
func (q *eventQueue) close() {
	q.mu.Lock()
	q.closed = true
	if q.dropped > 0 {
		q.items = append(q.items, Event{Kind: KindOutput, Stream: domain.StreamSystem,
			Text: fmt.Sprintf("%d lines of output were dropped because the controller fell behind", q.dropped)})
		q.dropped = 0
	}
	q.cond.Broadcast()
	q.mu.Unlock()
}

func (q *eventQueue) pump() {
	defer close(q.out)
	for {
		q.mu.Lock()
		for len(q.items) == 0 && !q.closed {
			q.cond.Wait()
		}
		if len(q.items) == 0 {
			q.mu.Unlock()
			return
		}
		ev := q.items[0]
		q.items[0] = Event{}
		q.items = q.items[1:]
		if len(q.items) == 0 {
			q.items = nil
		}
		q.mu.Unlock()
		q.out <- ev
	}
}
