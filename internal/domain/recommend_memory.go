package domain

import (
	"strings"
	"unicode"
)

// How the rules read saved statements.
//
// Saved policies and decisions are prose written by a person; Werkbord does not understand prose and does not try to.
// What it does is narrower and fails in one direction only: it looks for a small set of plain phrases, and what it finds
// can only RESTRICT a suggestion or EXPLAIN one. A statement the rules fail to recognise changes nothing. A statement
// that merely touches on how work reaches the main branch (merging, pushing, releasing, reviewing, protected branches)
// withholds the direct-merge shortcut, because the cost of being wrong is a merge a policy forbade, while the cost of
// being cautious is one extra pull request.
//
// Nothing here ever PERMITS anything. There is no phrase that makes a policy say "direct merges are fine": a restriction
// is lifted only by there being none.

// memFlags is what a saved statement was found to say.
type memFlags struct {
	// topic: it concerns how changes reach the main line (the broad, fail-closed test).
	topic bool
	// noDirect: it says, in as many words, not to commit or push straight to the main line.
	noDirect bool
	// prefersDirect: it says changes go straight to the main line, with no negation anywhere in it.
	prefersDirect bool
	requiresPR    bool
	requiresRev   bool
	requiresCheck bool
	protected     bool
	freeze        bool
	noRefactor    bool
	noAgents      bool
	// debt and bugs only ever raise a suggestion, and only among the allowed ones.
	debt bool
	bugs bool
}

// normalizeText lowercases, drops apostrophes ("don't" becomes "dont") and turns every other non-letter, non-digit into a
// space, so phrase tests do not depend on punctuation or case. It is padded with spaces so that " pull request " matches
// whole words only.
func normalizeText(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte(' ')
	space := true
	for _, r := range strings.ToLower(s) {
		switch {
		case r == '\'' || r == '’':
			continue
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			space = false
		default:
			if !space {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	if !space {
		b.WriteByte(' ')
	}
	return b.String()
}

func hasPhrase(norm string, phrases ...string) bool {
	for _, p := range phrases {
		if strings.Contains(norm, " "+p+" ") {
			return true
		}
	}
	return false
}

var negations = []string{"never", "not", "dont", "doesnt", "cannot", "cant", "avoid", "no", "without", "forbid", "forbidden", "prohibit", "prohibited", "disallow", "disallowed", "mustnt", "shouldnt", "wont"}

func negated(norm string) bool { return hasPhrase(norm, negations...) }

// classifyMemory reads one statement. It looks at a bounded prefix, so a very long statement costs nothing extra.
func classifyMemory(summary string) memFlags {
	if len(summary) > 2048 {
		summary = summary[:2048]
	}
	n := normalizeText(summary)
	var f memFlags
	f.topic = hasPhrase(n, "merge", "merges", "merging", "merged", "main", "master", "trunk", "branch", "branches", "push", "pushes", "pushing", "commit", "commits", "committing",
		"release", "releases", "releasing", "deploy", "deploys", "deploying", "pr", "prs", "pull request", "pull requests", "merge request", "review", "reviews", "reviewed", "reviewer",
		"reviewers", "approval", "approvals", "ci", "required checks", "status checks", "protected", "protection", "rebase", "squash", "codeowners")
	direct := hasPhrase(n, "directly to main", "directly to master", "directly to trunk", "directly on main", "directly on master", "direct commit", "direct commits", "direct push", "direct pushes",
		"push to main", "push to master", "commit to main", "commit to master", "commits to main", "commits to master", "straight to main", "straight to master", "work on main", "work directly on main")
	neg := negated(n)
	f.noDirect = direct && neg
	f.prefersDirect = direct && !neg
	f.requiresPR = hasPhrase(n, "pull request", "pull requests", "pr", "prs", "merge request", "merge requests")
	f.requiresRev = hasPhrase(n, "review", "reviews", "reviewed", "reviewer", "reviewers", "approval", "approvals", "approved", "codeowners")
	f.requiresCheck = hasPhrase(n, "ci", "checks", "status checks", "required checks", "tests pass", "passing tests", "green build", "lint")
	f.protected = hasPhrase(n, "protected branch", "protected branches", "branch protection", "protect main", "protected main")
	f.freeze = hasPhrase(n, "freeze", "frozen", "code freeze", "release freeze")
	f.noRefactor = hasPhrase(n, "refactor", "refactoring", "refactors") && neg
	f.noAgents = hasPhrase(n, "no ai", "no agents", "no agent", "no llm", "no automated agents", "do not run agents", "dont run agents", "agents are not allowed", "agents not allowed",
		"ai is not allowed", "ai tools are not allowed", "no ai tools", "no coding agents")
	f.debt = hasPhrase(n, "duplicate", "duplicated", "duplication", "tech debt", "technical debt", "refactor", "refactoring", "cleanup", "complexity", "tangled", "messy")
	f.bugs = hasPhrase(n, "bug", "bugs", "regression", "regressions", "flaky", "crash", "crashes", "broken")
	return f
}

// refactorWords marks a task as being about tidying code, for the one rule that looks at task titles.
func refactorTitle(title string) bool {
	return hasPhrase(normalizeText(title), "refactor", "refactoring", "cleanup", "clean up", "tidy", "simplify", "deduplicate", "dedupe", "restructure")
}
