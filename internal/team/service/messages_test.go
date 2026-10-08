package service

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"devboard/internal/deviceid/localidentity"
	"devboard/internal/envelope"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// A person with two devices: a phone, which asks, and a Mac, a runner, which is asked.
type pair struct {
	*netWorld
	phone, mac       *localidentity.Identity
	phoneA, macA     Actor
	memberID         string
	macDeviceToken   string
	phoneDeviceToken string
}

func newPair(t *testing.T) *pair {
	t.Helper()
	n := withNetwork(t)
	p := &pair{netWorld: n, phone: laptop(t, "bo-phone"), mac: laptop(t, "bo-mac")}
	r1 := n.invite(n.owner, EnrollInviteInput{Label: "Bo"})
	resp := n.mustJoin(r1, p.phone, "Bo")
	p.memberID, p.phoneDeviceToken = resp.MemberID, resp.DeviceToken
	r2 := n.invite(n.owner, EnrollInviteInput{ForMemberID: resp.MemberID, Capabilities: []domain.Capability{domain.CapabilityRunner}})
	resp2 := n.mustJoin(r2, p.mac, "")
	p.macDeviceToken = resp2.DeviceToken
	p.phoneA, p.macA = n.deviceActor(p.phoneDeviceToken), n.deviceActor(p.macDeviceToken)
	return p
}

func (p *pair) ask(signer *localidentity.Identity, user string, target *localidentity.Identity, a envelope.Action, payload any, life time.Duration) envelope.Envelope {
	p.t.Helper()
	e, err := envelope.Sign(signer, envelope.Request{WorkspaceID: p.owner.Workspace.ID, UserID: user, TargetDeviceID: target.DeviceID(), Action: a, Payload: payload, Lifetime: life}, time.Now())
	if err != nil {
		p.t.Fatal(err)
	}
	return e
}

func (p *pair) open(ticket string) envelope.Envelope {
	return p.ask(p.phone, p.memberID, p.mac, envelope.ActionOpenTicketOnRunner, envelope.OpenTicketOnRunner{ProjectID: "tpj_1", TicketID: ticket}, time.Minute)
}

// The target checks a request itself, as its daemon will: against the registry's public key, as the one the request is for.
func (p *pair) targetVerifies(e envelope.Envelope) (envelope.Verified, error) {
	v, err := envelope.NewVerifier(p.svc.DeviceDirectory(), &envelope.MemoryReplayCache{}, p.mac.DeviceID())
	if err != nil {
		p.t.Fatal(err)
	}
	return v.Verify(bg, e)
}

func TestAPersonAsksTheirOwnRunnerAndOnlyThatRunnerCollectsIt(t *testing.T) {
	p := newPair(t)
	e := p.open("ttk_1")
	m, err := p.svc.SendMessage(bg, p.phoneA, e)
	if err != nil || m.State != domain.MessageQueued || m.ToDeviceID != p.mac.DeviceID() || m.FromDeviceID != p.phone.DeviceID() || m.Envelope != nil {
		t.Fatalf("%+v %v", m, err)
	}

	// The phone's own inbox holds nothing; the Mac's holds the signed request.
	if got, err := p.svc.Inbox(bg, p.phoneA, 0); err != nil || len(got) != 0 {
		t.Fatalf("the sender collected a request that is not for it: %v %v", got, err)
	}
	got, err := p.svc.Inbox(bg, p.macA, 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("the Mac's inbox: %v %v", got, err)
	}
	var back envelope.Envelope
	if err := json.Unmarshal(got[0].Envelope, &back); err != nil {
		t.Fatal(err)
	}
	// What the Mac holds is what the phone signed, and it verifies as the Mac's own.
	ver, err := p.targetVerifies(back)
	if err != nil {
		t.Fatalf("the signed request does not verify at the Mac: %v", err)
	}
	if pl, ok := ver.Payload.(envelope.OpenTicketOnRunner); !ok || pl.TicketID != "ttk_1" || ver.Device.OwnerID != p.memberID {
		t.Fatalf("%+v", ver)
	}

	// The Mac answers; the person sees it from either device, or from their own token.
	ack, err := p.svc.AckMessage(bg, p.macA, m.ID, domain.MessageDone, json.RawMessage(`{"taskId":"tsk_9"}`))
	if err != nil || ack.State != domain.MessageDone || string(ack.Result) != `{"taskId":"tsk_9"}` {
		t.Fatalf("%+v %v", ack, err)
	}
	seen, err := p.svc.Message(bg, p.phoneA, m.ID)
	if err != nil || seen.State != domain.MessageDone || seen.Envelope != nil {
		t.Fatalf("%+v %v", seen, err)
	}
	if list, _ := p.svc.Messages(bg, p.phoneA); len(list) != 1 || list[0].State != domain.MessageDone {
		t.Fatalf("%+v", list)
	}
	// Answered once.
	_, err = p.svc.AckMessage(bg, p.macA, m.ID, domain.MessageRefused, nil)
	wantErr(t, err, domain.ErrConflict)
	// And it does not come back.
	if got, _ := p.svc.Inbox(bg, p.macA, 0); len(got) != 0 {
		t.Fatalf("an answered request came back: %v", got)
	}
}

