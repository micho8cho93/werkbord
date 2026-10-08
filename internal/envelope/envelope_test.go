package envelope

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"devboard/internal/deviceid/localidentity"
)

var t0 = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

// world is a workspace with Ada's laptop (the sender), Ben's runner (the target) and a clock.
type world struct {
	t       *testing.T
	now     time.Time
	ada     *localidentity.Identity
	adaRun  *localidentity.Identity
	ben     *localidentity.Identity
	devices map[string]Device
	replay  *MemoryReplayCache
	v       *Verifier
}

type dir struct{ w *world }

func (d dir) Device(_ context.Context, ws, id string) (Device, error) {
	if dv, ok := d.w.devices[ws+"/"+id]; ok {
		return dv, nil
	}
	return Device{}, ErrUnknownKey
}

func newWorld(t *testing.T) *world {
	w := &world{t: t, now: t0, devices: map[string]Device{}}
	mk := func(name string) *localidentity.Identity {
		i, err := localidentity.New(name, t0)
		if err != nil {
			t.Fatal(err)
		}
		return i
	}
	w.ada, w.adaRun, w.ben = mk("ada laptop"), mk("ada runner"), mk("ben runner")
	reg := func(ws, owner string, i *localidentity.Identity) {
		w.devices[ws+"/"+string(i.ID())] = Device{ID: string(i.ID()), WorkspaceID: ws, OwnerID: owner, PublicKey: i.PublicKey()}
	}
	reg("tws_1", "tmb_ada", w.ada)
	reg("tws_1", "tmb_ada", w.adaRun)
	reg("tws_1", "tmb_ben", w.ben)
	w.replay = &MemoryReplayCache{Now: func() time.Time { return w.now }}
	w.v = &Verifier{Directory: dir{w}, Replay: w.replay, Self: string(w.adaRun.ID()), Now: func() time.Time { return w.now }}
	return w
}

func (w *world) request() Request {
	return Request{WorkspaceID: "tws_1", UserID: "tmb_ada", TargetDeviceID: string(w.adaRun.ID()),
		Action: ActionOpenTicketOnRunner, Payload: OpenTicketOnRunner{ProjectID: "tpj_1", TicketID: "tkt_1"}}
}

func (w *world) sign() Envelope {
	e, err := Sign(w.ada, w.request(), w.now)
	if err != nil {
		w.t.Fatal(err)
	}
	return e
}

func (w *world) verify(e Envelope) (Verified, error) { return w.v.Verify(context.Background(), e) }

func TestASignedEnvelopeVerifiesAndCarriesItsPayload(t *testing.T) {
	w := newWorld(t)
	e := w.sign()
	if e.Protocol != Protocol || e.Version != Version || !strings.HasPrefix(e.MessageID, "msg_") || e.DeviceID != string(w.ada.ID()) {
		t.Fatalf("%+v", e)
	}
	if time.Duration(e.ExpiresAt-e.IssuedAt)*time.Millisecond != DefaultLifetime {
		t.Fatalf("lifetime %v", time.Duration(e.ExpiresAt-e.IssuedAt)*time.Millisecond)
	}
	got, err := w.verify(e)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := got.Payload.(OpenTicketOnRunner); !ok || p.TicketID != "tkt_1" || p.ProjectID != "tpj_1" {
		t.Fatalf("payload = %#v", got.Payload)
	}
	if got.Device.OwnerID != "tmb_ada" {
		t.Fatalf("device = %+v", got.Device)
	}
}

func TestAnEnvelopeSurvivesTheWire(t *testing.T) {
	w := newWorld(t)
	b, err := json.Marshal(w.sign())
	if err != nil {
		t.Fatal(err)
	}
	var e Envelope
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	if _, err := w.verify(e); err != nil {
		t.Fatalf("after a JSON round trip: %v", err)
	}
	// Re-serialising the JSON differently (field order, whitespace) changes nothing that is signed.
	var generic map[string]any
	_ = json.Unmarshal(b, &generic)
	b2, _ := json.MarshalIndent(generic, "", "\t")
	var e2 Envelope
	_ = json.Unmarshal(b2, &e2)
	if string(e.SigningBytes()) != string(e2.SigningBytes()) {
		t.Fatal("signing bytes depend on the JSON text")
	}
}

