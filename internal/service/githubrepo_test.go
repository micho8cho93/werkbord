package service

import "testing"

func TestParseRepoNameTakesOwnerNameOrHostOwnerNameAndNothingElse(t *testing.T) {
	for _, ok := range []string{"octo/live", "my.org/repo", "git.example.com/octo/live"} {
		if _, good := parseRepoName(ok); !good {
			t.Errorf("%q should be a repository name", ok)
		}
	}
	for _, bad := range []string{"", "octo", "--bad", "a/b/c/d", "octo/..", "host_x.com/octo/live", "-h.com/octo/live", "git.example.com/octo/--x", "octo/live;rm"} {
		if _, good := parseRepoName(bad); good {
			t.Errorf("%q should not be a repository name", bad)
		}
	}
	r, _ := parseRepoName("git.example.com/octo/live")
	if r.Host != "git.example.com" || r.Owner != "octo" || r.Name != "live" {
		t.Errorf("host form = %+v", r)
	}
}
