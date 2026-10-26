// Command vendor is the offline tool of the person who sells Werkbord. It is never packaged with the customer application and
// no build links it. It does three things: make the key pair that signs Premium unlock keys (keygen), sign one unlock key
// (premium), and sign a release's Team archive checksums (release). A private key arrives only on standard input as PKCS#8 PEM,
// never as a flag or an environment value, and every file it writes is new and owner-only: it never overwrites one.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"werkbord/internal/entitlement"
)

const usage = `usage:
  vendor keygen  --private-out issuer.pem --public-out issuer.pub
  vendor premium --out unlock-key.txt [--id pk_name] [--holder "Ada Lovelace"] < issuer.pem
  vendor release --input checksums-team.txt --out checksums-team.txt.sig --tag werkbord-vX.Y.Z < release-key.pem`

func main() {
	if err := run(os.Args[1:], os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, "offline issuer:", err)
		os.Exit(1)
	}
}

func run(args []string, keyInput io.Reader) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "keygen":
		return keygen(args[1:])
	case "premium":
		return premium(args[1:], keyInput)
	case "release":
		return release(args[1:], keyInput)
	}
	return errors.New(usage)
}

// keygen makes the Ed25519 pair that signs unlock keys. The private half is written as a PKCS#8 PEM file nobody else can read;
// the public half is the 43 characters the release build is stamped with (PREMIUM_ISSUER_PUBLIC_KEY).
func keygen(args []string) error {
	f := flag.NewFlagSet("keygen", flag.ContinueOnError)
	privateOut := f.String("private-out", "", "new file for the private key (PKCS#8 PEM); keep it offline")
	publicOut := f.String("public-out", "", "new file for the public key (raw URL base64)")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *privateOut == "" || *publicOut == "" || f.NArg() != 0 || *privateOut == *publicOut {
		return errors.New("two different output files are required")
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	defer clear(key)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	defer clear(der)
	if err := writeNew(*privateOut, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})); err != nil {
		return err
	}
	return writeNew(*publicOut, []byte(base64.RawURLEncoding.EncodeToString(pub)+"\n"))
}

// premium signs one unlock key.
func premium(args []string, keyInput io.Reader) error {
	f := flag.NewFlagSet("premium", flag.ContinueOnError)
	output := f.String("out", "", "new file for the unlock key (never overwritten)")
	id := f.String("id", "", "the key's ID, letters, digits, - and _ (a random one if left out)")
	holder := f.String("holder", "", "a label the buyer will recognise their key by; it grants nothing")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *output == "" || f.NArg() != 0 {
		return errors.New("an output file is required")
	}
	if *id == "" {
		b := make([]byte, 12)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		*id = "pk_" + hex.EncodeToString(b)
	}
	key, err := readSigningKey(keyInput)
	if err != nil {
		return err
	}
	defer clear(key)
	text, err := entitlement.Issue(entitlement.Claims{ID: *id, Holder: *holder, IssuedAt: time.Now()}, key)
	if err != nil {
		return err
	}
	return writeNew(*output, []byte(text+"\n"))
}

// release signs the checksums of a release's Team archives.
func release(args []string, keyInput io.Reader) error {
	f := flag.NewFlagSet("release", flag.ContinueOnError)
	input := f.String("input", "", "the checksums file to sign")
	output := f.String("out", "", "new output file (never overwritten)")
	tag := f.String("tag", "", "the release the Team archives belong to (werkbord-vX.Y.Z, the one tag of the whole release)")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *input == "" || *output == "" || f.NArg() != 0 {
		return errors.New("input and output files are required")
	}
	key, err := readSigningKey(keyInput)
	if err != nil {
		return err
	}
	defer clear(key)
	fh, err := os.Open(*input)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(fh, (1<<20)+1))
	_ = fh.Close()
	if err != nil || len(data) > 1<<20 {
		return errors.New("input is unreadable or too large")
	}
	if !strings.HasPrefix(*tag, "werkbord-v") || strings.ContainsAny(*tag, "\r\n\x00 /\\") {
		return errors.New("the release tag (werkbord-vX.Y.Z) is required")
	}
	// The signature binds the archives' checksums to this release and to Team's archives, so it cannot be replayed
	// for another release.
	return writeNew(*output, ed25519.Sign(key, append([]byte("werkbord-team/release/v1\x00"+*tag+"\x00"), data...)))
}

func readSigningKey(keyInput io.Reader) (ed25519.PrivateKey, error) {
	raw, err := io.ReadAll(io.LimitReader(keyInput, 8193))
	if err != nil || len(raw) > 8192 {
		return nil, errors.New("cannot read a PKCS#8 key from standard input")
	}
	defer clear(raw)
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("standard input must be one PKCS#8 private key")
	}
	defer clear(block.Bytes)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("cannot parse the signing key")
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("the signing key must be Ed25519")
	}
	return key, nil
}

// writeNew writes a file that did not exist, readable by its owner only.
func writeNew(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
