//go:build !windows

package main

func isWindowsService() bool { return false }

func runWindowsService() error { return nil }
