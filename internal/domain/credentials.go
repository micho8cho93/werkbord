package domain

import "strings"

// IsControllerSecret identifies controller configuration that must not be
// inherited by an agent or credential helper. The user's Git/agent credentials
// are intentionally retained for their own work.
func IsControllerSecret(kv string) bool {
	name, _, _ := strings.Cut(kv, "=")
	return strings.HasPrefix(name, "DEVBOARD_") || strings.HasPrefix(name, "WERKBORD_") || name == "TS_AUTHKEY" || strings.HasPrefix(name, "TS_") && strings.Contains(name, "AUTH")
}
