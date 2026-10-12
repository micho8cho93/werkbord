//go:build !darwin && !linux && !windows

package teamlink

func serviceInstalled() bool { return false }
