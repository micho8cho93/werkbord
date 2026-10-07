package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/store/replicated"
)

// Replication is not a backup. A Workspace Host that is given a directory (WERKBORD_TEAM_BACKUP_DIR: a disk or a
// network share that belongs to the customer) takes a backup of the whole workspace database on a schedule, checks it
// by reading it back and restoring it into a scratch copy, and keeps the newest ones by the retention policy. A backup is
// also taken on request. Werkbord hosts nothing for this: the destination is a directory the customer owns.
type backupRunner struct {
	st *Storage

	mu         sync.Mutex
	running    bool
	lastAt     time.Time
	lastOK     bool
	lastErr    string
	verifiedAt time.Time
}

func newBackupRunner(st *Storage) *backupRunner { return &backupRunner{st: st} }

func (b *backupRunner) dest() (replicated.Destination, bool) {
	if b.st.cfg.BackupDir == "" {
		return nil, false
	}
	return replicated.DirDestination{Dir: b.st.cfg.BackupDir}, true
}

// now takes a backup, applies the retention policy, and records what happened.
func (b *backupRunner) now(ctx context.Context) (domain.BackupStatus, error) {
	dest, ok := b.dest()
	if !ok {
		return domain.BackupStatus{}, fmt.Errorf("%w: no backup directory is configured on this host (set WERKBORD_TEAM_BACKUP_DIR to a directory on storage you own)", domain.ErrConflict)
	}
	repl := b.st.store()
	if repl == nil {
		return domain.BackupStatus{}, fmt.Errorf("%w: this host's database is not up yet", domain.ErrConflict)
	}
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return domain.BackupStatus{}, fmt.Errorf("%w: a backup is already being taken", domain.ErrBusy)
	}
	b.running = true
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.running = false
		b.mu.Unlock()
	}()
	info, err := repl.Backup(ctx, dest)
	b.mu.Lock()
	b.lastAt = time.Now()
	if err != nil {
		b.lastOK, b.lastErr = false, err.Error()
		b.mu.Unlock()
		b.st.log.Error("a backup of the workspace failed", "err", err)
		return *b.status(ctx), err
	}
	b.lastOK, b.lastErr, b.verifiedAt = true, "", time.Now()
	b.mu.Unlock()
	policy := replicated.Retention{KeepLast: b.st.cfg.BackupKeep, KeepFor: b.st.cfg.BackupKeepFor}
	if removed, rerr := policy.Apply(ctx, dest, time.Now()); rerr != nil {
		b.st.log.Warn("old backups could not be removed", "err", rerr)
	} else if len(removed) > 0 {
		b.st.log.Info("old backups removed by the retention policy", "removed", len(removed))
	}
	b.st.log.Info("a backup of the workspace was taken and checked", "backup", info.Name, "position", info.Position, "from", dest.Describe())
	return *b.status(ctx), nil
}

// status describes the backups: where they go, how many there are, and how the last one went.
func (b *backupRunner) status(ctx context.Context) *domain.BackupStatus {
	dest, ok := b.dest()
	if !ok {
		return &domain.BackupStatus{Configured: false}
	}
	b.mu.Lock()
	st := domain.BackupStatus{Configured: true, Destination: dest.Describe(), LastOK: b.lastOK, LastError: b.lastErr}
	if !b.lastAt.IsZero() {
		st.LastAt = b.lastAt.UnixMilli()
	}
	if !b.verifiedAt.IsZero() {
		st.VerifiedAt = b.verifiedAt.UnixMilli()
	}
	b.mu.Unlock()
	if list, err := replicated.ListBackups(ctx, dest); err == nil {
		st.Count = len(list)
		if len(list) > 0 && !list[0].CreatedAt.IsZero() && list[0].CreatedAt.UnixMilli() > st.LastAt {
			st.LastAt, st.LastOK, st.LastError = list[0].CreatedAt.UnixMilli(), true, ""
		}
	}
	return &st
}

// run takes a backup whenever the newest one is older than the interval. Hosts that share a destination do not all
// back up at once: the first to find it due makes it fresh, and the others find it fresh.
func (b *backupRunner) run(ctx context.Context) {
	every := b.st.cfg.BackupEvery
	dest, ok := b.dest()
	if !ok || every <= 0 {
		return
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	first := time.After(30 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-first:
		case <-t.C:
		}
		list, err := replicated.ListBackups(ctx, dest)
		if err != nil {
			b.st.log.Warn("the backup directory cannot be read", "err", err)
			continue
		}
		if len(list) > 0 && !list[0].CreatedAt.IsZero() && time.Since(list[0].CreatedAt) < every {
			continue
		}
		bctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		_, err = b.now(bctx)
		cancel()
		if err != nil && !errors.Is(err, domain.ErrBusy) && !errors.Is(err, domain.ErrConflict) {
			b.st.log.Warn("the scheduled backup failed; it will be tried again", "err", err)
		}
	}
}
