package e2e

import (
	"bytes"
	"crypto/ecdh"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

type store map[PSKID][]byte

func (s store) Lookup(id PSKID) ([]byte, bool) { p, ok := s[id]; return p, ok }
func (s store) Confirm(PSKID)                  {}

func seq(start byte) []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = start + byte(i)
	}
	return b
}

func handshake(t *testing.T) (phone, runner *Session) {
	t.Helper()
	id, _ := NewIdentity()
	psk := NewSecret()
	hello, in, err := Initiate(id.Public(), psk)
	if err != nil {
		t.Fatal(err)
	}
	accept, rs, _, err := Respond(id, store{IDOf(psk): psk}, hello)
	if err != nil {
		t.Fatal(err)
	}
	ps, err := in.Finish(accept)
	if err != nil {
		t.Fatal(err)
	}
	return ps, rs
}

func TestRoundTripBothWays(t *testing.T) {
	p, r := handshake(t)
	for i := 0; i < 3; i++ {
		f, _ := p.Seal([]byte("to runner"))
		got, err := r.Open(f)
		if err != nil || string(got) != "to runner" {
			t.Fatalf("runner open: %v %q", err, got)
		}
		f, _ = r.Seal([]byte("to phone"))
		got, err = p.Open(f)
		if err != nil || string(got) != "to phone" {
			t.Fatalf("phone open: %v %q", err, got)
		}
	}
}

func TestRejects(t *testing.T) {
	p, r := handshake(t)
	f, _ := p.Seal([]byte("x"))
	if _, err := r.Open(f); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Open(f); !errors.Is(err, ErrReplay) {
		t.Fatalf("replay: %v", err)
	}
	g, _ := p.Seal([]byte("y"))
	g[len(g)-1] ^= 1
	if _, err := r.Open(g); !errors.Is(err, ErrAuth) {
		t.Fatalf("tamper: %v", err)
	}
	// A frame meant for the runner is not accepted by the phone (separate directions).
	h, _ := p.Seal([]byte("z"))
	if _, err := p.Open(h); err == nil {
		t.Fatal("reflected frame accepted")
	}
	if _, err := p.Seal(make([]byte, MaxPlaintext+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatal("oversize accepted")
	}
}

func TestWrongSecretOrKey(t *testing.T) {
	id, _ := NewIdentity()
	psk := NewSecret()
	// Unknown secret id.
	hello, _, _ := Initiate(id.Public(), NewSecret())
	if _, _, _, err := Respond(id, store{IDOf(psk): psk}, hello); !errors.Is(err, ErrUnknownPSK) {
		t.Fatalf("unknown psk: %v", err)
	}
	// Same id, different secret on the runner side: keys differ, the accept fails to verify.
	hello, in, _ := Initiate(id.Public(), psk)
	accept, _, _, err := Respond(id, store{IDOf(psk): NewSecret()}, hello)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.Finish(accept); !errors.Is(err, ErrAuth) {
		t.Fatalf("wrong secret: %v", err)
	}
	// An impostor runner (different static key) cannot satisfy the phone.
	other, _ := NewIdentity()
	hello, in, _ = Initiate(id.Public(), psk)
	accept, _, _, err = Respond(other, store{IDOf(psk): psk}, hello)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.Finish(accept); !errors.Is(err, ErrAuth) {
		t.Fatalf("impostor: %v", err)
	}
	if _, _, _, err := Respond(id, store{}, []byte{1, 2, 3}); !errors.Is(err, ErrMalformed) {
		t.Fatalf("malformed: %v", err)
	}
}

type vectors struct {
	RunnerSeed, RunnerPublic, RunnerEphemeralSeed, PhoneEphemeralSeed string
	PSK, PSKID, Hello, Accept                                         string
	Frames                                                            []vectorFrame
}

type vectorFrame struct{ From, Plaintext, Frame string }

// TestVectors pins the wire format. relay/e2e/e2e.test.mjs checks the browser
// implementation against the same file. Regenerate with UPDATE_VECTORS=1 only when the
// protocol version changes on purpose.
func TestVectors(t *testing.T) {
	x := ecdh.X25519()
	runnerPriv, _ := x.NewPrivateKey(seq(1))
	runnerEph, _ := x.NewPrivateKey(seq(40))
	phoneEph, _ := x.NewPrivateKey(seq(80))
	id := &Identity{runnerPriv}
	psk := seq(120)
	h := hex.EncodeToString

	hello, in, err := initiate(phoneEph, id.Public(), psk)
	if err != nil {
		t.Fatal(err)
	}
	accept, rs, pid, err := respond(runnerEph, id, store{IDOf(psk): psk}, hello)
	if err != nil {
		t.Fatal(err)
	}
	ps, err := in.Finish(accept)
	if err != nil {
		t.Fatal(err)
	}
	v := vectors{
		RunnerSeed: h(id.Seed()), RunnerPublic: h(id.Public()),
		RunnerEphemeralSeed: h(runnerEph.Bytes()), PhoneEphemeralSeed: h(phoneEph.Bytes()),
		PSK: h(psk), PSKID: h(pid[:]), Hello: h(hello), Accept: h(accept),
	}
	steps := []struct {
		from string
		pt   string
	}{{"phone", "hello runner"}, {"runner", "hello phone"}, {"phone", ""}, {"runner", "second"}}
	for _, s := range steps {
		src, dst := ps, rs
		if s.from == "runner" {
			src, dst = rs, ps
		}
		f, _ := src.Seal([]byte(s.pt))
		if got, err := dst.Open(f); err != nil || string(got) != s.pt {
			t.Fatalf("step %v: %v", s, err)
		}
		v.Frames = append(v.Frames, vectorFrame{s.from, h([]byte(s.pt)), h(f)})
	}
	got, _ := json.MarshalIndent(v, "", "  ")
	got = append(got, '\n')
	const path = "../../../relay/conformance/e2e-vectors.json"
	if os.Getenv("UPDATE_VECTORS") != "" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no vectors file (run with UPDATE_VECTORS=1): %v", err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("wire format changed; vectors differ (UPDATE_VECTORS=1 if intended)\n%s", got)
	}
}
