package devicestate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devboard/internal/envelope"
)

func open(t *testing.T) (*State, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "device")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestAComputerStartsAskingBeforeItStartsAnything(t *testing.T) {
	s, dir := open(t)
	got := s.Settings()
	if got.RemoteStart != RemoteStartAsk || !got.RemoteOpen {
		t.Fatalf("%+v", got)
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Fatalf("the directory is %v", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(filepath.Join(dir, "device.json")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("the file is %v", fi.Mode().Perm())
	}
	for name, bad := range map[string]Settings{"a policy that does not exist": {RemoteStart: "sometimes"}, "a form that does not exist": {RemoteStart: RemoteStartAsk, Form: "toaster"}} {
		if err := s.SetSettings(bad); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := s.SetSettings(Settings{RemoteStart: RemoteStartOff, Form: "desktop"}); err != nil || s.Settings().RemoteStart != RemoteStartOff {
		t.Fatal(err)
	}
}

func TestCorruptPolicyUnknownFieldsAndSymlinksFailClosed(t *testing.T) {
	for _, raw := range []string{`{"settings":{"remoteStart":"surprise"}}`, `{"settings":{"remoteStart":"ask"},"shell":"whoami"}`, `{"settings":{"remoteStart":"ask"}} {}`} {
		dir := t.TempDir()
		path := filepath.Join(dir, "device.json")
		_ = os.WriteFile(path, []byte(raw), 0600)
		if _, err := Open(dir); err == nil {
			t.Fatal("corrupt state opened")
		}
		got, _ := os.ReadFile(path)
		if string(got) != raw {
			t.Fatal("corrupt state was overwritten")
		}
	}
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "state")
	_ = os.WriteFile(target, []byte(`{}`), 0600)
	_ = os.Symlink(target, filepath.Join(dir, "device.json"))
	if _, err := Open(dir); err == nil {
		t.Fatal("state symlink followed")
	}
}

func TestAnotherDeviceIsHeardOfButNotTrustedUntilThePersonSaysSo(t *testing.T) {
	s, _ := open(t)
	if s.IsApprovedSender("dev_phone") {
		t.Fatal("an unknown device is trusted")
	}
	if err := s.NoteSender("dev_phone", "Bo's phone"); err != nil {
		t.Fatal(err)
	}
	if s.IsApprovedSender("dev_phone") {
		t.Fatal("a device that was only heard of is trusted")
	}
	if err := s.Approve("dev_nobody"); err == nil {
		t.Fatal("a device that was never heard of was approved")
	}
	if err := s.Approve("dev_phone"); err != nil || !s.IsApprovedSender("dev_phone") {
		t.Fatal(err)
	}
	// Noting it again (the workspace lists it again) does not undo the person's decision, and revoking does.
	_ = s.NoteSender("dev_phone", "Bo's iPhone")
	if !s.IsApprovedSender("dev_phone") || s.Senders()[0].Name != "Bo's iPhone" {
		t.Fatal("renaming changed trust")
	}
	_ = s.Revoke("dev_phone")
	if s.IsApprovedSender("dev_phone") {
		t.Fatal("a revoked device is trusted")
	}
}

func TestAPermissionToStartIsForOneTaskOnceAndExpires(t *testing.T) {
	s, _ := open(t)
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })
	a, err := s.Allow("tsk_1", "prj_1", "WB-1")
	if err != nil || !strings.HasPrefix(a.ID, "apr_") {
		t.Fatalf("%+v %v", a, err)
	}
	// Not for another task, not a guess, not a made-up ID.
	for _, c := range [][2]string{{a.ID, "tsk_2"}, {"apr_0000000000000000", "tsk_1"}, {"", "tsk_1"}} {
		if _, err := s.Consume(c[0], c[1]); err != ErrNoApproval {
			t.Errorf("Consume(%q, %q) = %v", c[0], c[1], err)
		}
	}
	pid, err := s.Consume(a.ID, "tsk_1")
	if err != nil || pid != "prj_1" {
		t.Fatalf("%q %v", pid, err)
	}
	if _, err := s.Consume(a.ID, "tsk_1"); err != ErrNoApproval {
		t.Fatalf("a permission was spent twice: %v", err)
	}
	b, _ := s.Allow("tsk_3", "prj_1", "WB-3")
	now = now.Add(ApprovalLife + time.Second)
	if _, err := s.Consume(b.ID, "tsk_3"); err != ErrNoApproval || len(s.Approvals()) != 0 {
		t.Fatalf("an expired permission worked: %v", err)
	}
}

