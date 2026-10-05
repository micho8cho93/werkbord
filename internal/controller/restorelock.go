package controller

// LockController fences offline database maintenance against a live controller.
func LockController(path string) (func(), error) { return acquireLock(path) }
