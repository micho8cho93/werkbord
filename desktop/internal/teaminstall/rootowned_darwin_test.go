package teaminstall

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootDoesNotRunTheTeamAppFromAPlaceOthersCanChange(t *testing.T) {
	me := uint32(os.Getuid())
	app := filepath.Join(t.TempDir(), "Werkbord.app")
	for _, dir := range []string{"Contents/MacOS", "Contents/Helpers"} {
		if err := os.MkdirAll(filepath.Join(app, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	exe := filepath.Join(app, "Contents", "MacOS", "Werkbord")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("MacOS", filepath.Join(app, "Contents", "Link")); err != nil {
		t.Fatal(err)
	}
	// The temporary folder's own parents are not ours to arrange, so the walk over the app is what these cases are about.
	if err := walkOnly(app, me); err != nil {
		t.Fatalf("an app only its owner can change was refused: %v", err)
	}
	// Owned by someone else than the user it must belong to (here: not me), as an app the person dragged in is to root.
	if err := walkOnly(app, me+1); err == nil || !strings.Contains(err.Error(), "not root's") {
		t.Fatalf("an app owned by another user was accepted: %v", err)
	}
	// One file others can write is enough.
	if err := os.Chmod(exe, 0o775); err != nil {
		t.Fatal(err)
	}
	if err := walkOnly(app, me); err == nil || !strings.Contains(err.Error(), "can be changed by others") {
		t.Fatalf("a group-writable program was accepted: %v", err)
	}
	if err := os.Chmod(exe, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(app, "Contents", "Helpers"), 0o757); err != nil {
		t.Fatal(err)
	}
	if err := walkOnly(app, me); err == nil {
		t.Fatal("a folder anyone can write to was accepted")
	}
}

// walkOnly runs requireOwnedTree without the folders above the app, which a test does not choose.
func walkOnly(root string, uid uint32) error {
	return requireOwnedTree(root, uid, false)
}

func TestAFolderAnyoneCanWriteToIsOnlyAcceptedAboveTheAppAndOnlyWhenSticky(t *testing.T) {
	me := uint32(os.Getuid())
	dir := t.TempDir()
	open := filepath.Join(dir, "open")
	if err := os.Mkdir(open, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o777); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Lstat(open)
	if unsafeEntry(fi, me, true) == "" {
		t.Fatal("a folder anyone can write to, not sticky, was accepted above the app")
	}
	if err := os.Chmod(open, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	fi, _ = os.Lstat(open)
	if unsafeEntry(fi, me, true) != "" {
		t.Fatal("a sticky folder (as /private/var/tmp is) was refused above the app")
	}
	if unsafeEntry(fi, me, false) == "" {
		t.Fatal("a sticky folder was accepted inside the app")
	}
	if unsafeEntry(fi, me+1, true) == "" {
		t.Fatal("a folder of someone else was accepted")
	}
	// The real folders the checked copy lives under.
	for _, p := range []string{"/", "/private", "/private/var", "/private/var/tmp"} {
		fi, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		if why := unsafeEntry(fi, 0, true); why != "" {
			t.Fatalf("%s: %s", p, why)
		}
	}
}

func TestTheAdministratorCommandCopiesChecksAndOnlyThenRunsTheCopy(t *testing.T) {
	app := "/Users/pat/Applications/Wer'k \"bord\".app"
	exe := app + "/Contents/MacOS/Werkbord"
	cmd, err := privilegedCommand(app, exe, `=anchor apple generic and certificate leaf[subject.OU] = "ABCDE12345"`, "maintain", "501")
	if err != nil {
		t.Fatal(err)
	}
	// In this order, and each step stops the rest when it fails.
	order := []string{"/usr/bin/mktemp -d /private/var/tmp/werkbord-team.", "trap ", "/usr/bin/ditto --noqtn ", "/usr/sbin/chown -R 0:0", "/bin/chmod -R go-w", "/usr/bin/codesign --verify --strict --deep -R ", `"$A"/'Contents/MacOS/Werkbord' --team-service 'maintain' '501'`}
	at := -1
	for _, want := range order {
		i := strings.Index(cmd, want)
		if i <= at {
			t.Fatalf("%q is missing or out of order in:\n%s", want, cmd)
		}
		at = i
	}
	if strings.Count(cmd, "exit ") != 5 {
		t.Fatalf("each checking step must stop the rest:\n%s", cmd)
	}
	// What runs as root is the checked copy: the app's own path is only ever read from.
	if strings.Count(cmd, `Wer'"'"'k "bord".app`) != 2 { // the source of the copy, and the name the copy keeps
		t.Fatalf("the app's path must be quoted wherever it is used:\n%s", cmd)
	}
	last := cmd[strings.LastIndex(cmd, "; ")+2:]
	if strings.Contains(last, "Users/pat") {
		t.Fatalf("the program run as root is not the copy: %s", last)
	}
	// It is valid shell, however hostile the path.
	if out, err := exec.Command("/bin/sh", "-n", "-c", cmd).CombinedOutput(); err != nil {
		t.Fatalf("not valid shell: %v %s\n%s", err, out, cmd)
	}
	if _, err := privilegedCommand(app, "/elsewhere/Werkbord", "r", "install", "501"); err == nil {
		t.Fatal("an installer outside its app was accepted")
	}
}