func TestARequestIsRememberedAcrossARestartAndANewOneIsNot(t *testing.T) {
	s, dir := open(t)
	exp := time.Now().Add(time.Minute)
	if seen, err := s.Seen("dev_a", "msg_1", "nonce", exp); err != nil || seen {
		t.Fatalf("%v %v", seen, err)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if seen, _ := again.Seen("dev_a", "msg_1", "nonce", exp); !seen {
		t.Fatal("a request was not remembered across a restart")
	}
	if seen, _ := again.Seen("dev_a", "msg_2", "nonce", exp); !seen {
		t.Fatal("a reused nonce was not a replay")
	}
	if seen, _ := again.Seen("dev_a", "msg_1", "new_nonce", exp); !seen {
		t.Fatal("a reused message ID was not a replay")
	}
	if seen, _ := again.Seen("dev_a", "msg_2", "new_nonce", exp); seen {
		t.Fatal("new identifiers were called a replay")
	}
	if seen, _ := again.Seen("dev_b", "msg_1", "nonce", exp); seen {
		t.Fatal("one device's request shadowed another's")
	}
	var _ envelope.ReplayCache = again
}

func TestTwoCopiesOfOneRequestAreNotBothNew(t *testing.T) {
	s, _ := open(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	fresh := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if seen, err := s.Seen("dev_a", "msg_1", "n", time.Now().Add(time.Minute)); err == nil && !seen {
				mu.Lock()
				fresh++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if fresh != 1 {
		t.Fatalf("%d copies were new", fresh)
	}
}

func TestWhatWasDoneIsKeptAndTheOldIsForgotten(t *testing.T) {
	s, dir := open(t)
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })
	_ = s.RecordOutcome(Outcome{MessageID: "msg_1", State: "done", Result: json.RawMessage(`{"taskId":"t"}`)})
	if o, ok := s.Outcome("msg_1"); !ok || o.State != "done" || string(o.Result) != `{"taskId":"t"}` {
		t.Fatalf("%+v %v", o, ok)
	}
	if _, ok := s.Outcome("msg_none"); ok {
		t.Fatal("an outcome from nowhere")
	}
	now = now.Add(keepOutcomes + time.Hour)
	_ = s.RecordOutcome(Outcome{MessageID: "msg_2", State: "refused"})
	if _, ok := s.Outcome("msg_1"); ok {
		t.Fatal("an old outcome was kept")
	}
	again, _ := Open(dir)
	if _, ok := again.Outcome("msg_2"); !ok {
		t.Fatal("an outcome did not survive a restart")
	}
}

func TestTheLocalKeyIsMadeOnceAndChecked(t *testing.T) {
	s, dir := open(t)
	k := s.LocalKey()
	if len(k) != 64 || !s.CheckLocalKey(k) || s.CheckLocalKey("") || s.CheckLocalKey(k+"x") || s.CheckLocalKey(strings.ToUpper(k)) {
		t.Fatalf("key %q", k)
	}
	again, _ := Open(dir)
	if again.LocalKey() != k {
		t.Fatal("the key changed")
	}
}

func TestADamagedFileIsNotReplacedWithAnEmptyOne(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "device")
	_ = os.MkdirAll(dir, 0o700)
	path := filepath.Join(dir, "device.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("a damaged file was opened")
	}
	if b, _ := os.ReadFile(path); string(b) != "{not json" {
		t.Fatal("a damaged file was overwritten")
	}
}

func TestWorkspaceHostCannotReplaceTheApprovedApplicationKey(t *testing.T) {
	s, dir := open(t)
	_ = s.NoteDevice("dev_phone", "Phone", "key-original")
	_ = s.Approve("dev_phone")
	_ = s.NoteDevice("dev_phone", "Renamed", "key-substituted")
	next, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := next.Senders()[0]
	if got.PublicKey != "key-original" || !next.IsApprovedSender(got.DeviceID) {
		t.Fatal("approved identity was substituted")
	}
	key := next.LocalKey()
	settings := next.Settings()
	if err := next.ResetWorkspace(); err != nil {
		t.Fatal(err)
	}
	after, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Senders()) != 0 || after.LocalKey() != key || after.Settings() != settings {
		t.Fatal("workspace reset lost installation state or retained trust")
	}
}
