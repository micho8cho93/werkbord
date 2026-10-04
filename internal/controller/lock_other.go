//go:build !unix

package controller

// acquireLock is a no-op on platforms without flock. Windows support is not a
// goal yet; see docs/ARCHITECTURE.md.
func acquireLock(string) (func(), error) { return func() {}, nil }
