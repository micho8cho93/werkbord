package integration

import (
	"regexp"
	"strings"
	"unicode"
)

// LooksSensitive reports whether memory text appears to carry a credential, a
// local path, a diff or transcript framing. It is a screen, not a guarantee: the
// export policy is the closed allowlist and a person's review. It is shared so
// that Individual extraction, manual saves and Team's wire validation agree.
//
// Prefixes are matched at the start of a word so ordinary prose such as
// "task-runner" or "risk-based" is not mistaken for an "sk-" key.
func LooksSensitive(text string) bool {
	lower := strings.ToLower(text)
	for _, s := range sensitiveSubstrings {
		if strings.Contains(lower, s) {
			return true
		}
	}
	if sensitivePattern.MatchString(text) {
		return true
	}
	for _, m := range sensitiveAssignment.FindAllStringSubmatch(text, -1) {
		name, sep, value := strings.ToLower(m[1]), m[2], m[3]
		if sep == "=" || strongSecretName[name] {
			return true
		}
		// "token: refresh early" is prose; "token: 9f2c…" is a value.
		if len(value) >= 12 && strings.IndexFunc(value, unicode.IsDigit) >= 0 {
			return true
		}
	}
	return false
}

var sensitiveSubstrings = []string{
	"-----begin", "diff --git", "@@ ", "/users/", "/home/", "/root/", "~/.", "c:\\",
	"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_", "glpat-", "wba_", "wbt_", "wbi_", "wbm_", "wbp_",
	"api_key=", "api_key:", "password=", "authorization:", "authorization=",
	"<|im_start|>", "terminal transcript", "raw conversation",
}

var sensitivePattern = regexp.MustCompile(`(?i)` + strings.Join([]string{
	`\bbearer\s+\S`,
	`\bsk-[a-z0-9]`,
	`\bsk_(?:live|test)_`,
	`\bxox[abposr]-`,
	`\b(?:akia|asia)[0-9a-z]{16}\b`,
	`\baiza[0-9a-z_-]{30,}`,
	`\beyj[a-z0-9_-]{8,}\.[a-z0-9_-]{8,}\.`,
	`\bnpm_[a-z0-9]{20,}`,
	`://[^/\s@:]+:[^/\s@]+@`,
}, "|"))

var sensitiveAssignment = regexp.MustCompile(`(?i)\b(api[_-]?key|apikey|secret|token|passw(?:or)?d|pwd|private[_-]?key|access[_-]?key|client[_-]?secret|secret[_-]?key)\b\s*([:=])\s*["']?([^\s"']+)`)

var strongSecretName = map[string]bool{
	"api_key": true, "api-key": true, "apikey": true, "password": true, "passwd": true, "pwd": true,
	"private_key": true, "private-key": true, "client_secret": true, "client-secret": true,
	"secret_key": true, "secret-key": true, "access_key": true, "access-key": true,
}