// Every signed field, changed after signing, is caught.
func TestTamperingWithAnyFieldIsRejected(t *testing.T) {
	other := newWorld(t)
	mutations := map[string]func(e *Envelope){
		"protocol":     func(e *Envelope) { e.Protocol = "other" },
		"version":      func(e *Envelope) { e.Version = 2 },
		"message ID":   func(e *Envelope) { e.MessageID = "msg_other" },
		"workspace":    func(e *Envelope) { e.WorkspaceID = "tws_2" },
		"user":         func(e *Envelope) { e.UserID = "tmb_ben" },
		"sender":       func(e *Envelope) { e.DeviceID = string(other.ben.ID()) },
		"target":       func(e *Envelope) { e.TargetDeviceID = string(other.ben.ID()) },
		"action":       func(e *Envelope) { e.Action = ActionCancelRun },
		"issued":       func(e *Envelope) { e.IssuedAt++ },
		"expiry":       func(e *Envelope) { e.ExpiresAt += 1000 },
		"nonce":        func(e *Envelope) { e.Nonce = strings.Repeat("0", 32) },
		"payload hash": func(e *Envelope) { e.PayloadHash = strings.Repeat("0", 64) },
		"payload":      func(e *Envelope) { e.Payload = []byte(`{"projectId":"tpj_1","ticketId":"tkt_2"}`) },
		"signature":    func(e *Envelope) { e.Signature = flip(e.Signature) },
		"no signature": func(e *Envelope) { e.Signature = "" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			e := w.sign()
			mutate(&e)
			if _, err := w.verify(e); err == nil {
				t.Fatal("a tampered envelope was accepted")
			}
		})
	}
}

func flip(sig string) string {
	b, _ := base64.RawURLEncoding.DecodeString(sig)
	b[0] ^= 1
	return base64.RawURLEncoding.EncodeToString(b)
}

func TestATamperedEnvelopeDoesNotBurnTheGenuineOne(t *testing.T) {
	w := newWorld(t)
	e := w.sign()
	forged := e
	forged.Signature = flip(e.Signature)
	if _, err := w.verify(forged); !errors.Is(err, ErrSignature) {
		t.Fatalf("%v", err)
	}
	if w.replay.Len() != 0 {
		t.Fatal("an unauthenticated envelope was recorded in the replay cache")
	}
	if _, err := w.verify(e); err != nil {
		t.Fatalf("the genuine envelope was refused after a forgery: %v", err)
	}
}

func TestTheWrongSignerIsRejected(t *testing.T) {
	w := newWorld(t)
	// Ben's device signs, claiming to be Ada's laptop.
	e, err := Sign(w.ben, w.request(), w.now)
	if err != nil {
		t.Fatal(err)
	}
	e.DeviceID = string(w.ada.ID())
	if _, err := w.verify(e); !errors.Is(err, ErrSignature) {
		t.Fatalf("a device signed as another: %v", err)
	}
	// Ben's own device, but the envelope claims Ada is the user.
	e, _ = Sign(w.ben, w.request(), w.now)
	if _, err := w.verify(e); !errors.Is(err, ErrWrongOwner) {
		t.Fatalf("a device signed for a user it does not belong to: %v", err)
	}
	// An unknown device.
	stranger, _ := localidentity.New("stranger", t0)
	e, _ = Sign(stranger, w.request(), w.now)
	if _, err := w.verify(e); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("an unregistered device: %v", err)
	}
	// A registered device in another workspace does not vouch for this one.
	req := w.request()
	req.WorkspaceID = "tws_2"
	e, _ = Sign(w.ada, req, w.now)
	if _, err := w.verify(e); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("a device of another workspace: %v", err)
	}
}

