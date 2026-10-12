//go:build windows

package teaminstall

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func systemDir() string {
	base, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, windows.KF_FLAG_DEFAULT)
	if err != nil || !filepath.IsAbs(base) {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, "Werkbord Team")
}

func serviceDefinition() string { return filepath.Join(systemDir(), "service-definition.json") }

func accessKeyPath(home string) string {
	return filepath.Join(home, "AppData", "Roaming", "werkbord-team-desktop", "access.key")
}

func serviceDefinitionBytes() ([]byte, error) { return captureServiceDefinition() }

func windowsServiceInstalled() bool {
	_, err := serviceDefinitionBytes()
	return !errors.Is(err, os.ErrNotExist) && err == nil
}
