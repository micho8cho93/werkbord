//go:build darwin

package teaminstall

import (
	"os"
	"path/filepath"
)

func systemDir() string         { return "/Library/Application Support/Werkbord Team" }
func serviceDefinition() string { return "/Library/LaunchDaemons/dev.werkbord.team.plist" }
func accessKeyPath(home string) string {
	return filepath.Join(home, "Library", "Application Support", "werkbord-team-desktop", "access.key")
}
func serviceDefinitionBytes() ([]byte, error) { return os.ReadFile(ServicePlist) }
