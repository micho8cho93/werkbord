package deviceid

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"
)

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestPublicKeysRoundTripAndBadOnesAreRefused(t *testing.T) {
	pub, _ := newKey(t)
	got, err := ParsePublicKey(EncodePublicKey(pub))
	if err != nil || string(got) != string(pub) {
		t.Fatalf("%v", err)
	}
	for _, bad := range []string{"", "AAAA", "not base64!", EncodePublicKey(pub) + "AA", strings.Repeat("A", 100)} {
		if _, err := ParsePublicKey(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestVerifyRefusesWhatIsNotASignature(t *testing.T) {
	pub, priv := newKey(t)
	sig := ed25519.Sign(priv, []byte("m"))
	if !Verify(pub, []byte("m"), sig) {
		t.Fatal("a good signature failed")
	}
	// Malformed keys and signatures are a false, never a panic.
	for name, ok := range map[string]bool{
		"short key":   Verify(pub[:10], []byte("m"), sig),
		"nil key":     Verify(nil, []byte("m"), sig),
		"short sig":   Verify(pub, []byte("m"), sig[:10]),
		"nil sig":     Verify(pub, []byte("m"), nil),
		"other msg":   Verify(pub, []byte("n"), sig),
		"long sig":    Verify(pub, []byte("m"), append(append([]byte(nil), sig...), 0)),
		"empty both":  Verify(nil, nil, nil),
		"zero length": Verify(pub, nil, sig),
	} {
		if ok {
			t.Errorf("%s verified", name)
		}
	}
}

func TestAPublicRecordIsValidatedWholly(t *testing.T) {
	pub, _ := newKey(t)
	good := Public{ID: "dev_aaaaaaaaaaaaaaaa", Name: "laptop", PublicKey: EncodePublicKey(pub), CreatedAt: time.Now()}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Public){
		"id":   func(p *Public) { p.ID = "x" },
		"name": func(p *Public) { p.Name = "" },
		"key":  func(p *Public) { p.PublicKey = "x" },
	} {
		p := good
		mutate(&p)
		if err := p.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRegistrationStatementsDoNotCollide(t *testing.T) {
	pub, _ := newKey(t)
	// Fields are length-prefixed, so moving a byte from one field to the next changes the statement.
	a := RegistrationStatement("ab", "c", "dev_aaaaaaaaaaaaaaaa", "n", pub)
	b := RegistrationStatement("a", "bc", "dev_aaaaaaaaaaaaaaaa", "n", pub)
	if string(a) == string(b) {
		t.Fatal("two different registrations have the same statement")
	}
}
