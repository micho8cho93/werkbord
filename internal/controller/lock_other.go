//go:build !unix && !windows

package controller

// acquireLock is a no-op on platforms without a lock this code knows how to take.
func acquireLock(string) (func(), error) { return func() {}, nil }
