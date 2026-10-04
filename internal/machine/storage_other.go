//go:build !darwin && !linux

package machine

func storage(string) *int64 { return nil }
