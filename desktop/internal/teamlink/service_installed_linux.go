//go:build linux

package teamlink

import "os"

func serviceInstalled() bool {
	_, err := os.Stat("/etc/systemd/system/dev.werkbord.team.service")
	return err == nil
}
