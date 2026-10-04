package github

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"devboard/internal/domain"
)

// fakeGH writes an executable that stands in for gh. The script body decides
// what each invocation prints; it receives the arguments as $@, and every call is
// recorded in the returned log file.
func fakeGH(t *testing.T, body string) (*CLI, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\necho \"$@\" >> '" + log + "'\n" + body + "\n"
	path := filepath.Join(dir, "gh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &CLI{Binary: path}, log
}

// printing returns a script body that prints text exactly.
func printing(text string) string {
	return "cat <<'__END__'\n" + text + "\n__END__"
}

const listJSON = `[
 {"number":7,"title":"Add login","url":"https://github.com/acme/app/pull/7","state":"OPEN","isDraft":false,
  "headRefName":"devboard/login-aaaaaa","baseRefName":"main","headRefOid":"1111111111111111111111111111111111111111",
  "author":{"login":"michel"},"reviewDecision":"APPROVED","mergeable":"MERGEABLE",
  "statusCheckRollup":[
    {"__typename":"CheckRun","name":"build","status":"COMPLETED","conclusion":"SUCCESS"},
    {"__typename":"CheckRun","name":"lint","status":"COMPLETED","conclusion":"NEUTRAL"},
    {"__typename":"StatusContext","context":"ci/x","state":"SUCCESS"}],
  "createdAt":"2026-01-02T03:04:05Z","updatedAt":"2026-01-03T03:04:05Z","mergedAt":"0001-01-01T00:00:00Z","closedAt":"0001-01-01T00:00:00Z","isCrossRepository":false},
 {"number":6,"title":"Fix typo","url":"https://github.com/acme/app/pull/6","state":"MERGED","isDraft":false,
  "headRefName":"devboard/typo-bbbbbb","baseRefName":"main","headRefOid":"2222222222222222222222222222222222222222",
  "author":{"login":"michel"},"reviewDecision":"","mergeable":"UNKNOWN","statusCheckRollup":[],
  "createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T01:00:00Z","mergedAt":"2026-01-01T01:00:00Z","closedAt":"2026-01-01T01:00:00Z","isCrossRepository":false},
 {"number":5,"title":"Draft from a fork","url":"https://github.com/acme/app/pull/5","state":"OPEN","isDraft":true,
  "headRefName":"main","baseRefName":"main","headRefOid":"3333333333333333333333333333333333333333",
  "author":{"login":"stranger"},"reviewDecision":"REVIEW_REQUIRED","mergeable":"CONFLICTING",
  "statusCheckRollup":[{"__typename":"CheckRun","name":"build","status":"COMPLETED","conclusion":"FAILURE"},{"__typename":"CheckRun","name":"slow","status":"IN_PROGRESS","conclusion":""}],
  "createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T01:00:00Z","mergedAt":null,"closedAt":null,"isCrossRepository":true}
]`

func TestPullRequestsAreNormalised(t *testing.T) {
	gh, log := fakeGH(t, printing(listJSON))
	prs, err := gh.PullRequests(context.Background(), Repo{Host: "github.com", Owner: "acme", Name: "app"}, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 3 {
		t.Fatalf("got %d pull requests", len(prs))
	}
	open, merged, fork := prs[0], prs[1], prs[2]
	if open.Number != 7 || open.State != "open" || open.HeadBranch != "devboard/login-aaaaaa" || open.BaseBranch != "main" ||
		open.Review != "approved" || open.Mergeable != "mergeable" || open.Checks.State != "passing" || open.Checks.Passed != 3 || open.Author != "michel" {
		t.Errorf("open = %+v", open)
	}
	if open.MergedAt != nil || open.ClosedAt != nil || open.CreatedAt == nil {
		t.Errorf("zero times must be absent: %+v", open)
	}
	if merged.State != "merged" || merged.MergedAt == nil || merged.Mergeable != "" || merged.Checks.State != "none" || merged.Review != "" {
		t.Errorf("merged = %+v (an unknown mergeability must stay empty)", merged)
	}
	if !fork.Draft || !fork.CrossRepo || fork.Mergeable != "conflicting" || fork.Checks.State != "failing" || fork.Checks.Failed != 1 || fork.Checks.Pending != 1 || fork.Review != "review_required" {
		t.Errorf("fork = %+v", fork)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "pr list -R acme/app --state all") {
		t.Errorf("gh was called with %q", calls)
	}
}

func TestNotInstalledNotSignedInAndOtherFailures(t *testing.T) {
	ctx := context.Background()
	repo := Repo{Host: "github.com", Owner: "a", Name: "b"}

	_, err := (&CLI{Binary: filepath.Join(t.TempDir(), "no-such-gh")}).PullRequests(ctx, repo, 5)
	var ge *Error
	if !errors.As(err, &ge) || ge.Reason != domain.GHMissing {
		t.Errorf("missing gh: %v", err)
	}

	gh, _ := fakeGH(t, "echo 'To get started with GitHub CLI, please run:  gh auth login' >&2; exit 4")
	if _, err := gh.PullRequests(ctx, repo, 5); !errors.As(err, &ge) || ge.Reason != domain.GHUnauthenticated {
		t.Errorf("signed out: %v", err)
	}
	gh, _ = fakeGH(t, "echo 'error connecting to api.github.com' >&2; exit 1")
	if _, err := gh.PullRequests(ctx, repo, 5); !errors.As(err, &ge) || ge.Reason != domain.GHError || !strings.Contains(ge.Message, "could not be reached") {
		t.Errorf("offline: %v", err)
	}
	gh, _ = fakeGH(t, "echo 'GraphQL: Could not resolve to a Repository with the name a/b.' >&2; exit 1")
	if _, err := gh.PullRequests(ctx, repo, 5); !errors.As(err, &ge) || ge.Reason != domain.GHError || !strings.Contains(ge.Message, "Could not resolve") {
		t.Errorf("repo not found: %v", err)
	}
	gh, _ = fakeGH(t, "echo 'this is not json'")
	if _, err := gh.PullRequests(ctx, repo, 5); !errors.As(err, &ge) || ge.Reason != domain.GHError {
		t.Errorf("garbage: %v", err)
	}
	gh, _ = fakeGH(t, "echo 'fatal: https://user:tok3n@github.com/x denied' >&2; exit 1")
	_, err = gh.PullRequests(ctx, repo, 5)
	if err == nil || strings.Contains(err.Error(), "tok3n") {
		t.Errorf("credentials in gh's output must be removed: %v", err)
	}
	gh, _ = fakeGH(t, "sleep 5")
	gh.Timeout = 100 * time.Millisecond
	if _, err := gh.PullRequests(ctx, repo, 5); err == nil || !strings.Contains(err.Error(), "in time") {
		t.Errorf("timeout: %v", err)
	}
}

func TestCreatePullRequestReadsBackWhatGitHubHas(t *testing.T) {
	viewed := `{"number":9,"title":"T","url":"https://github.com/acme/app/pull/9","state":"OPEN","isDraft":true,"headRefName":"devboard/x-111111","baseRefName":"main","headRefOid":"","author":{"login":"me"},"reviewDecision":"","mergeable":"UNKNOWN","statusCheckRollup":[],"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z","mergedAt":null,"closedAt":null,"isCrossRepository":false}`
	jsonFile := filepath.Join(t.TempDir(), "view.json")
	if err := os.WriteFile(jsonFile, []byte(viewed), 0o644); err != nil {
		t.Fatal(err)
	}
	gh, log := fakeGH(t, `
case "$1 $2" in
  "pr create") echo "Creating pull request..."; echo "https://github.com/acme/app/pull/9";;
  "pr view") cat '`+jsonFile+`';;
esac`)
	pr, err := gh.CreatePullRequest(context.Background(), Repo{Host: "github.com", Owner: "acme", Name: "app"},
		CreateRequest{Head: "devboard/x-111111", Base: "main", Title: "T", Body: "B", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 9 || pr.URL != "https://github.com/acme/app/pull/9" || pr.State != "open" || !pr.Draft {
		t.Errorf("pr = %+v", pr)
	}
	calls, _ := os.ReadFile(log)
	for _, want := range []string{"pr create -R acme/app --head devboard/x-111111 --base main --title T --body B --draft", "pr view 9 -R acme/app"} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("calls missing %q:\n%s", want, calls)
		}
	}

	// gh that exits 0 but never says what it opened is not success.
	bad, _ := fakeGH(t, "echo done")
	if _, err := bad.CreatePullRequest(context.Background(), Repo{Owner: "a", Name: "b"}, CreateRequest{Head: "h", Base: "b", Title: "t"}); err == nil {
		t.Error("a pull request that cannot be read back must not be reported as created")
	}
	// A failure is a failure.
	fail, _ := fakeGH(t, `echo 'a pull request for branch "x" into branch "main" already exists' >&2; exit 1`)
	if _, err := fail.CreatePullRequest(context.Background(), Repo{Owner: "a", Name: "b"}, CreateRequest{Head: "h", Base: "b", Title: "t"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v", err)
	}
}

func TestEnvDoesNotLeakOrRedirect(t *testing.T) {
	env := ghEnv([]string{"PATH=/bin", "GH_REPO=evil/redirect", "GH_TOKEN=keep-users-own"})
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "GH_REPO") {
		t.Error("GH_REPO would redirect every call")
	}
	if !strings.Contains(joined, "GH_TOKEN=keep-users-own") || !strings.Contains(joined, "GH_PROMPT_DISABLED=1") {
		t.Errorf("env = %v", env)
	}
}

func TestHostKnown(t *testing.T) {
	ok, _ := fakeGH(t, "exit 0")
	bad, _ := fakeGH(t, "exit 1")
	if !ok.HostKnown(context.Background(), "ghe.corp.com") || bad.HostKnown(context.Background(), "ghe.corp.com") || ok.HostKnown(context.Background(), "") {
		t.Error("HostKnown follows gh auth status")
	}
}

func TestParseRemote(t *testing.T) {
	good := map[string]Repo{
		"https://github.com/acme/app.git":       {"github.com", "acme", "app"},
		"https://github.com/acme/app":           {"github.com", "acme", "app"},
		"https://github.com/acme/app/":          {"github.com", "acme", "app"},
		"git@github.com:acme/app.git":           {"github.com", "acme", "app"},
		"ssh://git@github.com/acme/app.git":     {"github.com", "acme", "app"},
		"ssh://git@ssh.github.com:443/acme/app": {"github.com", "acme", "app"},
		"git://github.com/acme/app.git":         {"github.com", "acme", "app"},
		"https://GitHub.com/Acme/App.git":       {"github.com", "Acme", "App"},
		"git@ghe.corp.com:team/svc.git":         {"ghe.corp.com", "team", "svc"},
		"https://ghe.corp.com/team/svc.git":     {"ghe.corp.com", "team", "svc"},
		"https://tok:x@github.com/acme/app.git": {"github.com", "acme", "app"},
	}
	for in, want := range good {
		got, ok := ParseRemote(in)
		if !ok || got != want {
			t.Errorf("ParseRemote(%q) = %+v, %v; want %+v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "/srv/git/app.git", "../relative", "file:///srv/git/app.git", "https://github.com/acme", "https://github.com/a/b/c", "git@github.com:app.git", "ext::sh -c x"} {
		if got, ok := ParseRemote(in); ok {
			t.Errorf("ParseRemote(%q) = %+v, want no match", in, got)
		}
	}
	if (Repo{Host: "github.com", Owner: "a", Name: "b"}).Selector() != "a/b" || (Repo{Host: "ghe.x", Owner: "a", Name: "b"}).Selector() != "ghe.x/a/b" {
		t.Error("selector")
	}
}
