package main

import (
	"context"
	"devboard/desktop/internal/migration"
	"devboard/internal/launcher"
	"os"
	"path/filepath"
)

func newMigrationManager(ctx context.Context) *migration.Manager {
	home, _ := os.UserHomeDir()
	configDir, _ := os.UserConfigDir()
	// The launcher remains authoritative for custom data paths and legacy launchd definitions.
	cfg, _ := launcher.New(launcherOptions()).Config(ctx)
	roots := []migration.Installation{
		{Kind: "personal_resolved", Path: cfg.DataDir},
		{Kind: "personal_data", Path: filepath.Join(configDir, "werkbord")},
		{Kind: "devboard_data", Path: filepath.Join(configDir, "devboard")},
		{Kind: "team_user_data", Path: filepath.Join(configDir, "werkbord-team")},
		{Kind: "team_user_data", Path: filepath.Join(configDir, "werkbord-team-desktop")},
		{Kind: "personal_app", Path: "/Applications/Werkbord.app"},
		{Kind: "team_app", Path: "/Applications/Werkbord Team.app"},
		{Kind: "team_service", Path: "/Library/LaunchDaemons/dev.werkbord.team.plist"},
		{Kind: "personal_cli", Path: filepath.Join(home, ".local", "bin", "werkbord")},
	}
	// The root-owned Team state is retained in place and accessed only by Team's service.
	// It is never read/copied by the user-scoped shell.
	filtered := roots[:0]
	for _, r := range roots {
		if updaterTestBuild && (r.Kind == "personal_app" || r.Kind == "team_app" || r.Kind == "team_service") {
			continue
		}
		if r.Kind == "personal_app" && r.Path == appBundle() {
			continue
		}
		filtered = append(filtered, r)
	}
	return &migration.Manager{Dir: filepath.Join(configDir, "werkbord-desktop", "migration"), Roots: filtered, Personal: cfg.DataDir}
}

func bundledComponents() string {
	if app := appBundle(); app != "" {
		b, err := os.ReadFile(filepath.Join(app, "Contents", "Resources", "components.txt"))
		if err == nil && len(b) < 4096 {
			return string(b)
		}
	}
	return "Shell: " + version + "\nPersonal: " + version + "\nTeam: separately installed (development)"
}
