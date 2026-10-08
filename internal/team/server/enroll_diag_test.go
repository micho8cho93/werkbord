package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"devboard/internal/enrollment"
	"devboard/internal/team/service"
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// The workspace answers a spent, a mistaken and an expired invitation with the same refusal, so that nobody can probe for
// them. That must not leave its own administrator blind: the host's log says which it was.
func TestTheHostLogsWhyItRefusedAJoinWhileTheJoinerIsToldOneThing(t *testing.T) {
	h, created, _ := newFirstHost(t, "localhost")
	var log syncBuffer
	h.nw.log = slog.New(slog.NewTextHandler(&log, nil))
	h.serve()

	spent := h.invite(created, service.EnrollInviteInput{})
	if _, err := newDevice(t, "first").join(spent.Link, "Pat"); err != nil {
		t.Fatal(err)
	}
	_, err := newDevice(t, "second").join(spent.Link, "Sam")
	if !errors.Is(err, enrollment.ErrRefused) {
		t.Fatalf("reusing an invitation = %v, want ErrRefused", err)
	}
	if strings.Contains(err.Error(), "used") && !strings.Contains(err.Error(), "may have been used") {
		t.Errorf("the joiner was told which way it failed: %v", err)
	}
	if out := log.String(); !strings.Contains(out, "is not usable") || !strings.Contains(out, "used") {
		t.Errorf("the host's log does not say the invitation was spent:\n%s", out)
	}

	guess := h.invite(created, service.EnrollInviteInput{})
	inv, _ := enrollment.Parse(guess.Link, time.Now())
	inv.Credential = strings.Repeat("A", 43)
	d := newDevice(t, "guess")
	if _, err := enrollment.Join(bg, inv, enrollment.JoinParams{Signer: d.id, MemberName: "Guess", DeviceName: "guess", NetworkPublicKeyPEM: d.netPub}); !errors.Is(err, enrollment.ErrRefused) {
		t.Fatalf("a wrong credential = %v, want ErrRefused", err)
	}
	if out := log.String(); !strings.Contains(out, "credential does not match") {
		t.Errorf("the host's log does not say the credential was wrong:\n%s", out)
	}
	if out := log.String(); strings.Contains(out, inv.Credential) {
		t.Error("the log holds a credential")
	}
}

// What the joiner reads has to say what is wrong on their own computer, not "invalid or expired" for everything.
func TestTheJoinerIsToldWhetherTheClockTheLinkOrTheInvitationIsWrong(t *testing.T) {
	h, created, _ := newFirstHost(t, "localhost")
	h.serve()
	d := testDaemon(t)

	post := func(link string) string {
		in, _ := json.Marshal(map[string]string{"link": link})
		w := daemonCall(d, "POST", "/api/device/v1/invitation", string(in))
		if w.Code == 200 {
			return "ok"
		}
		var eb struct {
			Error struct{ Message string } `json:"error"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &eb)
		return eb.Error.Message
	}
	fresh := h.invite(created, service.EnrollInviteInput{})
	if got := post(fresh.Link); got != "ok" {
		t.Fatalf("a fresh invitation = %s", got)
	}
	if got := post(fresh.Link[:len(fresh.Link)-5]); !strings.Contains(got, "part of it is missing") {
		t.Errorf("a link with its end missing = %s", got)
	}
	if got := post("hello"); !strings.Contains(got, "not a Werkbord Team invitation") {
		t.Errorf("something that is not a link = %s", got)
	}

	// The computer that made it runs ten minutes ahead of this one.
	h.svc.SetClock(func() time.Time { return time.Now().Add(10 * time.Minute) })
	ahead := h.invite(created, service.EnrollInviteInput{})
	h.svc.SetClock(nil)
	if got := post(ahead.Link); !strings.Contains(got, "clocks disagree") || !strings.Contains(got, "Date & Time") {
		t.Errorf("an invitation from a clock ten minutes ahead = %s", got)
	}
	// ...but a few seconds is nothing.
	h.svc.SetClock(func() time.Time { return time.Now().Add(40 * time.Second) })
	near := h.invite(created, service.EnrollInviteInput{})
	h.svc.SetClock(nil)
	if got := post(near.Link); got != "ok" {
		t.Errorf("an invitation from a clock 40 seconds ahead = %s", got)
	}

	short := h.invite(created, service.EnrollInviteInput{TTL: time.Minute})
	inv, _ := enrollment.Parse(short.Link, time.Now())
	msg := invitationProblem(inv, enrollment.ErrExpired, time.Now().Add(time.Hour)).Error()
	if !strings.Contains(msg, "ended on") || !strings.Contains(msg, "Date & Time") {
		t.Errorf("an expired invitation = %s", msg)
	}
	if got := invitationProblem(inv, enrollment.ErrRefused, time.Now()).Error(); !strings.Contains(got, "works once") {
		t.Errorf("a refused invitation = %s", got)
	}
}