func TestExpiryAndClockSkew(t *testing.T) {
	w := newWorld(t)
	e := w.sign() // valid for one minute from t0
	w.now = t0.Add(DefaultLifetime)
	if _, err := w.verify(e); err != nil {
		t.Fatalf("at its expiry: %v", err)
	}
	w.now = t0.Add(DefaultLifetime + DefaultSkew + time.Second)
	if _, err := w.verify(e); !errors.Is(err, ErrExpired) {
		t.Fatalf("after expiry: %v", err)
	}

	// Issued in the future beyond the tolerated skew.
	w = newWorld(t)
	future, _ := Sign(w.ada, w.request(), t0.Add(time.Hour))
	if _, err := w.verify(future); !errors.Is(err, ErrNotYetValid) {
		t.Fatalf("future: %v", err)
	}
	slightly, _ := Sign(w.ada, w.request(), t0.Add(DefaultSkew/2))
	if _, err := w.verify(slightly); err != nil {
		t.Fatalf("within skew: %v", err)
	}

	// An envelope cannot be made to live longer than allowed, by signer or by a verifier that believes one.
	req := w.request()
	req.Lifetime = MaxLifetime + time.Second
	if _, err := Sign(w.ada, req, t0); !errors.Is(err, ErrLifetime) {
		t.Fatalf("Sign: %v", err)
	}
	e = w.sign()
	e.ExpiresAt = e.IssuedAt + int64(24*time.Hour/time.Millisecond)
	e.Signature = encodeSig(w.ada.Sign(e.SigningBytes())) // validly signed by the device itself
	if _, err := w.verify(e); !errors.Is(err, ErrLifetime) {
		t.Fatalf("a day-long envelope: %v", err)
	}
	e = w.sign()
	e.ExpiresAt = e.IssuedAt
	e.Signature = encodeSig(w.ada.Sign(e.SigningBytes()))
	if _, err := w.verify(e); !errors.Is(err, ErrMalformed) {
		t.Fatalf("expiring when issued: %v", err)
	}
}

func TestAReplayedEnvelopeIsRejected(t *testing.T) {
	w := newWorld(t)
	e := w.sign()
	if _, err := w.verify(e); err != nil {
		t.Fatal(err)
	}
	if _, err := w.verify(e); !errors.Is(err, ErrReplay) {
		t.Fatalf("second delivery: %v", err)
	}
	// Even through a different verifier instance sharing the cache (a restarted
	// handler, a second listener).
	v2 := &Verifier{Directory: dir{w}, Replay: w.replay, Self: w.v.Self, Now: w.v.Now}
	if _, err := v2.Verify(context.Background(), e); !errors.Is(err, ErrReplay) {
		t.Fatalf("via a second verifier: %v", err)
	}
	// A new envelope with the same content is a different message.
	if _, err := w.verify(w.sign()); err != nil {
		t.Fatalf("a fresh envelope: %v", err)
	}
	// Once the entry has expired the envelope is expired too, so the cache can forget it safely.
	w.now = t0.Add(DefaultLifetime + DefaultSkew + time.Second)
	if _, err := w.verify(e); !errors.Is(err, ErrExpired) {
		t.Fatalf("after the cache could have forgotten it: %v", err)
	}
}

func TestTheReplayCacheNeverForgetsALiveEnvelopeToMakeRoom(t *testing.T) {
	now := t0
	c := &MemoryReplayCache{Max: 2, Now: func() time.Time { return now }}
	exp := t0.Add(time.Minute)
	for _, id := range []string{"a", "b"} {
		if seen, err := c.Seen("dev", id, "n"+id, exp); seen || err != nil {
			t.Fatal(seen, err)
		}
	}
	if _, err := c.Seen("dev", "c", "nc", exp); !errors.Is(err, ErrReplayCacheFull) {
		t.Fatalf("a full cache evicted a live entry: %v", err)
	}
	if seen, _ := c.Seen("dev", "a", "na", exp); !seen {
		t.Fatal("a was forgotten")
	}
	// Expired entries make room.
	now = exp.Add(time.Second)
	if seen, err := c.Seen("dev", "c", "n", now.Add(time.Minute)); seen || err != nil {
		t.Fatal(seen, err)
	}
	// Entries are per device: another device's identical message ID is not a replay.
	if seen, _ := c.Seen("other", "c", "n", now.Add(time.Minute)); seen {
		t.Fatal("one device's message shadowed another's")
	}
}

