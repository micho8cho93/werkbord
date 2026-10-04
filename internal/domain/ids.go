package domain

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
)

// ID prefixes make identifiers self-describing in logs and URLs.
const (
	PrefixProject  = "prj"
	PrefixTask     = "tsk"
	PrefixRun      = "run"
	PrefixQuestion = "qst"
	PrefixWorktree = "wt"
)

var idEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewID returns a random identifier such as "tsk_k3j9x2m4q7p1a8z5".
func NewID(prefix string) string {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("domain: crypto/rand failed: " + err.Error())
	}
	return prefix + "_" + strings.ToLower(idEncoding.EncodeToString(b[:]))
}
