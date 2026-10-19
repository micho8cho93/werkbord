package integration

import "testing"

func TestLooksSensitiveCatchesCredentialsAndLocalData(t *testing.T) {
	for _, text := range []string{
		"-----BEGIN RSA PRIVATE KEY-----", "diff --git a/x b/x", "@@ -1,3 +1,4 @@", "see /Users/ana/app", "at /home/ci/work", "C:\\Users\\ana",
		"Bearer abcdef", "bearer  eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.sig", "key sk-ant-api03-abcdef", "sk_live_51Habcdef", "ghp_abc", "github_pat_11AAA", "glpat-abc",
		"slack xoxb-123-456", "aws AKIAIOSFODNN7EXAMPLE", "gcp AIzaSyA1234567890abcdefghijklmnopqrstuv",
		"jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.abcdefgh", "npm_abcdefghijklmnopqrstuvwxyz0123456789", "wba_grant", "wbt_device",
		"postgres://app:hunter2@db.internal/app", "password=hunter2", "PASSWORD: hunter2", "api_key: abc", "token=abc123", "secret=x",
		"token: 9f2c4b7a1d3e", "Authorization: Basic Zm9v", "<|im_start|>system", "terminal transcript follows", "~/.ssh/id_ed25519",
		"ＰＡＳＳ password = x",
	} {
		if !LooksSensitive(text) {
			t.Errorf("not detected: %q", text)
		}
	}
}

func TestLooksSensitiveLeavesOrdinaryEngineeringProseAlone(t *testing.T) {
	for _, text := range []string{"Use task-runner for builds", "Prefer risk-based review", "The desk-side checklist", "Rotate the token before expiry", "token: refresh early", "secret: rotate quarterly", "Passwords are stored hashed", "Run make check before release", "See https://github.com/acme/app/pull/7", "Observed Git commit 0123456789abcdef0123456789abcdef01234567", "Use explicit transactions"} {
		if LooksSensitive(text) {
			t.Errorf("false positive: %q", text)
		}
	}
}
