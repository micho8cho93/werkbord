package shell

import (
	"context"
	"errors"
	"time"

	"devboard/desktop/internal/migration"
	"devboard/internal/launcher"
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

// UpdatePersonal brings the person's own Werkbord up to the version this app carries, when the one running is too old for
// the window to show. It is the existing path, in its order: adopt the existing installation first (backed up, after the
// person agrees in a dialog), then let the launcher replace the program, which refuses while agents are working and puts
// the old one back if the new one does not come up. Frames cannot call it.
func (s *Shell) UpdatePersonal() error {
	if s.o.Migration != nil {
		st, err := s.o.Migration.Status()
		if err != nil {
			return err
		}
		if st.Phase != "verified" && st.Phase != "not_needed" {
			if st, err = s.Migrate("adopt"); err != nil {
				return err
			}
			if st.Phase != "verified" {
				return errors.New("your existing installation was not adopted, so Werkbord was not updated")
			}
		}
	}
	if s.o.Launcher == nil {
		return errors.New("this app cannot update Werkbord")
	}
	conn, err := s.o.Launcher.Connect(s.o.Ctx, func(step launcher.Step) { s.o.UI.Emit("progress", step) })
	if err != nil {
		return err
	}
	if conn.Notice != "" {
		return errors.New(conn.Notice)
	}
	return nil
}
