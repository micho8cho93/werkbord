//go:build !windows

package rqlitetest

import (
	"bytes"
	"io"
	"net"
	"sync"
	"time"
)

// A raftProxy sits in front of one node's Raft port. Every connection to the node comes through it, and
// the first bytes of one say who is calling (Raft's own message headers and rqlite's cluster requests both
// carry the sender's advertised address), so the proxy can refuse a caller the test has cut off, and close
// the connections it already has from that caller. That makes a partition between nodes, in both
// directions, without privileges and without touching the program.
type raftProxy struct {
	ln     net.Listener
	target string
	node   int
	c      *Cluster

	mu    sync.Mutex
	conns map[*proxied]struct{}
	done  chan struct{}
}

type proxied struct {
	src      int // the node that called, or -1 when it could not be told
	in, out  net.Conn
	closeOne sync.Once
}

func (p *proxied) close() {
	p.closeOne.Do(func() {
		_ = p.in.Close()
		_ = p.out.Close()
	})
}

func newRaftProxy(c *Cluster, node int, listen, target string) (*raftProxy, error) {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, err
	}
	p := &raftProxy{ln: ln, target: target, node: node, c: c, conns: map[*proxied]struct{}{}, done: make(chan struct{})}
	go p.accept()
	return p, nil
}

func (p *raftProxy) close() {
	select {
	case <-p.done:
		return
	default:
		close(p.done)
	}
	_ = p.ln.Close()
	p.mu.Lock()
	for c := range p.conns {
		c.close()
	}
	p.mu.Unlock()
}

func (p *raftProxy) accept() {
	for {
		in, err := p.ln.Accept()
		if err != nil {
			return
		}
		go p.serve(in)
	}
}

func (p *raftProxy) serve(in net.Conn) {
	// What the caller sends first says who it is: Raft's own messages and rqlite's cluster requests carry the sender's
	// advertised address, though not always in the first bytes to arrive.
	first := make([]byte, 0, 2048)
	buf := make([]byte, 2048)
	deadline := time.Now().Add(400 * time.Millisecond)
	src := -1
	for src < 0 && len(first) < 2048 && time.Now().Before(deadline) {
		_ = in.SetReadDeadline(deadline)
		n, err := in.Read(buf)
		first = append(first, buf[:n]...)
		if src = p.c.whoSent(first); src >= 0 || err != nil {
			break
		}
	}
	_ = in.SetReadDeadline(time.Time{})
	if src < 0 && p.c.opts.Owner != nil {
		// Some connections say nothing about who they are (rqlite's pooled requests between nodes): the machine says
		// which process owns the other end. It may take a moment for the connection to show up there.
		for try := 0; try < 8 && src < 0; try++ {
			if pid := p.c.opts.Owner(in.RemoteAddr()); pid > 0 {
				src = p.c.nodeOfPID(pid)
			}
			if src < 0 {
				time.Sleep(60 * time.Millisecond)
			}
		}
		if src < 0 {
			p.c.tracef("proxy for node %d: could not tell who made a connection from %s", p.node, in.RemoteAddr())
		}
	}
	p.c.tracef("proxy for node %d: connection from %d (%s): %q", p.node, src, in.RemoteAddr(), first[:min(len(first), 40)])
	if src >= 0 && p.c.blocked(src, p.node) {
		p.c.tracef("proxy for node %d: refused a connection from %d (cut off)", p.node, src)
		_ = in.Close()
		return
	}
	out, err := net.DialTimeout("tcp", p.target, 2*time.Second)
	if err != nil {
		_ = in.Close()
		return
	}
	pc := &proxied{src: src, in: in, out: out}
	p.mu.Lock()
	select {
	case <-p.done:
		p.mu.Unlock()
		pc.close()
		return
	default:
	}
	// Decided here, under the lock cut() takes, so that a connection whose caller was told apart cannot slip across a
	// partition that began while it was being dialled: either cut() sees it, or this does.
	if p.c.refuses(src, p.node) {
		p.mu.Unlock()
		p.c.tracef("proxy for node %d: refused a connection from %d at registration (a partition began while it was being made)", p.node, src)
		pc.close()
		return
	}
	p.conns[pc] = struct{}{}
	p.mu.Unlock()
	if len(first) > 0 {
		if _, err := out.Write(first); err != nil {
			pc.close()
			return
		}
	}
	go func() { _, _ = io.Copy(out, in); pc.close() }()
	go func() { _, _ = io.Copy(in, out); pc.close() }()
}

