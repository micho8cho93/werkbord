//go:build linux

package teaminstall

import (
	"os"
	"path/filepath"
)

func systemDir() string         { return "/var/lib/werkbord-team" }
func serviceDefinition() string { return "/etc/systemd/system/dev.werkbord.team.service" }
func accessKeyPath(home string) string {
	return filepath.Join(home, ".config", "werkbord-team-desktop", "access.key")
}
func serviceDefinitionBytes() ([]byte, error) { return os.ReadFile(ServicePlist) }