func TestARevokedDeviceCannotSign(t *testing.T) {
	w := newWorld(t)
	good := w.sign()
	dv := w.devices["tws_1/"+string(w.ada.ID())]
	dv.Revoked = true
	w.devices["tws_1/"+string(w.ada.ID())] = dv
	if _, err := w.verify(good); !errors.Is(err, ErrRevoked) {
		t.Fatalf("a revoked device's envelope: %v", err)
	}
	// Revocation applies even to an envelope that was signed before it and is still within its lifetime.
	if _, err := w.verify(w.sign()); !errors.Is(err, ErrRevoked) {
		t.Fatalf("%v", err)
	}
}

func TestAnEnvelopeForAnotherDeviceIsNotThisDevicesToActOn(t *testing.T) {
	w := newWorld(t)
	req := w.request()
	req.TargetDeviceID = string(w.ben.ID())
	e, _ := Sign(w.ada, req, w.now)
	if _, err := w.verify(e); !errors.Is(err, ErrWrongTarget) {
		t.Fatalf("%v", err)
	}
	// A relay (no Self) checks the signature and the rest but is not the target.
	relay := &Verifier{Directory: dir{w}, Replay: &MemoryReplayCache{Now: w.v.Now}, Now: w.v.Now}
	if _, err := relay.Verify(context.Background(), e); err != nil {
		t.Fatalf("a relay refused a valid envelope: %v", err)
	}
}

func TestAnActionThatActsOnADeviceMustNameIt(t *testing.T) {
	w := newWorld(t)
	for _, a := range Actions() {
		if !a.NeedsTarget() {
			t.Fatalf("%s: every action defined so far acts on a runner", a)
		}
		p, _ := PayloadFor(a)
		req := Request{WorkspaceID: "tws_1", UserID: "tmb_ada", Action: a, Payload: samplePayload(a)}
		if _, err := Sign(w.ada, req, w.now); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s with no target: %v (payload type %T)", a, err, p)
		}
	}
}

func samplePayload(a Action) any {
	switch a {
	case ActionOpenTicketOnRunner:
		return OpenTicketOnRunner{ProjectID: "p", TicketID: "t"}
	case ActionStartApprovedRun:
		return StartApprovedRun{TaskID: "t", ApprovalID: "a"}
	case ActionCancelRun:
		return CancelRun{RunID: "r"}
	case ActionRespondToAgentQuestion:
		return RespondToAgentQuestion{RunID: "r", QuestionID: "q", OptionID: "o"}
	case ActionFetchRunnerStatus:
		return FetchRunnerStatus{}
	}
	return nil
}

func TestEveryActionRoundTrips(t *testing.T) {
	w := newWorld(t)
	for _, a := range Actions() {
		req := Request{WorkspaceID: "tws_1", UserID: "tmb_ada", TargetDeviceID: string(w.adaRun.ID()), Action: a, Payload: samplePayload(a)}
		e, err := Sign(w.ada, req, w.now)
		if err != nil {
			t.Fatalf("%s: %v", a, err)
		}
		got, err := w.verify(e)
		if err != nil {
			t.Fatalf("%s: %v", a, err)
		}
		if !reflect.DeepEqual(got.Payload, samplePayload(a)) {
			t.Errorf("%s: payload %#v", a, got.Payload)
		}
	}
}

func TestUnknownActionsAndMismatchedPayloadsAreRefused(t *testing.T) {
	w := newWorld(t)
	for _, a := range []Action{"", "execute_command", "run_shell", "OpenTicketOnRunner"} {
		req := w.request()
		req.Action = a
		if _, err := Sign(w.ada, req, w.now); !errors.Is(err, ErrAction) {
			t.Errorf("%q: %v", a, err)
		}
		if Known(a) {
			t.Errorf("%q is known", a)
		}
	}
	// A payload of another action's type.
	req := w.request()
	req.Payload = CancelRun{RunID: "r"}
	if _, err := Sign(w.ada, req, w.now); !errors.Is(err, ErrPayload) {
		t.Errorf("wrong payload type: %v", err)
	}
	req.Payload = map[string]any{"projectId": "p", "ticketId": "t"}
	if _, err := Sign(w.ada, req, w.now); !errors.Is(err, ErrPayload) {
		t.Errorf("an untyped payload: %v", err)
	}
	req.Payload = nil
	if _, err := Sign(w.ada, req, w.now); !errors.Is(err, ErrPayload) {
		t.Errorf("no payload: %v", err)
	}
	// Required fields.
	req.Payload = OpenTicketOnRunner{ProjectID: "p"}
	if _, err := Sign(w.ada, req, w.now); !errors.Is(err, ErrPayload) {
		t.Errorf("a missing field: %v", err)
	}
	// A pointer to the right type is fine.
	req.Payload = &OpenTicketOnRunner{ProjectID: "p", TicketID: "t"}
	if _, err := Sign(w.ada, req, w.now); err != nil {
		t.Errorf("a pointer payload: %v", err)
	}
}

