package replicated

import (
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// lsofOwner tells which rqlited process holds the local end of a connection that arrived from remote: the way a
// test that cuts the network between nodes finds out whose connection it is. It needs lsof; a test that cuts the
// network skips without it. (-a: lsof joins its selections with OR unless told otherwise, and a connection's other end,
// held by the test itself, or any rqlited at all, would be named instead of the node that made it.)
func lsofOwner(remote net.Addr) int {
	tcp, ok := remote.(*net.TCPAddr)
	if !ok {
		return 0
	}
	out, err := exec.Command("lsof", "-nP", "-a", "-iTCP:"+strconv.Itoa(tcp.Port), "-sTCP:ESTABLISHED", "-c", "rqlited", "-Fp").Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "p") {
			if pid, err := strconv.Atoi(line[1:]); err == nil {
				return pid
			}
		}
	}
	return 0
}

func needLsof(t testing.TB) {
	t.Helper()
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof is needed to cut the network between database nodes in this test")
	}
}
