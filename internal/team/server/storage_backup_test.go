package server

import (
	"context"
	"testing"
	"time"

	"werkbord/internal/team/config"
)

func TestBackupDestinationDoesNotImplyAutomaticBackups(t *testing.T) {
	for _, every := range []time.Duration{0, time.Nanosecond, config.DefaultBackupEvery} {
		cfg := config.Default()
		cfg.BackupDir, cfg.BackupEvery = t.TempDir(), every
		status := newBackupRunner(&Storage{cfg: cfg}).status(context.Background())
		if !status.Configured || status.Automatic != (every > 0) {
			t.Fatalf("interval %v: %+v", every, status)
		}
	}
	cfg := config.Default()
	status := newBackupRunner(&Storage{cfg: cfg}).status(context.Background())
	if status.Configured || status.Automatic {
		t.Fatalf("no destination: %+v", status)
	}
}
