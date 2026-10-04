package domain

import (
	"strings"
	"testing"
	"time"
)

func TestClassifyRelation(t *testing.T) {
	cases := []struct {
		ahead, behind int
		want          TargetRelation
		merged        bool
	}{
		{0, 0, RelSame, true},
		{0, 5, RelMerged, true},
		{3, 0, RelAhead, false},
		{3, 2, RelDiverged, false},
	}
	for _, c := range cases {
		got := ClassifyRelation(c.ahead, c.behind)
		if got != c.want {
			t.Errorf("ClassifyRelation(%d, %d) = %s, want %s", c.ahead, c.behind, got, c.want)
		}
		if m := (GitVsTarget{Ahead: c.ahead, Behind: c.behind, Relation: got}).FullyMerged(); m != c.merged {
			t.Errorf("FullyMerged(%d, %d) = %v, want %v", c.ahead, c.behind, m, c.merged)
		}
	}
	// A branch that never had a commit of its own is deletable by ancestry (nothing on it is lost)...
	if !(GitVsTarget{Relation: RelBehind}).FullyMerged() {
		t.Error("a branch with no commits of its own has nothing to lose")
	}
	// Nothing about an unknown or a target relation counts as merged: deletion must never be offered on a guess.
	for _, r := range []TargetRelation{RelUnknown, RelTarget} {
		if (GitVsTarget{Relation: r}).FullyMerged() {
			t.Errorf("%s must not count as fully merged", r)
		}
	}
}

func TestClassifyUpstream(t *testing.T) {
	for _, c := range []struct {
		ahead, behind int
		want          UpstreamState
	}{{0, 0, UpstreamInSync}, {2, 0, UpstreamAhead}, {0, 2, UpstreamBehind}, {1, 1, UpstreamDiverged}} {
		if got := ClassifyUpstream(c.ahead, c.behind); got != c.want {
			t.Errorf("ClassifyUpstream(%d, %d) = %s, want %s", c.ahead, c.behind, got, c.want)
		}
	}
}

func TestProtectedBranch(t *testing.T) {
	for _, name := range []string{"main", "master", "trunk", "develop", "release/1.2", "hotfix/x", "production", "staging", "", "my-target", "checked-out"} {
		if !ProtectedBranch(name, "my-target", "checked-out") {
			t.Errorf("%q must be protected", name)
		}
	}
	for _, name := range []string{"devboard/fix-abc123", "feature/x", "mainline", "domain"} {
		if ProtectedBranch(name, "main", "main") {
			t.Errorf("%q must not be protected", name)
		}
	}
}

func TestStaleReason(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	old := now.Add(-30 * 24 * time.Hour)
	recent := now.Add(-2 * 24 * time.Hour)
	for _, c := range []struct {
		name   string
		rel    TargetRelation
		behind int
		tip    time.Time
		want   bool
	}{
		{"old, ahead, target moved on", RelAhead, 4, old, true},
		{"old, diverged", RelDiverged, 1, old, true},
		{"old but the target has not moved", RelAhead, 0, old, false},
		{"recent", RelAhead, 9, recent, false},
		{"merged is finished, not stale", RelMerged, 9, old, false},
		{"the target itself", RelTarget, 9, old, false},
		{"no commit date", RelAhead, 9, time.Time{}, false},
	} {
		got, why := StaleReason(c.rel, c.behind, c.tip, now)
		if got != c.want || (got && !strings.Contains(why, "30 days")) || (!got && why != "") {
			t.Errorf("%s: stale = %v (%q), want %v", c.name, got, why, c.want)
		}
	}
}

func owned() GitBranchOwnership {
	return GitBranchOwnership{Created: true, Namespace: true, Phase: PhaseReview}
}

func kinds(as []GitAttention) []string {
	var out []string
	for _, a := range as {
		out = append(out, a.Kind+"/"+a.Severity)
	}
	return out
}

