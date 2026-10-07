package runnerlink

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"devboard/internal/deviceid"
	"devboard/internal/deviceid/localidentity"
	"devboard/internal/envelope"
	"devboard/internal/team/devicestate"
	"devboard/internal/team/domain"
	"devboard/internal/team/hostclient"
	"devboard/internal/team/localwerkbord"
)

var bg = context.Background()

const (
	ws  = "tws_1"
	bo  = "tmb_bo"
	cy  = "tmb_cy"
	pr  = "tpj_1"
	tk  = "ttk_1"
	rep = "https://github.com/acme/shop"
)

// directory is the workspace's registry as a verifier sees it.
type directory map[string]envelope.Device

func (d directory) Device(_ context.Context, w, id string) (envelope.Device, error) {
	dev, ok := d[id]
	if !ok || dev.WorkspaceID != w {
		return envelope.Device{}, envelope.ErrUnknownKey
	}
	return dev, nil
}

type fakeBridge struct {
	mu      sync.Mutex
	calls   []string
	err     error
	tasks   int
	project localwerkbord.Project
	status  localwerkbord.Status
	last    localwerkbord.NewTask
}

func (b *fakeBridge) rec(s string) { b.mu.Lock(); b.calls = append(b.calls, s); b.mu.Unlock() }
func (b *fakeBridge) ProjectFor(_ context.Context, repo string) (localwerkbord.Project, error) {
	b.rec("ProjectFor " + repo)
	return b.project, b.err
}
func (b *fakeBridge) CreateTask(_ context.Context, p string, t localwerkbord.NewTask) (string, error) {
	b.rec("CreateTask " + p)
	b.last = t
	b.tasks++
	return "tsk_" + string(rune('0'+b.tasks)), b.err
}
func (b *fakeBridge) StartRun(_ context.Context, p, t string) (string, error) {
	b.rec("StartRun " + p + " " + t)
	return "run_1", b.err
}
func (b *fakeBridge) StopRun(_ context.Context, r string) error { b.rec("StopRun " + r); return b.err }
func (b *fakeBridge) Answer(_ context.Context, r, q, o, w string) error {
	b.rec("Answer " + r + " " + q + " " + o + " " + w)
	return b.err
}
func (b *fakeBridge) Status(context.Context) (localwerkbord.Status, error) {
	b.rec("Status")
	return b.status, b.err
}
func (b *fakeBridge) count(prefix string) int {
	n := 0
	for _, c := range b.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

type fakeHandoffs struct {
	err error
	h   hostclient.Handoff
}

func (f fakeHandoffs) Handoff(context.Context, string, string) (hostclient.Handoff, error) {
	return f.h, f.err
}

type rig struct {
	t       *testing.T
	phone   *localidentity.Identity
	mac     *localidentity.Identity
	cyPhone *localidentity.Identity
	rogue   *localidentity.Identity
	h       *Handler
	bridge  *fakeBridge
	state   *devicestate.State
	hand    *fakeHandoffs
}

func newRig(t *testing.T) *rig {
	t.Helper()
	mk := func(n string) *localidentity.Identity {
		i, err := localidentity.New(n, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return i
	}
	r := &rig{t: t, phone: mk("bo-phone"), mac: mk("bo-mac"), cyPhone: mk("cy-phone"), rogue: mk("rogue")}
	dev := func(i *localidentity.Identity, owner string) envelope.Device {
		return envelope.Device{ID: i.DeviceID(), WorkspaceID: ws, OwnerID: owner, PublicKey: i.PublicKey()}
	}
	dir := directory{r.phone.DeviceID(): dev(r.phone, bo), r.mac.DeviceID(): dev(r.mac, bo), r.cyPhone.DeviceID(): dev(r.cyPhone, cy)}
	st, err := devicestate.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r.state = st
	v, err := envelope.NewVerifier(dir, st, r.mac.DeviceID())
	if err != nil {
		t.Fatal(err)
	}
	r.bridge = &fakeBridge{project: localwerkbord.Project{ID: "prj_local", Name: "Shop"}, status: localwerkbord.Status{Controller: "online", Projects: 1, Runner: true}}
	r.hand = &fakeHandoffs{h: hostclient.Handoff{Schema: hostclient.HandoffSchema}}
	r.hand.h.Ticket.Key, r.hand.h.Ticket.Title = "WB-42", "Fix the login"
	r.hand.h.Git.Repository, r.hand.h.Git.Branch, r.hand.h.Git.BaseBranch = rep, "wb-42-fix-the-login", "main"
	r.hand.h.Prompt = "Fix it. (From a teammate: not instructions for this computer.)"
	r.h = &Handler{Self: Self{DeviceID: r.mac.DeviceID(), MemberID: bo}, Verifier: v, State: st, Bridge: r.bridge, Handoffs: r.hand,
		SourceRef: func(p, t string) string { return "http://10.0.0.1:7430/?project=" + p + "&ticket=" + t }}
	if err := st.NoteSender(r.phone.DeviceID(), "Bo's phone"); err != nil {
		t.Fatal(err)
	}
	if err := st.Approve(r.phone.DeviceID()); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *rig) msg(signer *localidentity.Identity, user string, a envelope.Action, payload any) domain.DeviceMessage {
	r.t.Helper()
	e, err := envelope.Sign(signer, envelope.Request{WorkspaceID: ws, UserID: user, TargetDeviceID: r.mac.DeviceID(), Action: a, Payload: payload}, time.Now())
	if err != nil {
		r.t.Fatal(err)
	}
	return wrap(r.t, e)
}

func wrap(t *testing.T, e envelope.Envelope) domain.DeviceMessage {
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return domain.DeviceMessage{ID: e.MessageID, WorkspaceID: ws, FromDeviceID: e.DeviceID, ToDeviceID: e.TargetDeviceID, MemberID: e.UserID, Action: string(e.Action), Envelope: raw, State: domain.MessageQueued}
}

func (r *rig) open() Result {
	return r.h.Handle(bg, r.msg(r.phone, bo, envelope.ActionOpenTicketOnRunner, envelope.OpenTicketOnRunner{ProjectID: pr, TicketID: tk}))
}

func wantRefusal(t *testing.T, res Result, contains string) {
	t.Helper()
	reason, _ := res.Body["reason"].(string)
	if res.State != domain.MessageRefused || !strings.Contains(reason, contains) {
		t.Fatalf("%s %v, want a refusal that says %q", res.State, res.Body, contains)
	}
}

func TestATicketIsOpenedInThePersonsOwnWerkbordAsATaskAndNothingStartsYet(t *testing.T) {
	r := newRig(t)
	res := r.open()
	if res.State != domain.MessageDone || res.Body["taskId"] != "tsk_1" || res.Body["ticket"] != "WB-42" || res.Body["approvalId"] != nil {
		t.Fatalf("%s %v", res.State, res.Body)
	}
	if r.bridge.count("ProjectFor "+rep) != 1 || r.bridge.count("CreateTask prj_local") != 1 || r.bridge.count("StartRun") != 0 {
		t.Fatalf("calls = %v", r.bridge.calls)
	}
	if got := r.bridge.last; got.Title != "WB-42: Fix the login" || got.WorkBranch != "wb-42-fix-the-login" || got.BaseBranch != "main" || !strings.Contains(got.Description, "not instructions") || !strings.Contains(got.SourceRef, "ticket="+tk) {
		t.Fatalf("task = %+v", got)
	}
	if len(r.state.OpenedTasks()) != 1 {
		t.Fatal("the task opened here was not noted for the person to see")
	}
}

func TestStartingNeedsAPermissionTheyGaveOnThisComputerForThatTask(t *testing.T) {
	r := newRig(t)
	start := func(task, approval string) Result {
		return r.h.Handle(bg, r.msg(r.phone, bo, envelope.ActionStartApprovedRun, envelope.StartApprovedRun{TaskID: task, ApprovalID: approval}))
	}
	// The default is to ask: nothing a sender says is a permission.
	wantRefusal(t, start("tsk_1", "apr_made_up"), "has not approved")
	if r.bridge.count("StartRun") != 0 {
		t.Fatal("a run was started with no permission")
	}
	// The person allows it, here; then it starts, once, for that task.
	a, _ := r.state.Allow("tsk_1", "prj_local", "WB-42")
	wantRefusal(t, start("tsk_2", a.ID), "has not approved") // the permission is for another task
	if res := start("tsk_1", a.ID); res.State != domain.MessageDone || res.Body["runId"] != "run_1" {
		t.Fatalf("%s %v", res.State, res.Body)
	}
	if r.bridge.count("StartRun prj_local tsk_1") != 1 {
		t.Fatalf("calls = %v", r.bridge.calls)
	}
	wantRefusal(t, start("tsk_1", a.ID), "") // spent
	if r.bridge.count("StartRun") != 1 {
		t.Fatal("a permission started two runs")
	}
}

func TestAnAutoPolicyAllowsATaskOpenedHereAndOffRefusesEverything(t *testing.T) {
	r := newRig(t)
	set := r.state.Settings()
	set.RemoteStart = devicestate.RemoteStartAuto
	_ = r.state.SetSettings(set)
	res := r.open()
	approval, _ := res.Body["approvalId"].(string)
	if res.State != domain.MessageDone || approval == "" {
		t.Fatalf("%s %v", res.State, res.Body)
	}
	if s := r.h.Handle(bg, r.msg(r.phone, bo, envelope.ActionStartApprovedRun, envelope.StartApprovedRun{TaskID: "tsk_1", ApprovalID: approval})); s.State != domain.MessageDone {
		t.Fatalf("%s %v", s.State, s.Body)
	}
	set.RemoteStart = devicestate.RemoteStartOff
	_ = r.state.SetSettings(set)
	a, _ := r.state.Allow("tsk_9", "prj_local", "WB-9")
	wantRefusal(t, r.h.Handle(bg, r.msg(r.phone, bo, envelope.ActionStartApprovedRun, envelope.StartApprovedRun{TaskID: "tsk_9", ApprovalID: a.ID})), "turned off")
	set.RemoteOpen = false
	_ = r.state.SetSettings(set)
	wantRefusal(t, r.open(), "turned off")
}

// What a host, or anyone between the devices, could try.
func TestARequestThatDoesNotVerifyOrIsNotThePersonsIsNeverActedOn(t *testing.T) {
	r := newRig(t)
	good := r.msg(r.phone, bo, envelope.ActionFetchRunnerStatus, envelope.FetchRunnerStatus{})
	var env envelope.Envelope
	_ = json.Unmarshal(good.Envelope, &env)

	cases := map[string]domain.DeviceMessage{
		"a device nobody registered":       r.msg(r.rogue, bo, envelope.ActionFetchRunnerStatus, envelope.FetchRunnerStatus{}),
		"a device naming another person":   r.msg(r.cyPhone, bo, envelope.ActionFetchRunnerStatus, envelope.FetchRunnerStatus{}),
		"another person's own device":      r.msg(r.cyPhone, cy, envelope.ActionFetchRunnerStatus, envelope.FetchRunnerStatus{}),
		"an altered action":                alter(t, env, func(e *envelope.Envelope) { e.Action = envelope.ActionCancelRun }),
		"an altered payload":               alter(t, env, func(e *envelope.Envelope) { e.Payload = []byte(`{"runId":"x"}`) }),
		"another target":                   alter(t, env, func(e *envelope.Envelope) { e.TargetDeviceID = r.phone.DeviceID() }),
		"a different message ID than sent": func() domain.DeviceMessage { m := good; m.ID = "msg_other"; return m }(),
		"nothing":                          {ID: "msg_x", Envelope: []byte(`{}`)},
		"not even JSON":                    {ID: "msg_y", Envelope: []byte(`rm -rf /`)},
	}
	for name, m := range cases {
		res := r.h.Handle(bg, m)
		if res.State != domain.MessageRefused || res.Retry {
			t.Errorf("%s: %s %v", name, res.State, res.Body)
		}
	}
	if len(r.bridge.calls) != 0 {
		t.Fatalf("something reached Werkbord: %v", r.bridge.calls)
	}
	// The genuine one, after all that, still works, once.
	if res := r.h.Handle(bg, good); res.State != domain.MessageDone {
		t.Fatalf("%s %v", res.State, res.Body)
	}
	// Handed over again, it is not done again, and it is answered as it was.
	if res := r.h.Handle(bg, good); res.State != domain.MessageDone || r.bridge.count("Status") != 1 {
		t.Fatalf("%s %v calls=%v", res.State, res.Body, r.bridge.calls)
	}
}

// A tampered copy that carries a genuine request's ID is refused each time and remembered as nothing, so it cannot use up the
// ID: the genuine request, if it ever arrives, is still done.
func TestATamperedCopyCannotSpendAGenuineRequestsID(t *testing.T) {
	r := newRig(t)
	good := r.msg(r.phone, bo, envelope.ActionFetchRunnerStatus, envelope.FetchRunnerStatus{})
	var env envelope.Envelope
	_ = json.Unmarshal(good.Envelope, &env)
	tampered := alter(t, env, func(e *envelope.Envelope) { e.Action = envelope.ActionCancelRun })
	for i := 0; i < 3; i++ {
		wantRefusal(t, r.h.Handle(bg, tampered), "does not verify")
	}
	if _, ok := r.state.Outcome(env.MessageID); ok {
		t.Fatal("a request that did not verify was remembered")
	}
	if res := r.h.Handle(bg, good); res.State != domain.MessageDone {
		t.Fatalf("%s %v", res.State, res.Body)
	}
	if len(r.bridge.calls) != 1 || r.bridge.calls[0] != "Status" {
		t.Fatalf("calls = %v", r.bridge.calls)
	}
}

func alter(t *testing.T, e envelope.Envelope, f func(*envelope.Envelope)) domain.DeviceMessage {
	f(&e)
	return wrap(t, e)
}

func TestARequestFromADeviceThePersonHasNotApprovedWaits(t *testing.T) {
	r := newRig(t)
	_ = r.state.Revoke(r.phone.DeviceID())
	wantRefusal(t, r.h.Handle(bg, r.msg(r.phone, bo, envelope.ActionFetchRunnerStatus, envelope.FetchRunnerStatus{})), "approve it in Werkbord Team on this computer")
	if len(r.bridge.calls) != 0 {
		t.Fatalf("calls = %v", r.bridge.calls)
	}
	// This computer asking itself needs no approval: it is the person's own.
	if res := r.h.Handle(bg, r.msg(r.mac, bo, envelope.ActionFetchRunnerStatus, envelope.FetchRunnerStatus{})); res.State != domain.MessageDone {
		t.Fatalf("%s %v", res.State, res.Body)
	}
	// Approving the phone lets it through, and only the person at this computer can.
	_ = r.state.Approve(r.phone.DeviceID())
	if res := r.h.Handle(bg, r.msg(r.phone, bo, envelope.ActionFetchRunnerStatus, envelope.FetchRunnerStatus{})); res.State != domain.MessageDone {
		t.Fatalf("%s %v", res.State, res.Body)
	}
}

func TestAnExpiredRequestIsRefused(t *testing.T) {
	r := newRig(t)
	e, _ := envelope.Sign(r.phone, envelope.Request{WorkspaceID: ws, UserID: bo, TargetDeviceID: r.mac.DeviceID(), Action: envelope.ActionFetchRunnerStatus, Payload: envelope.FetchRunnerStatus{}}, time.Now().Add(-time.Hour))
	wantRefusal(t, r.h.Handle(bg, wrap(t, e)), "expired")
}

// Werkbord not running is not a refusal: the request waits, and is done when it is, without being taken for a replay.
func TestARequestWaitsForWerkbordAndIsNotMistakenForAReplay(t *testing.T) {
	r := newRig(t)
	m := r.msg(r.phone, bo, envelope.ActionCancelRun, envelope.CancelRun{RunID: "run_7"})
	r.bridge.err = localwerkbord.ErrNotRunning
	if res := r.h.Handle(bg, m); !res.Retry {
		t.Fatalf("%s %v", res.State, res.Body)
	}
	if _, ok := r.state.Outcome(m.ID); ok {
		t.Fatal("a request that could not be tried was recorded as handled")
	}
	r.bridge.err = nil
	if res := r.h.Handle(bg, m); res.State != domain.MessageDone || res.Retry {
		t.Fatalf("%s %v retry=%v", res.State, res.Body, res.Retry)
	}
	if r.bridge.count("StopRun run_7") != 2 { // tried twice, done once
		t.Fatalf("calls = %v", r.bridge.calls)
	}
	// With no Werkbord connected at all it waits too.
	r2 := newRig(t)
	r2.h.Bridge = nil
	if res := r2.h.Handle(bg, r2.msg(r2.phone, bo, envelope.ActionFetchRunnerStatus, envelope.FetchRunnerStatus{})); !res.Retry {
		t.Fatalf("%+v", res)
	}
}

func TestWhatWerkbordOrTheWorkspaceRefusesIsSaidInTheirWords(t *testing.T) {
	r := newRig(t)
	r.bridge.err = &localwerkbord.StatusError{Code: 403, Message: "this program's access to Werkbord does not include that"}
	wantRefusal(t, r.h.Handle(bg, r.msg(r.phone, bo, envelope.ActionCancelRun, envelope.CancelRun{RunID: "run_1"})), "does not include")
	r.bridge.err = localwerkbord.ErrAccessDenied
	wantRefusal(t, r.h.Handle(bg, r.msg(r.phone, bo, envelope.ActionCancelRun, envelope.CancelRun{RunID: "run_2"})), "connect it again")
	r.bridge.err = nil
	r.hand.err = &hostclient.Error{Status: 403, Message: "your role cannot hand off a ticket you do not hold"}
	wantRefusal(t, r.open(), "do not hold")
	r.hand.err = hostclient.ErrUnreachable
	if res := r.h.Handle(bg, r.msg(r.phone, bo, envelope.ActionOpenTicketOnRunner, envelope.OpenTicketOnRunner{ProjectID: pr, TicketID: "ttk_2"})); !res.Retry {
		t.Fatalf("an unreachable workspace was a refusal: %+v", res)
	}
	r.hand.err = nil
	r.bridge.err = errors.New("none of your Werkbord projects uses https://github.com/acme/shop")
	wantRefusal(t, r.h.Handle(bg, r.msg(r.phone, bo, envelope.ActionOpenTicketOnRunner, envelope.OpenTicketOnRunner{ProjectID: pr, TicketID: "ttk_3"})), "could not do that")
}

func TestAnAnswerGoesToTheAgentOnlyAsAnOptionOrWords(t *testing.T) {
	r := newRig(t)
	res := r.h.Handle(bg, r.msg(r.phone, bo, envelope.ActionRespondToAgentQuestion, envelope.RespondToAgentQuestion{RunID: "run_1", QuestionID: "q_1", OptionID: "1"}))
	if res.State != domain.MessageDone || r.bridge.count("Answer run_1 q_1 1 ") != 1 {
		t.Fatalf("%s %v %v", res.State, res.Body, r.bridge.calls)
	}
}

func TestAStatusNamesNothingButCountsAndWhatMayBeStarted(t *testing.T) {
	r := newRig(t)
	a, _ := r.state.Allow("tsk_5", "prj_local", "WB-5")
	res := r.h.Handle(bg, r.msg(r.phone, bo, envelope.ActionFetchRunnerStatus, envelope.FetchRunnerStatus{}))
	b, _ := json.Marshal(res.Body)
	if res.State != domain.MessageDone || !strings.Contains(string(b), a.ID) || !strings.Contains(string(b), `"remoteStart":"ask"`) {
		t.Fatalf("%s %s", res.State, b)
	}
	for _, leak := range []string{"/Users", "token", "Bearer", "prj_local"} {
		if strings.Contains(string(b), leak) {
			t.Errorf("the status carries %q: %s", leak, b)
		}
	}
	if len(b) > domain.MaxResultBytes {
		t.Fatalf("a status of %d bytes is more than a workspace takes", len(b))
	}
}

// The asking side signs with its own key and hands the workspace what it signed.
type fakeHost struct{ sent []envelope.Envelope }

func (f *fakeHost) Send(_ context.Context, e envelope.Envelope) (domain.DeviceMessage, error) {
	f.sent = append(f.sent, e)
	return domain.DeviceMessage{ID: e.MessageID}, nil
}
func (f *fakeHost) Message(context.Context, string) (domain.DeviceMessage, error) {
	return domain.DeviceMessage{}, nil
}

func TestTheAskingDeviceSignsWithItsOwnKeyForAnotherDeviceOfThePerson(t *testing.T) {
	r := newRig(t)
	h := &fakeHost{}
	s := &Sender{Signer: r.phone, WorkspaceID: ws, UserID: bo, Host: h}
	m, err := s.Ask(bg, r.mac.DeviceID(), envelope.ActionOpenTicketOnRunner, envelope.OpenTicketOnRunner{ProjectID: pr, TicketID: tk})
	if err != nil || len(h.sent) != 1 || h.sent[0].MessageID != m.ID {
		t.Fatalf("%v %+v", err, h.sent)
	}
	e := h.sent[0]
	if e.DeviceID != r.phone.DeviceID() || e.UserID != bo || e.TargetDeviceID != r.mac.DeviceID() || e.Action != envelope.ActionOpenTicketOnRunner {
		t.Fatalf("%+v", e)
	}
	// What was handed to the workspace is a request the target accepts.
	if res := r.h.Handle(bg, wrap(t, e)); res.State != domain.MessageDone {
		t.Fatalf("%s %v", res.State, res.Body)
	}
	// An action that is not on the list, or a payload that is not its own, cannot be signed.
	if _, err := s.Ask(bg, r.mac.DeviceID(), envelope.Action("run_shell"), map[string]string{"cmd": "ls"}); err == nil {
		t.Fatal("an action that does not exist was signed")
	}
	if _, err := s.Ask(bg, r.mac.DeviceID(), envelope.ActionCancelRun, envelope.OpenTicketOnRunner{}); err == nil {
		t.Fatal("a payload of another action was signed")
	}
	var _ = deviceid.ValidID
}

func TestCachedRequestsRemainSignedCurrentAndRevocable(t *testing.T) {
	for _, change := range []string{"payload", "signature", "expired", "revoked", "local trust"} {
		t.Run(change, func(t *testing.T) {
			r := newRig(t)
			message := r.msg(r.phone, bo, envelope.ActionFetchRunnerStatus, envelope.FetchRunnerStatus{})
			r.h.Bridge = nil
			if got := r.h.Handle(bg, message); !got.Retry {
				t.Fatal("not queued", got)
			}
			var e envelope.Envelope
			_ = json.Unmarshal(message.Envelope, &e)
			switch change {
			case "payload":
				e.Action = envelope.ActionCancelRun
				e.Payload = json.RawMessage(`{"runId":"run_other"}`)
				message.Envelope, _ = json.Marshal(e)
			case "signature":
				e.Signature = "changed"
				message.Envelope, _ = json.Marshal(e)
			case "expired":
				r.h.Verifier.Now = func() time.Time { return e.ExpiryTime().Add(envelope.DefaultSkew + time.Second) }
			case "revoked":
				d := r.h.Verifier.Directory.(directory)
				v := d[r.phone.DeviceID()]
				v.Revoked = true
				d[v.ID] = v
			case "local trust":
				_ = r.state.Revoke(r.phone.DeviceID())
			}
			r.h.Bridge = r.bridge
			if got := r.h.Handle(bg, message); got.State != domain.MessageRefused {
				t.Fatal("changed queued request executed", got)
			}
			if len(r.bridge.calls) != 0 {
				t.Fatal("bridge called", r.bridge.calls)
			}
		})
	}
}

func TestAStartWithALostAnswerNeverSpendsItsApprovalTwice(t *testing.T) {
	r := newRig(t)
	a, _ := r.state.Allow("tsk_1", "prj_local", "WB-42")
	m := r.msg(r.phone, bo, envelope.ActionStartApprovedRun, envelope.StartApprovedRun{TaskID: "tsk_1", ApprovalID: a.ID})
	r.bridge.err = errors.New("answer lost")
	r.h.Handle(bg, m)
	r.bridge.err = nil
	r.h.Handle(bg, m)
	if r.bridge.count("StartRun") != 1 {
		t.Fatal("uncertain start repeated", r.bridge.calls)
	}
}
