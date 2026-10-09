package domain

import (
	"devboard/internal/integration"
	"strings"
)

// RepositoryKey is the validated canonical identity shared with local task exchange.
// Nondefault ports remain significant and credentials/traversal are refused.
func RepositoryKey(addr string) string {
	key, err := integration.RepositoryIdentity(strings.TrimSpace(addr))
	if err != nil {
		return ""
	}
	return key
}
