//go:build !windows

package rqlitetest

import "testing"

func TestNodePortsAreDistinct(t *testing.T) {
	ports := freePorts(t, 32)
	seen := make(map[int]bool)
	for _, port := range ports {
		if port <= 0 || seen[port] {
			t.Fatalf("invalid or reused port %d in %v", port, ports)
		}
		seen[port] = true
	}
}
