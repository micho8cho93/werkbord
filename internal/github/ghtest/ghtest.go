// Package ghtest is a stand-in for the GitHub CLI, for tests of what is built
// on it. It is a shell script whose answers are files in a state directory, so a
// test sets the scene by writing files and sees what was asked by reading them.
package ghtest

import (
	"os"
	"path/filepath"
	"testing"
)

// Fake is a fake gh.
type Fake struct {
	// Binary is the path of the script: use it as the GitHub CLI's command.
	Binary string
	// Dir is the state directory.
	Dir string
}

const script = `#!/bin/sh
D="$FAKE_GH_DIR"
echo "$@" >> "$D/calls"
case "$1" in
--version) echo "gh version 2.86.0 (2026-01-21)"; exit 0 ;;
api)
  [ -f "$D/offline" ] && { echo "error connecting to api.github.com" >&2; exit 1; }
  if [ ! -f "$D/signedin" ]; then
    echo "gh: To get started with GitHub CLI, please run:  gh auth login" >&2; exit 4
  fi
  case "$*" in
  *user/repos*)
    page=1
    for a in "$@"; do case "$a" in page=*) page="${a#page=}" ;; esac; done
    if [ -f "$D/repos.$page.json" ]; then cat "$D/repos.$page.json"; else echo "[]"; fi
    exit 0 ;;
  *) cat "$D/user.json"; exit 0 ;;
  esac ;;
auth)
  case "$2" in
  login)
    echo ""
    echo "! First copy your one-time code: ABCD-1234"
    echo "Open this URL to continue in your web browser: https://github.com/login/device"
    i=0
    while [ ! -f "$D/approve" ] && [ ! -f "$D/deny" ] && [ $i -lt 100 ]; do sleep 0.1; i=$((i+1)); done
    if [ -f "$D/approve" ]; then touch "$D/signedin"; exit 0; fi
    echo "authentication failed" >&2; exit 1 ;;
  git-credential) exit 0 ;;
  esac ;;
esac
echo "unexpected: $*" >&2
exit 64
`

// New writes a fake gh into a temporary directory. It starts signed out.
func New(t *testing.T) *Fake {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "gh")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_GH_DIR", dir)
	f := &Fake{Binary: bin, Dir: dir}
	f.write("user.json", `{"login":"octo","name":"Octo Cat"}`)
	return f
}

func (f *Fake) write(name, body string) {
	if err := os.WriteFile(filepath.Join(f.Dir, name), []byte(body), 0o644); err != nil {
		panic(err)
	}
}

// SignIn makes the fake signed in.
func (f *Fake) SignIn() { f.write("signedin", "") }

// SignOut makes it signed out again.
func (f *Fake) SignOut() { _ = os.Remove(filepath.Join(f.Dir, "signedin")) }

// Offline makes every API call fail as if GitHub could not be reached.
func (f *Fake) Offline(on bool) {
	if on {
		f.write("offline", "")
	} else {
		_ = os.Remove(filepath.Join(f.Dir, "offline"))
	}
}

// Approve makes a pending sign-in succeed, as the user approving it on GitHub does.
func (f *Fake) Approve() { f.write("approve", "") }

// Deny makes a pending sign-in fail.
func (f *Fake) Deny() { f.write("deny", "") }

// Repos sets the JSON of one page of the repository list.
func (f *Fake) Repos(page int, json string) {
	f.write("repos."+string(rune('0'+page))+".json", json)
}

// Calls returns every invocation so far, one per line.
func (f *Fake) Calls() string {
	b, _ := os.ReadFile(filepath.Join(f.Dir, "calls"))
	return string(b)
}