// cut closes the connections the proxy holds from callers that may no longer reach it. A connection whose caller could
// not be told is closed too: the node connects again, and is told apart (or refused) the second time, whereas leaving
// it open would let it carry traffic across a partition.
func (p *raftProxy) cut() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for c := range p.conns {
		if c.src < 0 || p.c.blocked(c.src, p.node) {
			p.c.tracef("proxy for node %d: closed a connection from %d at the cut", p.node, c.src)
			c.close()
			delete(p.conns, c)
		}
	}
}

// sniff reports which of the addresses appears first in what a caller sent first.
func sniff(first []byte, addrs []string) int {
	best, at := -1, len(first)+1
	for i, a := range addrs {
		if a == "" {
			continue
		}
		if j := bytes.Index(first, []byte(a)); j >= 0 && j < at {
			best, at = i, j
		}
	}
	return best
}

// pairProxy carries one host's connections to another node's HTTP API, so that a partition cuts what a host
// can reach of another host's node as well as what the nodes can reach of each other. The host (from) is the one
// whose storage layer connects; the node (to) is the one it connects to. While the pair is cut nothing listens at its
// address, so a connection is refused outright, as it is when a network is down.
type pairProxy struct {
	addr     string
	target   string
	from, to int
	c        *Cluster

	mu    sync.Mutex
	ln    net.Listener
	conns map[net.Conn]struct{}
	done  bool
}

func newPairProxy(c *Cluster, from, to int, target string) (*pairProxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &pairProxy{addr: ln.Addr().String(), target: target, from: from, to: to, c: c, conns: map[net.Conn]struct{}{}, ln: ln}
	go p.accept(ln)
	return p, nil
}

func (p *pairProxy) accept(ln net.Listener) {
	for {
		in, err := ln.Accept()
		if err != nil {
			return
		}
		out, err := net.DialTimeout("tcp", p.target, 2*time.Second)
		if err != nil {
			_ = in.Close()
			continue
		}
		p.mu.Lock()
		if p.done || p.ln != ln {
			// The pair was cut while this connection was being made: it must not outlive the partition.
			p.mu.Unlock()
			_ = out.Close()
			_ = in.Close()
			continue
		}
		p.conns[in], p.conns[out] = struct{}{}, struct{}{}
		p.mu.Unlock()
		go func() { _, _ = io.Copy(out, in); _ = out.Close(); _ = in.Close() }()
		go func() { _, _ = io.Copy(in, out); _ = out.Close(); _ = in.Close() }()
	}
}

// sync makes the proxy match the partition: it stops listening, and closes what it carries, while it is cut, and
// listens again at the same address when it is mended.
func (p *pairProxy) sync() {
	cut := p.c.blocked(p.from, p.to)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		return
	}
	switch {
	case cut && p.ln != nil:
		_ = p.ln.Close()
		p.ln = nil
		for c := range p.conns {
			_ = c.Close()
			delete(p.conns, c)
		}
	case !cut && p.ln == nil:
		for i := 0; i < 50; i++ {
			ln, err := net.Listen("tcp", p.addr)
			if err == nil {
				p.ln = ln
				go p.accept(ln)
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func (p *pairProxy) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done = true
	if p.ln != nil {
		_ = p.ln.Close()
	}
	for c := range p.conns {
		_ = c.Close()
	}
}
