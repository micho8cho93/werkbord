package domain

import (
	"strings"
	"testing"
	"time"
)

func TestRepositoryKeyIsTheSameForEveryWayOfWritingAnAddress(t *testing.T) {
	same := []string{
		"https://github.com/Acme/Shop.git", "https://github.com/Acme/Shop", "https://GitHub.com/Acme/Shop/", "git@github.com:Acme/Shop.git",
		"git@github.com:Acme/Shop", "ssh://git@github.com/Acme/Shop.git", "ssh://git@github.com:22/Acme/Shop", "git://github.com/Acme/Shop.git",
	}
	for _, s := range same {
		if got := RepositoryKey(s); got != "github.com/Acme/Shop" {
			t.Errorf("RepositoryKey(%q) = %q", s, got)
		}
	}
	if RepositoryKey("https://github.com/Acme/Shop") == RepositoryKey("https://github.com/Acme/Other") {
		t.Error("different repositories share a key")
	}
	for _, s := range []string{"", "  ", "nonsense", "https://github.com", "https://github.com/", "git@github.com:", "/local/path"} {
		if got := RepositoryKey(s); got != "" {
			t.Errorf("RepositoryKey(%q) = %q, want none", s, got)
		}
	}
}

func TestCleanBranch(t *testing.T) {
	for _, ok := range []string{"main", "wb-142-authentication-error", "feature/x", "release/1.2", "a.b", "UPPER", "ünï"} {
		if _, err := CleanBranch(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", " ", "a b", "a..b", "-x", "x.lock", "a/", "/a", "a//b", "a@{b", "@", "a~b", "a^b", "a:b", "a?b", "a*b", "a[b", "a\\b", "a\tb", "a/.hidden", ".x", "end.", strings.Repeat("x", 201)} {
		if _, err := CleanBranch(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestCleanPullRequest(t *testing.T) {
	got, err := CleanPullRequest(PullRequest{URL: "https://github.com/a/b/pull/1", Behind: -1})
	if err != nil || got.State != PROpen || got.Mergeable != MergeUnknown {
		t.Fatalf("%+v %v", got, err)
	}
	for name, p := range map[string]PullRequest{
		"http":        {URL: "http://github.com/a/b/pull/1"},
		"file":        {URL: "file:///etc/passwd"},
		"javascript":  {URL: "javascript:alert(1)"},
		"credentials": {URL: "https://u:p@github.com/a/b/pull/1"},
		"user only":   {URL: "https://token@github.com/a/b/pull/1"},
		"query":       {URL: "https://github.com/a/b/pull/1?x=1"},
		"fragment":    {URL: "https://github.com/a/b/pull/1#c"},
		"empty":       {},
		"space":       {URL: "https://github.com/a/b /pull/1"},
		"bad base":    {URL: "https://github.com/a/b/pull/1", BaseBranch: "a b"},
		"neg number":  {URL: "https://github.com/a/b/pull/1", Number: -1},
		"big behind":  {URL: "https://github.com/a/b/pull/1", Behind: 2_000_000},
		"neg ahead":   {URL: "https://github.com/a/b/pull/1", Ahead: -1},
	} {
		if _, err := CleanPullRequest(p); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestCleanCommitAndFiles(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 6_000_000, time.UTC)
	c, err := CleanCommit(Commit{SHA: " ABCDEF1234 ", Subject: "  First line\nsecond", CommittedAt: at})
	if err != nil || c.SHA != "abcdef1234" || c.Subject != "First line" {
		t.Fatalf("%+v %v", c, err)
	}
	long, _ := CleanCommit(Commit{SHA: "abcdef1", Subject: strings.Repeat("é", 500)})
	if len([]rune(long.Subject)) != MaxCommitSubject {
		t.Fatalf("subject has %d runes", len([]rune(long.Subject)))
	}
	for _, bad := range []string{"", "xyz", "abc", "ghijklm", strings.Repeat("a", 65)} {
		if _, err := CleanCommit(Commit{SHA: bad}); err == nil {
			t.Errorf("sha %q accepted", bad)
		}
	}
	files, err := CleanFiles([]string{"a/b.go", "a/b.go", " c.go "})
	if err != nil || len(files) != 2 {
		t.Fatalf("%v %v", files, err)
	}
	for _, bad := range []string{"", "/abs", "../up", "a/../b", "a\\b", "a\x00b"} {
		if _, err := CleanFiles([]string{bad}); err == nil {
			t.Errorf("file %q accepted", bad)
		}
	}
	if _, err := CleanFiles(make([]string, MaxBranchFiles+1)); err == nil {
		t.Error("too many files accepted")
	}
}

func TestInviteCodes(t *testing.T) {
	a, ha := NewInviteCode()
	b, hb := NewInviteCode()
	if a == b || ha == hb || !strings.HasPrefix(a, InviteCodePrefix) || len(a) != len(InviteCodePrefix)+32 {
		t.Fatalf("%q %q", a, b)
	}
	if HashInviteCode(a) != ha || HashInviteCode(" "+a+" ") != ha {
		t.Error("hashing is not stable")
	}
	// An invite code's hash is not a sign-in token's hash, so one can never stand in for the other.
	if HashInviteCode(a) == HashToken(a) {
		t.Error("invite and token hashes are interchangeable")
	}
	now := time.Now()
	inv := Invite{ExpiresAt: now.Add(time.Hour), MaxUses: 2}
	if !inv.Active(now) {
		t.Error("a fresh invite is not active")
	}
	inv.Uses = 2
	if inv.Active(now) {
		t.Error("a used-up invite is active")
	}
	inv.Uses = 0
	if inv.Active(now.Add(2 * time.Hour)) {
		t.Error("an expired invite is active")
	}
	revoked := now
	inv.RevokedAt = &revoked
	if inv.Active(now) {
		t.Error("a revoked invite is active")
	}
}
