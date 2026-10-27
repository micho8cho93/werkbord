package testport

import (
	"net"
	"strconv"
	"testing"
)

func TestPortsComeFromBelowTheEphemeralRangeAndAreNeverRepeated(t *testing.T) {
	seen := map[int]bool{}
	for i := 0; i < 300; i++ {
		var port int
		if i%2 == 0 {
			port = TCP(t)
		} else {
			port = UDP(t)
		}
		if port < low || port >= low+span {
			t.Fatalf("port %d is outside the reserved range", port)
		}
		if seen[port] {
			t.Fatalf("port %d was given twice", port)
		}
		seen[port] = true
	}
}

func TestAPortInUseIsNotGiven(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for i := 0; i < 200; i++ {
		if TCP(t) == l.Addr().(*net.TCPAddr).Port {
			t.Fatal("a port that is in use was given")
		}
	}
}

func TestAGivenPortCanBeBound(t *testing.T) {
	port := TCP(t)
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("the port %d could not be bound: %v", port, err)
	}
	_ = l.Close()
}
