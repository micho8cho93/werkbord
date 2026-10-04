package events

import (
	"testing"

	"devboard/internal/domain"
)

func TestPublishReachesAllSubscribers(t *testing.T) {
	b := NewBroker()
	s1, s2 := b.Subscribe(4), b.Subscribe(4)
	b.Publish(domain.Event{Seq: 1}, domain.Event{Seq: 2})
	for _, s := range []*Subscription{s1, s2} {
		if e := <-s.C; e.Seq != 1 {
			t.Fatalf("first event seq = %d", e.Seq)
		}
		if e := <-s.C; e.Seq != 2 {
			t.Fatalf("second event seq = %d", e.Seq)
		}
	}
}

func TestSlowSubscriberIsDroppedNotBlocking(t *testing.T) {
	b := NewBroker()
	slow := b.Subscribe(1)
	fast := b.Subscribe(10)
	b.Publish(domain.Event{Seq: 1}, domain.Event{Seq: 2}, domain.Event{Seq: 3})

	if !slow.Dropped() {
		t.Fatal("slow subscriber should be dropped")
	}
	n := 0
	for range slow.C { // drains the buffered event, then sees the close
		n++
	}
	if n != 1 {
		t.Fatalf("slow subscriber got %d events before close, want 1", n)
	}
	if fast.Dropped() || len(fast.C) != 3 {
		t.Fatalf("fast subscriber: dropped=%v buffered=%d", fast.Dropped(), len(fast.C))
	}
	if b.Len() != 1 {
		t.Fatalf("live subscriptions = %d, want 1", b.Len())
	}
}

func TestCloseDisconnectsEveryone(t *testing.T) {
	b := NewBroker()
	s := b.Subscribe(1)
	s.Close()
	s.Close() // idempotent
	s2 := b.Subscribe(1)
	b.Close()
	if _, ok := <-s2.C; ok {
		t.Fatal("expected closed channel after broker Close")
	}
	if _, ok := <-b.Subscribe(1).C; ok {
		t.Fatal("subscribe after Close should return a closed subscription")
	}
	b.Publish(domain.Event{Seq: 1}) // must not panic
}
