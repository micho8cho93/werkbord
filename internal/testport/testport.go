// Package testport gives a test a loopback port to bind a moment later.
//
// A test that asks the operating system for a free port (listen on :0), closes it and hands the number to the program under test has
// a gap in which the port can be taken: the system's ephemeral range (32768 and up on Linux, 49152 and up on macOS and Windows) is
// also where every outgoing connection of every test in every package takes its own port, so on a busy runner the "free" port is
// somewhere else's by the time it is bound. These ports come from below that range, each is checked to be free when chosen, and a
// port is never given twice in one process.
package testport

import (
	"fmt"
	"math/rand/v2"
	"net"
	"sync"
	"testing"
)

const (
	low  = 20000 // below every platform's ephemeral range
	span = 10000
)

var (
	mu    sync.Mutex
	given = map[int]bool{}
)

// TCP returns a loopback TCP port that was free when it was chosen and has not been given to any other test in this process.
func TCP(t testing.TB) int {
	t.Helper()
	return pick(t, func(port int) error {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			err = l.Close()
		}
		return err
	})
}

// UDP is TCP for a UDP port.
func UDP(t testing.TB) int {
	t.Helper()
	return pick(t, func(port int) error {
		c, err := net.ListenPacket("udp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			err = c.Close()
		}
		return err
	})
}

func pick(t testing.TB, free func(port int) error) int {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	for range 500 {
		port := low + rand.IntN(span)
		if given[port] || free(port) != nil {
			continue
		}
		given[port] = true
		return port
	}
	t.Fatal("testport: no free port found in the reserved range")
	return 0
}
