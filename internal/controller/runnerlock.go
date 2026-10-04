package controller

import "path/filepath"

// LockRunner holds a distinct per-machine executor lock. The runner never opens
// the controller database, and both roles can run on this machine at once.
func LockRunner(dir string) (func(), error) { return acquireLock(filepath.Join(dir, "worker.lock")) }
