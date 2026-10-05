package domain

import "testing"

func TestRepositoryWebURLKnowsOnlyHostsItCanBuildLinksFor(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/Acme/Shop.git":    "https://github.com/Acme/Shop",
		"git@github.com:acme/shop.git":        "https://github.com/acme/shop",
		"ssh://git@github.com:22/acme/shop":   "https://github.com/acme/shop",
		"https://gitlab.com/acme/group/docs":  "https://gitlab.com/acme/group/docs",
		"https://git.example.com/acme/shop":   "", // a host whose page layout is unknown: no guessed link
		"https://github.com.evil.example/a/b": "",
		"https://evil.example/github.com/a/b": "",
		"javascript:alert(1)":                 "",
		"":                                    "",
		"https://github.com/":                 "",
		"https://github.com/acme/<script>/x":  "https://github.com/acme/%3Cscript%3E/x", // escaped, never raw
	} {
		if got := RepositoryWebURL(in); got != want {
			t.Errorf("RepositoryWebURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTicketLinksPointAtTheBranchTheCompareViewAndTheCommits(t *testing.T) {
	k := Ticket{Branch: "wb-1-fix/it", PullRequest: &PullRequest{URL: "https://github.com/acme/shop/pull/7", BaseBranch: "main"}}
	l := TicketLinks("https://github.com/acme/shop", k)
	if l.Repository != "https://github.com/acme/shop" || l.Branch != "https://github.com/acme/shop/tree/wb-1-fix/it" ||
		l.Compare != "https://github.com/acme/shop/compare/main...wb-1-fix/it" || l.PullRequest != k.PullRequest.URL ||
		l.CommitPrefix != "https://github.com/acme/shop/commit/" {
		t.Fatalf("%+v", l)
	}
	gl := TicketLinks("git@gitlab.com:acme/docs.git", k)
	if gl.Branch != "https://gitlab.com/acme/docs/-/tree/wb-1-fix/it" || gl.CommitPrefix != "https://gitlab.com/acme/docs/-/commit/" {
		t.Fatalf("%+v", gl)
	}
	// An unknown host still yields the pull request, and nothing it would have to guess.
	other := TicketLinks("https://git.example.com/acme/shop", k)
	if other.PullRequest == "" || other.Branch != "" || other.Compare != "" || other.Repository != "" {
		t.Fatalf("%+v", other)
	}
	// A pull-request address that is not https is never offered as a link.
	bad := Ticket{PullRequest: &PullRequest{URL: "javascript:alert(1)"}}
	if got := TicketLinks("", bad); got.PullRequest != "" {
		t.Fatalf("%+v", got)
	}
	// No branch, no branch links; no base, no compare.
	if got := TicketLinks("https://github.com/acme/shop", Ticket{}); got.Branch != "" || got.Compare != "" || got.Repository == "" {
		t.Fatalf("%+v", got)
	}
}
