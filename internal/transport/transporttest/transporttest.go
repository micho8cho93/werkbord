// Package transporttest is the conformance suite for transport.Transport: the
// behaviour every implementation must have, run against each of them (the
// in-memory one, and the Tailscale one in internal/netprivate), so that code
// written against the contract does not depend on which network is behind it.
package transporttest

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"devboard/internal/transport"
)

// Pair returns two stopped transports on the same private network. Cleanup of
// anything else it made is up to it (t.Cleanup).
type Pair func(t *testing.T) (a, b transport.Transport)

// WaitReady polls until the transport is ready.
func WaitReady(t *testing.T, tr transport.Transport, timeout time.Duration) transport.Status {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(timeout)
	for {
		st, err := tr.Status(ctx)
		if err == nil && st.State == transport.StateReady {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("transport never became ready: %+v, %v", st, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Run runs the suite. ready bounds how long a transport may take to come up.
func Run(t *testing.T, newPair Pair, ready time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Run("not started", func(t *testing.T) {
		a, _ := newPair(t)
		if _, err := a.Local(ctx); !errors.Is(err, transport.ErrNotStarted) {
			t.Errorf("Local before Start = %v, want ErrNotStarted", err)
		}
		if _, err := a.Dial(ctx, "127.0.0.1:1"); err == nil {
			t.Error("Dial before Start succeeded")
		}
		if st, err := a.Status(ctx); err != nil || st.State != transport.StateStopped {
			t.Errorf("Status before Start = %+v, %v; want stopped", st, err)
		}
	})

	t.Run("start and stop are idempotent", func(t *testing.T) {
		a, _ := newPair(t)
		for i := 0; i < 2; i++ {
			if err := a.Start(ctx); err != nil {
				t.Fatal(err)
			}
		}
		WaitReady(t, a, ready)
		for i := 0; i < 2; i++ {
			if err := a.Stop(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if st, _ := a.Status(ctx); st.State != transport.StateStopped {
			t.Errorf("state after Stop = %q", st.State)
		}
	})

	t.Run("a node has an identity and an address", func(t *testing.T) {
		a, _ := newPair(t)
		start(t, ctx, ready, a)
		n, err := a.Local(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if n.ID == "" || len(n.Addrs) == 0 {
			t.Fatalf("local node has no identity or address: %+v", n)
		}
		st, _ := a.Status(ctx)
		if st.Node.ID != n.ID {
			t.Errorf("Status.Node %q differs from Local %q", st.Node.ID, n.ID)
		}
	})

	t.Run("one node reaches another, and the connection says who it is from", func(t *testing.T) {
		a, b := newPair(t)
		start(t, ctx, ready, a, b)
		na, _ := a.Local(ctx)
		nb, _ := b.Local(ctx)
		l, err := a.Listen(ctx, ":4100")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		type accepted struct {
			c   net.Conn
			err error
		}
		got := make(chan accepted, 1)
		go func() { c, err := l.Accept(); got <- accepted{c, err} }()

		c, err := b.Dial(ctx, net.JoinHostPort(na.Addrs[0].String(), "4100"))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		srv := <-got
		if srv.err != nil {
			t.Fatal(srv.err)
		}
		defer srv.c.Close()

		go func() { _, _ = io.WriteString(c, "ping") }() // an in-memory connection is synchronous
		buf := make([]byte, 4)
		if _, err := io.ReadFull(srv.c, buf); err != nil || string(buf) != "ping" {
			t.Fatalf("read %q, %v", buf, err)
		}
		if m, ok := transport.MetaOf(srv.c); !ok {
			t.Error("an accepted connection carries no metadata")
		} else if m.RemoteNode != "" && m.RemoteNode != nb.ID {
			t.Errorf("accepted connection says it is from %q, want %q", m.RemoteNode, nb.ID)
		}
		if m, ok := transport.MetaOf(c); !ok {
			t.Error("a dialled connection carries no metadata")
		} else if m.RemoteNode != "" && m.RemoteNode != na.ID {
			t.Errorf("dialled connection says it goes to %q, want %q", m.RemoteNode, na.ID)
		}
	})

	t.Run("a listener's port is taken until it is closed", func(t *testing.T) {
		a, _ := newPair(t)
		start(t, ctx, ready, a)
		l, err := a.Listen(ctx, ":4101")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.Listen(ctx, ":4101"); err == nil {
			t.Error("a second listener took the same port")
		}
		_ = l.Close()
		l2, err := a.Listen(ctx, ":4101")
		if err != nil {
			t.Fatalf("port not freed by Close: %v", err)
		}
		_ = l2.Close()
	})

	t.Run("nobody is listening", func(t *testing.T) {
		a, b := newPair(t)
		start(t, ctx, ready, a, b)
		na, _ := a.Local(ctx)
		dctx, dcancel := context.WithTimeout(ctx, 10*time.Second)
		defer dcancel()
		if c, err := b.Dial(dctx, net.JoinHostPort(na.Addrs[0].String(), strconv.Itoa(4999))); err == nil {
			_ = c.Close()
			t.Error("dialled a port nothing listens on")
		}
	})

	t.Run("peers", func(t *testing.T) {
		a, b := newPair(t)
		start(t, ctx, ready, a, b)
		nb, _ := b.Local(ctx)
		deadline := time.Now().Add(ready)
		for {
			peers, err := a.Peers(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range peers {
				if p.Node.ID == nb.ID {
					return
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("a never saw b among its peers: %+v", peers)
			}
			time.Sleep(50 * time.Millisecond)
		}
	})

	t.Run("stop closes listeners", func(t *testing.T) {
		a, _ := newPair(t)
		start(t, ctx, ready, a)
		l, err := a.Listen(ctx, ":4102")
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := l.Accept(); done <- err }()
		if err := a.Stop(ctx); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err == nil {
				t.Error("Accept succeeded after Stop")
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Accept still blocked after Stop")
		}
	})
}

func start(t *testing.T, ctx context.Context, ready time.Duration, trs ...transport.Transport) {
	t.Helper()
	for _, tr := range trs {
		if err := tr.Start(ctx); err != nil {
			t.Fatal(err)
		}
		tr := tr
		t.Cleanup(func() { _ = tr.Stop(context.Background()) })
	}
	for _, tr := range trs {
		WaitReady(t, tr, ready)
	}
}
