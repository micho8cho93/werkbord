//go:build race

package service

// raceEnabled reports whether the race detector is on. It is for measurements that it distorts and slows down.
const raceEnabled = true