// What a Workspace Host could try, and what each attempt meets.
func TestAWorkspaceHostCannotForgeAPersonsRequest(t *testing.T) {
	p := newPair(t)

	// 1. The host's own key signing as Bo: the registry says that key is Ada's host's, not Bo's.
	forged := p.ask(p.host, p.memberID, p.mac, envelope.ActionStartApprovedRun, envelope.StartApprovedRun{TaskID: "tsk_1", ApprovalID: "apr_1"}, time.Minute)
	if _, err := p.targetVerifies(forged); err == nil {
		t.Fatal("a request signed by the host's key, naming Bo, verified at Bo's Mac")
	}
	hostActor := p.deviceActorFor(t, p.host)
	_, err := p.svc.SendMessage(bg, hostActor, forged)
	wantErr(t, err, domain.ErrForbidden) // names another person

	// 2. The host signing as itself, for a runner that is not its owner's: nobody else's device can be asked.
	own := p.ask(p.host, p.owner.Member.ID, p.mac, envelope.ActionStartApprovedRun, envelope.StartApprovedRun{TaskID: "tsk_1", ApprovalID: "apr_1"}, time.Minute)
	_, err = p.svc.SendMessage(bg, hostActor, own)
	wantErr(t, err, domain.ErrNotFound)

	// 3. A real request, changed after it was signed: every field is covered.
	real := p.open("ttk_1")
	m, err := p.svc.SendMessage(bg, p.phoneA, real)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := p.svc.Inbox(bg, p.macA, 0)
	var e envelope.Envelope
	_ = json.Unmarshal(stored[0].Envelope, &e)
	for name, tamper := range map[string]func(e *envelope.Envelope){
		"another ticket":  func(e *envelope.Envelope) { e.Payload = []byte(`{"projectId":"tpj_1","ticketId":"ttk_EVIL"}`) },
		"another action":  func(e *envelope.Envelope) { e.Action = envelope.ActionCancelRun },
		"later expiry":    func(e *envelope.Envelope) { e.ExpiresAt += 60_000 },
		"another target":  func(e *envelope.Envelope) { e.TargetDeviceID = p.host.DeviceID() },
		"another person":  func(e *envelope.Envelope) { e.UserID = p.owner.Member.ID },
		"another sender":  func(e *envelope.Envelope) { e.DeviceID = p.host.DeviceID() },
		"another message": func(e *envelope.Envelope) { e.MessageID = "msg_other" },
		"another nonce":   func(e *envelope.Envelope) { e.Nonce = strings.Repeat("0", 32) },
		"another signature": func(e *envelope.Envelope) {
			sig, err := base64.RawURLEncoding.DecodeString(e.Signature)
			if err != nil || len(sig) == 0 {
				t.Fatalf("invalid test signature: %v", err)
			}
			sig[0] ^= 1
			e.Signature = base64.RawURLEncoding.EncodeToString(sig)
		},
	} {
		c := e
		tamper(&c)
		if _, err := p.targetVerifies(c); err == nil {
			t.Errorf("a request with %s verified at the Mac", name)
		}
	}
	// The untouched one still does, once.
	if _, err := p.targetVerifies(e); err != nil {
		t.Fatalf("the genuine request does not verify: %v", err)
	}
	_ = m

	// 4. Sending the same signed request twice, or a stale one.
	_, err = p.svc.SendMessage(bg, p.phoneA, real)
	wantErr(t, err, domain.ErrConflict)
	old, err := envelope.Sign(p.phone, envelope.Request{WorkspaceID: p.owner.Workspace.ID, UserID: p.memberID, TargetDeviceID: p.mac.DeviceID(),
		Action: envelope.ActionFetchRunnerStatus, Payload: envelope.FetchRunnerStatus{}}, time.Now().Add(-10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.svc.SendMessage(bg, p.phoneA, old)
	wantErr(t, err, domain.ErrInvalid)
}

func (p *pair) deviceActorFor(t *testing.T, id *localidentity.Identity) Actor {
	t.Helper()
	// The host is the workspace's own first device, which has no credential from enrollment: it speaks through the
	// service's own entry point. A credential is minted for it here, as a joined host's would be, to attempt the same.
	token, hash := domain.NewToken()
	if err := p.db.Update(bg, func(tx store.Tx) error {
		return tx.SetDeviceCredential(bg, p.owner.Workspace.ID, id.DeviceID(), hash, time.Now())
	}); err != nil {
		t.Fatal(err)
	}
	return p.deviceActor(token)
}

// The mailbox is for a person's own devices only.
func TestNobodyCanAskAnotherPersonsDeviceOrAnythingThatIsNotARunner(t *testing.T) {
	p := newPair(t)
	// Cy, another person, with a runner of their own.
	cyPhone := laptop(t, "cy-phone")
	cyMac := laptop(t, "cy-mac")
	r := p.invite(p.owner, EnrollInviteInput{Label: "Cy"})
	respPhone := p.mustJoin(r, cyPhone, "Cy")
	r2 := p.invite(p.owner, EnrollInviteInput{ForMemberID: respPhone.MemberID, Capabilities: []domain.Capability{domain.CapabilityRunner}})
	respMac := p.mustJoin(r2, cyMac, "")
	cyPhoneA, cyMacA := p.deviceActor(respPhone.DeviceToken), p.deviceActor(respMac.DeviceToken)

	// Bo's phone asks Cy's Mac, signing as Bo: Cy's device is not Bo's, so there is no such device for Bo.
	e := p.ask(p.phone, p.memberID, cyMac, envelope.ActionFetchRunnerStatus, envelope.FetchRunnerStatus{}, time.Minute)
	_, err := p.svc.SendMessage(bg, p.phoneA, e)
	wantErr(t, err, domain.ErrNotFound)
	// Bo's phone signing as Cy: not Bo's to say.
	e = p.ask(p.phone, cyPhoneA.Member.ID, cyMac, envelope.ActionFetchRunnerStatus, envelope.FetchRunnerStatus{}, time.Minute)
	_, err = p.svc.SendMessage(bg, p.phoneA, e)
	wantErr(t, err, domain.ErrForbidden)
	// A request signed by Bo's Mac but handed over with the phone's credential: the sender is whoever signed.
	e = p.ask(p.mac, p.memberID, p.phone, envelope.ActionFetchRunnerStatus, envelope.FetchRunnerStatus{}, time.Minute)
	_, err = p.svc.SendMessage(bg, p.phoneA, e)
	wantErr(t, err, domain.ErrForbidden)
	// To a device that is not a runner (Bo's phone has no runner capability): nothing there to open or start.
	e = p.ask(p.mac, p.memberID, p.phone, envelope.ActionFetchRunnerStatus, envelope.FetchRunnerStatus{}, time.Minute)
	_, err = p.svc.SendMessage(bg, p.macA, e)
	wantErr(t, err, domain.ErrConflict)
	// A person's own token, which is not a device's, sends nothing.
	boToken := p.token("Bo")
	e = p.open("ttk_1")
	_, err = p.svc.SendMessage(bg, boToken, e)
	wantErr(t, err, domain.ErrForbidden)
	_ = cyPhoneA

	// Cy's Mac cannot collect or answer what is Bo's.
	m, err := p.svc.SendMessage(bg, p.phoneA, p.open("ttk_1"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := p.svc.Inbox(bg, cyMacA, 0); len(got) != 0 {
		t.Fatalf("another person's runner collected Bo's request: %v", got)
	}
	_, err = p.svc.AckMessage(bg, cyMacA, m.ID, domain.MessageDone, nil)
	wantErr(t, err, domain.ErrNotFound)
	_, err = p.svc.Message(bg, cyPhoneA, m.ID)
	wantErr(t, err, domain.ErrNotFound)
	// A person's own phone may not answer for the Mac either.
	_, err = p.svc.AckMessage(bg, p.phoneA, m.ID, domain.MessageDone, nil)
	wantErr(t, err, domain.ErrNotFound)
	// An answer is small and is an object.
	_, err = p.svc.AckMessage(bg, p.macA, m.ID, domain.MessageDone, json.RawMessage(`"rm -rf /"`))
	wantErr(t, err, domain.ErrInvalid)
	_, err = p.svc.AckMessage(bg, p.macA, m.ID, domain.MessageDone, json.RawMessage(`{"x":"`+strings.Repeat("a", 3000)+`"}`))
	wantErr(t, err, domain.ErrInvalid)
	_, err = p.svc.AckMessage(bg, p.macA, m.ID, domain.MessageState("exploded"), nil)
	wantErr(t, err, domain.ErrInvalid)
}

func (p *pair) token(name string) Actor {
	// Bo has no sign-in token of their own (a device is how they reach the API): an administrator issues one.
	mt, err := p.svc.ReissueToken(bg, p.owner, p.memberID)
	if err != nil {
		p.t.Fatal(err)
	}
	return p.signIn(mt.Token)
}

func TestARequestExpiresAndTheQueueIsBounded(t *testing.T) {
	p := newPair(t)
	e := p.ask(p.phone, p.memberID, p.mac, envelope.ActionFetchRunnerStatus, envelope.FetchRunnerStatus{}, 2*time.Second)
	m, err := p.svc.SendMessage(bg, p.phoneA, e)
	if err != nil {
		t.Fatal(err)
	}
	// After its expiry it is neither collected nor answerable, and the person sees that it expired.
	p.svc.SetClock(func() time.Time { return time.Now().Add(10 * time.Second) })
	if got, _ := p.svc.Inbox(bg, p.macA, 0); len(got) != 0 {
		t.Fatalf("an expired request was handed over: %v", got)
	}
	_, err = p.svc.AckMessage(bg, p.macA, m.ID, domain.MessageDone, nil)
	wantErr(t, err, domain.ErrConflict)
	if seen, _ := p.svc.Message(bg, p.phoneA, m.ID); seen.State != domain.MessageExpired {
		t.Fatalf("state = %s", seen.State)
	}
}

func TestARunnerThatHasNotCaughtUpIsNotBuried(t *testing.T) {
	p := newPair(t)
	for i := 0; i < domain.MaxQueuedPerDevice; i++ {
		if _, err := p.svc.SendMessage(bg, p.phoneA, p.ask(p.phone, p.memberID, p.mac, envelope.ActionFetchRunnerStatus, envelope.FetchRunnerStatus{}, time.Minute)); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	_, err := p.svc.SendMessage(bg, p.phoneA, p.ask(p.phone, p.memberID, p.mac, envelope.ActionFetchRunnerStatus, envelope.FetchRunnerStatus{}, time.Minute))
	wantErr(t, err, domain.ErrBusy)
}

// A request wakes a device that is waiting for one.
func TestARequestWakesADeviceThatIsWaitingForOne(t *testing.T) {
	p := newPair(t)
	got := make(chan []domain.DeviceMessage, 1)
	go func() {
		ms, _ := p.svc.Inbox(bg, p.macA, 10*time.Second)
		got <- ms
	}()
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	if _, err := p.svc.SendMessage(bg, p.phoneA, p.open("ttk_1")); err != nil {
		t.Fatal(err)
	}
	select {
	case ms := <-got:
		if len(ms) != 1 || time.Since(start) > 3*time.Second {
			t.Fatalf("%v after %v", ms, time.Since(start))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a waiting device was not woken")
	}
}

// Revoking a device ends what it can be asked, and what it can ask.
func TestARevokedDeviceCannotBeAskedOrAsk(t *testing.T) {
	p := newPair(t)
	if _, err := p.svc.RevokeDevice(bg, p.owner, p.mac.DeviceID()); err != nil {
		t.Fatal(err)
	}
	_, err := p.svc.SendMessage(bg, p.phoneA, p.open("ttk_1"))
	wantErr(t, err, domain.ErrNotFound)
	if _, err := p.svc.Authenticate(bg, p.macDeviceToken); err == nil {
		t.Fatal("a revoked device still authenticates")
	}
}
