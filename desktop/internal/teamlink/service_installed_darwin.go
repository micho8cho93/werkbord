//go:build darwin

package teamlink

import "os"

func serviceInstalled() bool {
	_, err := os.Stat("/Library/LaunchDaemons/dev.werkbord.team.plist")
	return err == nil
}
