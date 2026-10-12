//go:build windows

package teamlink

import "golang.org/x/sys/windows/svc/mgr"

func serviceInstalled() bool {
	m, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer m.Disconnect()
	s, err := m.OpenService("dev.werkbord.team")
	if err != nil {
		return false
	}
	_ = s.Close()
	return true
}
