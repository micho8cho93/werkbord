package integration

import "testing"

func TestRepositoryIdentityValidatesBeforeNormalizing(t *testing.T) {
	for _, s := range []string{"https://GitHub.com/Acme/Shop.git", "git@github.com:Acme/Shop", "ssh://git@github.com:22/Acme/Shop", "git://github.com/Acme/Shop/"} {
		got, err := RepositoryIdentity(s)
		if err != nil || got != "github.com/Acme/Shop" {
			t.Fatalf("%q: %q %v", s, got, err)
		}
	}
	for _, s := range []string{"https://token@github.com/a/b", "https://u:password@github.com/a/b", "ssh://token@github.com/a/b", "git@host:../repo", "https://host/a/../b", "https://host/a%2fb", "https://host/a?token=secret", "https://host/a#secret", "file:///tmp/repo", "/tmp/repo", "ext::sh", "git@host:a//b"} {
		if got, err := RepositoryIdentity(s); err == nil {
			t.Fatalf("unsafe %q accepted as %q", s, got)
		}
	}
	a, _ := RepositoryIdentity("ssh://git@host:2222/a/b")
	b, _ := RepositoryIdentity("git@host:a/b")
	if a == b {
		t.Fatal("nondefault port lost")
	}
	a, _ = RepositoryIdentity("https://host/A/B")
	b, _ = RepositoryIdentity("https://host/a/b")
	if a == b {
		t.Fatal("path case lost")
	}
}