func TestBranchAttention(t *testing.T) {
	ahead := GitVsTarget{Ahead: 2, Relation: RelAhead}
	cases := []struct {
		name string
		b    GitBranch
		want []string
	}{
		{"finished work to review, never pushed", GitBranch{Scope: ScopeLocal, VsTarget: ahead, DevBoard: owned(), Upstream: GitUpstream{State: UpstreamNone}, NotPushed: 2},
			[]string{"review/action", "no_remote/warn"}},
		{"the same, with the agent still working", GitBranch{Scope: ScopeLocal, VsTarget: ahead, DevBoard: GitBranchOwnership{Created: true, Namespace: true, Phase: PhaseActive, ActiveRun: true}, Upstream: GitUpstream{State: UpstreamNone}, NotPushed: 2},
			[]string{"no_remote/warn"}},
		{"owned and diverged is still to be reviewed", GitBranch{Scope: ScopeLocal, VsTarget: GitVsTarget{Ahead: 1, Behind: 3, Relation: RelDiverged}, DevBoard: owned(), Upstream: GitUpstream{State: UpstreamInSync}},
			[]string{"review/action"}},
		{"someone else's diverged branch is only flagged", GitBranch{Scope: ScopeLocal, VsTarget: GitVsTarget{Ahead: 1, Behind: 3, Relation: RelDiverged}, Upstream: GitUpstream{State: UpstreamInSync}},
			[]string{"diverged/warn"}},
		{"merged and nothing left: delete", GitBranch{Scope: ScopeLocal, Merged: true, VsTarget: GitVsTarget{Behind: 1, Relation: RelMerged}, DevBoard: owned(), Upstream: GitUpstream{State: UpstreamNone}},
			[]string{"cleanup/action"}},
		{"merged but its worktree is still there: clean it first", GitBranch{Scope: ScopeLocal, Merged: true, VsTarget: GitVsTarget{Behind: 1, Relation: RelMerged}, DevBoard: owned(), Worktree: &GitBranchWorktree{Owned: true}, Upstream: GitUpstream{State: UpstreamNone}},
			[]string{"cleanup/info"}},
		{"merged but still running: leave it", GitBranch{Scope: ScopeLocal, Merged: true, VsTarget: GitVsTarget{Behind: 1, Relation: RelMerged}, DevBoard: GitBranchOwnership{Created: true, ActiveRun: true}, Upstream: GitUpstream{State: UpstreamNone}},
			nil},
		{"a branch of the user's that is merged is not Dev Board's to clean", GitBranch{Scope: ScopeLocal, Merged: true, VsTarget: GitVsTarget{Behind: 1, Relation: RelMerged}, Upstream: GitUpstream{State: UpstreamNone}},
			nil},
		{"a branch the target moved past, with no commits of its own, is not called merged", GitBranch{Scope: ScopeLocal, Merged: true, VsTarget: GitVsTarget{Behind: 2, Relation: RelBehind}, DevBoard: owned(), Upstream: GitUpstream{State: UpstreamNone}},
			[]string{"cleanup/info"}},
		{"an empty branch is just empty", GitBranch{Scope: ScopeLocal, Merged: true, VsTarget: GitVsTarget{Relation: RelSame}, DevBoard: owned(), Upstream: GitUpstream{State: UpstreamNone}},
			[]string{"cleanup/info"}},
		{"upstream deleted", GitBranch{Scope: ScopeLocal, VsTarget: GitVsTarget{Relation: RelUnknown}, Upstream: GitUpstream{State: UpstreamGone}},
			[]string{"gone/warn"}},
		{"behind its remote", GitBranch{Scope: ScopeLocal, VsTarget: GitVsTarget{Relation: RelUnknown}, Upstream: GitUpstream{State: UpstreamBehind, Behind: 2}},
			[]string{"behind/info"}},
		{"uncommitted work and an unfinished merge in its worktree", GitBranch{Scope: ScopeLocal, VsTarget: GitVsTarget{Relation: RelSame}, Upstream: GitUpstream{State: UpstreamInSync},
			Worktree: &GitBranchWorktree{Dirty: &GitChangeCounts{Unstaged: 2}, Operation: "merge"}},
			[]string{"operation/warn", "dirty/warn"}},
		{"its worktree directory is gone", GitBranch{Scope: ScopeLocal, Upstream: GitUpstream{State: UpstreamInSync}, Worktree: &GitBranchWorktree{Missing: true}},
			[]string{"missing/warn"}},
		{"stale", GitBranch{Scope: ScopeLocal, Stale: true, StaleWhy: "no commits for 30 days", VsTarget: GitVsTarget{Relation: RelUnknown}, Upstream: GitUpstream{State: UpstreamInSync}},
			[]string{"stale/warn"}},
		{"the target is never flagged", GitBranch{Scope: ScopeLocal, Target: true, Upstream: GitUpstream{State: UpstreamAhead, Ahead: 3}}, nil},
		{"a remote branch is never flagged", GitBranch{Scope: ScopeRemote, VsTarget: ahead}, nil},
	}
	for _, c := range cases {
		b := c.b
		got := kinds(BranchAttention(&b))
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: attention = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAttentionRankPutsDevBoardFirstThenTheMostPressing(t *testing.T) {
	mk := func(created, namespace bool, sev string) *GitBranch {
		b := &GitBranch{DevBoard: GitBranchOwnership{Created: created, Namespace: namespace}}
		if sev != "" {
			b.Attention = []GitAttention{{Severity: sev}}
		}
		return b
	}
	ordered := []*GitBranch{
		mk(true, true, SeverityAction), mk(true, true, SeverityWarn), mk(true, true, ""),
		mk(false, true, SeverityAction), mk(false, false, SeverityAction), mk(false, false, SeverityWarn), mk(false, false, ""),
	}
	for i := 1; i < len(ordered); i++ {
		if AttentionRank(ordered[i-1]) <= AttentionRank(ordered[i]) {
			t.Errorf("rank[%d]=%d should be above rank[%d]=%d", i-1, AttentionRank(ordered[i-1]), i, AttentionRank(ordered[i]))
		}
	}
}

func TestChangeCounts(t *testing.T) {
	c := GitChangeCounts{Staged: 1, Untracked: 2}
	if c.Total() != 3 || !c.Dirty() || !c.TrackedDirty() {
		t.Errorf("%+v", c)
	}
	u := GitChangeCounts{Untracked: 5}
	if !u.Dirty() || u.TrackedDirty() {
		t.Errorf("untracked files are dirty but not tracked-dirty: %+v", u)
	}
	if (GitChangeCounts{}).Dirty() {
		t.Error("nothing is not dirty")
	}
}

func TestValidateMergeStrategy(t *testing.T) {
	for in, want := range map[string]string{"": MergeCommit, "merge": MergeCommit, "ff-only": MergeFastForward} {
		if got, err := ValidateMergeStrategy(in); err != nil || got != want {
			t.Errorf("%q → %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"squash", "rebase", "--force", "MERGE"} {
		if _, err := ValidateMergeStrategy(in); err == nil {
			t.Errorf("%q must be refused", in)
		}
	}
}
