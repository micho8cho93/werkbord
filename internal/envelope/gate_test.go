package envelope

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestReplayMemoryStaysBoundedWhenExpiredMessageIDsAreReused(t *testing.T) {
	now := t0
	c := &MemoryReplayCache{Max: 2, Now: func() time.Time { return now }}
	for i := range 10 {
		if seen, err := c.Seen("device", "same-id", fmt.Sprintf("nonce-%d", i), now.Add(time.Second)); seen || err != nil {
			t.Fatal(seen, err)
		}
		if len(c.entries) > c.Max || len(c.nonces) > c.Max {
			t.Fatal("expired identifier reuse grew the replay cache beyond its limit")
		}
		now = now.Add(2 * time.Second)
	}
}

func TestHugeSignedTimesAndPathInjectionFailClosed(t *testing.T) {
	w := newWorld(t)
	e := w.sign()
	e.IssuedAt = 1
	e.ExpiresAt = 1<<63 - 1
	if err := e.checkShape(); err == nil {
		t.Fatal("duration overflow accepted")
	}
	for _, id := range []string{"x/../../run", "x?url=http://evil", "x%2fpath", "x\x00"} {
		if _, err := EncodePayload(ActionOpenTicketOnRunner, OpenTicketOnRunner{ProjectID: id, TicketID: "ttk_one"}); err == nil {
			t.Fatal("path/query injection accepted", id)
		}
	}
}

func FuzzSignedEnvelopeAndPayload(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"action":"exec","payload":{"command":"whoami"}}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > MaxPayload+4096 {
			return
		}
		var e Envelope
		if json.Unmarshal(b, &e) == nil {
			_ = e.checkShape()
			_, _ = DecodePayload(e)
		}
		_, _ = DecodePayload(Envelope{Action: ActionOpenTicketOnRunner, Payload: json.RawMessage(b)})
	})
}
