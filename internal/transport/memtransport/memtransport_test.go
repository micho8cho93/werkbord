package memtransport

import (
	"context"
	"net"
	"testing"
	"time"

	"devboard/internal/transport"
	"devboard/internal/transport/transporttest"
)

func TestMemoryTransportConforms(t *testing.T) {
	transporttest.Run(t, func(t *testing.T) (transport.Transport, transport.Transport) {
		n := NewNetwork()
		return n.NewNode("a"), n.NewNode("b")
	}, 5*time.Second)
}

func TestACutLinkIsUnreachableUntilHealed(t *testing.T) {
	ctx := context.Background()
	n := NewNetwork()
	a, b := n.NewNode("a"), n.NewNode("b")
	for _, x := range []*Node{a, b} {
		if err := x.Start(ctx); err != nil {
			t.Fatal(err)
		}
	}
	l, err := a.Listen(ctx, ":4000")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	addr := net.JoinHostPort(a.addr.String(), "4000")

	a.Cut(b)
	if _, err := b.Dial(ctx, addr); err == nil {
		t.Fatal("dialled across a cut link")
	}
	peers, _ := b.Peers(ctx)
	if len(peers) != 1 || peers[0].Reachable {
		t.Fatalf("a cut peer is still reachable: %+v", peers)
	}
	a.Heal(b)
	c, err := b.Dial(ctx, addr)
	if err != nil {
		t.Fatalf("link not healed: %v", err)
	}
	c.Close()
}
