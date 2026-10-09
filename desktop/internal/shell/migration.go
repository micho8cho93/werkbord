package shell

import (
	"context"
	"devboard/desktop/internal/migration"
	"errors"
	"time"
)

// MigrationStatus exposes only installation locations and adoption status, never
// credentials or database contents. Workspace frames cannot call it through Relay.
func (s *Shell) MigrationStatus() (migration.Status, error) {
	if s.o.Migration == nil {
		return migration.Status{Phase: "not_needed", Installations: []migration.Installation{}}, nil
	}
	return s.o.Migration.Status()
}

// Migrate accepts a fixed operation, never paths. Adoption and rollback require a
// native confirmation; neither restarts services nor restores old data over new jobs.
func (s *Shell) Migrate(action string) (migration.Status, error) {
	if s.o.Migration == nil {
		return migration.Status{}, errors.New("migration is unavailable")
	}
	ctx, cancel := context.WithTimeout(s.o.Ctx, 2*time.Minute)
	defer cancel()
	switch action {
	case "adopt":
		if s.o.UI.Ask(Dialog{Kind: Question, Title: "Use your existing installation?", Message: "Werkbord will back up your local managed state and connect to the existing services. Your Personal and Team databases remain separate, in their current locations. Agents, credentials, worktrees, licenses and schedules stay where they are. Old applications and backups are kept.", Buttons: []string{"Back up and adopt", "Cancel"}, Default: "Cancel", Cancel: "Cancel"}) != "Back up and adopt" {
			return s.o.Migration.Status()
		}
		if _, err := s.o.Migration.Prepare(ctx); err != nil {
			return migration.Status{}, err
		}
		return s.o.Migration.Verify(ctx, s.o.VerifyMigration)
	case "rollback":
		if s.o.UI.Ask(Dialog{Kind: Question, Title: "Roll back desktop adoption?", Message: "Your original applications, services and data remain available. This rolls back the adoption marker and keeps the backup. It preserves work created since adoption; it does not restore an older database over it.", Buttons: []string{"Roll back adoption", "Cancel"}, Default: "Cancel", Cancel: "Cancel"}) != "Roll back adoption" {
			return s.o.Migration.Status()
		}
		return s.o.Migration.Rollback()
	default:
		return migration.Status{}, errors.New("choose adopt or rollback")
	}
}
