package remote

import "time"

// Health is local evidence of a completed, authenticated controller sync. It
// contains no signing key, token, prompt or repository contents.
type Health struct {
	Version    string    `json:"version"`
	RunnerID   string    `json:"runnerId"`
	Controller string    `json:"controller"`
	SyncedAt   time.Time `json:"syncedAt"`
	Sequence   int64     `json:"sequence"`
}