// A device signs whatever it likes, so a verifier still has to check the payload
// it is given is the one the action defines, even when the signature is valid.
func TestAValidlySignedButMalformedPayloadIsRefused(t *testing.T) {
	w := newWorld(t)
	resign := func(e Envelope) Envelope {
		e.Signature = encodeSig(w.ada.Sign(e.SigningBytes()))
		return e
	}
	withPayload := func(raw string) Envelope {
		e := w.sign()
		e.Payload = []byte(raw)
		e.PayloadHash = hashHex(e.Payload)
		return resign(e)
	}
	for name, raw := range map[string]string{
		"an extra field":    `{"projectId":"p","ticketId":"t","command":"rm -rf /"}`,
		"trailing data":     `{"projectId":"p","ticketId":"t"} {}`,
		"a missing field":   `{"projectId":"p"}`,
		"not JSON":          `projectId=p`,
		"the wrong type":    `{"projectId":1,"ticketId":"t"}`,
		"an empty document": ``,
	} {
		if _, err := w.verify(withPayload(raw)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	e := w.sign()
	e.Payload = append([]byte(nil), make([]byte, MaxPayload+1)...)
	e.PayloadHash = hashHex(e.Payload)
	if _, err := w.verify(resign(e)); !errors.Is(err, ErrMalformed) {
		t.Errorf("an oversized payload: %v", err)
	}
	// A different JSON text for the same content has a different hash, so it is a different message.
	good := w.sign()
	spaced := good
	spaced.Payload = []byte(strings.Replace(string(good.Payload), ",", ", ", 1))
	if _, err := w.verify(spaced); !errors.Is(err, ErrPayload) {
		t.Errorf("a payload that does not match its hash: %v", err)
	}
}

func TestMalformedEnvelopesAreRefusedBeforeAnythingElse(t *testing.T) {
	w := newWorld(t)
	for name, mutate := range map[string]func(e *Envelope){
		"no message ID":    func(e *Envelope) { e.MessageID = "" },
		"no workspace":     func(e *Envelope) { e.WorkspaceID = "" },
		"no user":          func(e *Envelope) { e.UserID = "" },
		"no device":        func(e *Envelope) { e.DeviceID = "" },
		"a bad device ID":  func(e *Envelope) { e.DeviceID = "ada" },
		"a bad target":     func(e *Envelope) { e.TargetDeviceID = "ben" },
		"a short nonce":    func(e *Envelope) { e.Nonce = "00" },
		"a non-hex nonce":  func(e *Envelope) { e.Nonce = strings.Repeat("z", 32) },
		"a short hash":     func(e *Envelope) { e.PayloadHash = "00" },
		"a huge identifer": func(e *Envelope) { e.UserID = strings.Repeat("u", 500) },
		"another protocol": func(e *Envelope) { e.Protocol = "x" },
	} {
		e := w.sign()
		mutate(&e)
		if _, err := w.verify(e); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := NewVerifier(nil, w.replay, ""); err == nil {
		t.Error("a verifier without a directory")
	}
	if _, err := NewVerifier(dir{w}, nil, ""); err == nil {
		t.Error("a verifier without a replay cache: expiry alone leaves a replay window")
	}
}

// What the signature covers is fixed, field by field and in order: if a field were
// added to Envelope without being added to SigningBytes, it would be unauthenticated.
func TestEveryEnvelopeFieldIsCoveredBySigningBytes(t *testing.T) {
	w := newWorld(t)
	base := w.sign()
	typ := reflect.TypeOf(base)
	covered := map[string]bool{"Signature": true, "Payload": true} // the signature is not covered by itself; the payload through its hash
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if covered[f.Name] {
			continue
		}
		changed := base
		fv := reflect.ValueOf(&changed).Elem().Field(i)
		switch fv.Kind() {
		case reflect.String:
			fv.SetString(fv.String() + "x")
		case reflect.Int, reflect.Int64:
			fv.SetInt(fv.Int() + 1)
		default:
			t.Fatalf("field %s has a kind the test does not know (%s): decide whether it is signed", f.Name, fv.Kind())
		}
		if string(changed.SigningBytes()) == string(base.SigningBytes()) {
			t.Errorf("changing %s does not change what is signed", f.Name)
		}
	}
}

// An envelope signature cannot be a registration proof or anything else the same
// key signs, and the reverse.
func TestSignaturesAreDomainSeparated(t *testing.T) {
	w := newWorld(t)
	e := w.sign()
	if !strings.HasPrefix(string(e.SigningBytes()), "werkbord/envelope/v1\x00") {
		t.Fatal("no domain prefix")
	}
	proof := w.ada.ProveRegistration("tws_1", "tmb_ada")
	e.Signature = encodeSig(proof)
	if _, err := w.verify(e); !errors.Is(err, ErrSignature) {
		t.Fatalf("a registration proof was accepted as an envelope signature: %v", err)
	}
}

func TestNoPayloadCarriesAnythingExecutable(t *testing.T) {
	forbidden := []string{"command", "cmd", "shell", "script", "exec", "args", "argv", "argument", "env", "environment", "path", "file", "dir", "cwd", "url", "uri", "code", "program", "binary", "sh", "bash", "stdin"}
	seen := 0
	for _, a := range Actions() {
		low := strings.ToLower(string(a))
		for _, w := range []string{"exec", "command", "shell", "script", "run_command", "eval"} {
			if strings.Contains(low, w) {
				t.Errorf("action %q names execution: an action is a meaning, not a way to run something", a)
			}
		}
		p, _ := PayloadFor(a)
		typ := reflect.TypeOf(p).Elem()
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			seen++
			if f.Type.Kind() != reflect.String {
				t.Errorf("%s.%s is a %s: payload fields are plain strings that name things", typ.Name(), f.Name, f.Type.Kind())
			}
			name := strings.ToLower(f.Name)
			for _, bad := range forbidden {
				if name == bad {
					t.Errorf("%s.%s: a payload has no field for %q", typ.Name(), f.Name, bad)
				}
			}
			if f.Name != "Reply" && !strings.HasSuffix(f.Name, "ID") {
				t.Errorf("%s.%s: payload fields are IDs (and the one reply in words)", typ.Name(), f.Name)
			}
		}
	}
	if seen < 8 {
		t.Fatalf("looked at only %d payload fields", seen)
	}
}

func TestEveryActionHasAPayloadAndASample(t *testing.T) {
	if len(Actions()) != len(specs) {
		t.Fatalf("Actions() lists %d, specs has %d", len(Actions()), len(specs))
	}
	for _, a := range Actions() {
		if !Known(a) || samplePayload(a) == nil {
			t.Errorf("%s has no spec or no sample", a)
		}
	}
}

func TestReplyAnswersAreEitherAnOptionOrWords(t *testing.T) {
	for _, p := range []RespondToAgentQuestion{
		{RunID: "r", QuestionID: "q"},
		{RunID: "r", QuestionID: "q", OptionID: "o", Reply: "both"},
		{RunID: "r", QuestionID: "q", Reply: strings.Repeat("x", MaxReply+1)},
		{QuestionID: "q", Reply: "hi"},
	} {
		if _, err := EncodePayload(ActionRespondToAgentQuestion, p); !errors.Is(err, ErrPayload) {
			t.Errorf("%+v: %v", p, err)
		}
	}
	if _, err := EncodePayload(ActionRespondToAgentQuestion, RespondToAgentQuestion{RunID: "r", QuestionID: "q", Reply: "yes, go ahead"}); err != nil {
		t.Error(err)
	}
}

func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
