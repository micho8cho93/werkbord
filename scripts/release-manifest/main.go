// Command release-manifest signs offline or verifies a unified checksum manifest.
// Private keys arrive only on stdin and are never generated or printed here.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"werkbord/internal/releaseverify"
)

func run(args []string) error {
	if len(args) == 0 || (args[0] != "sign" && args[0] != "verify") {
		return errors.New("usage: release-manifest sign|verify --tag <tag> --input <checksums> --signature <file> [--key <trusted public PEM>]")
	}
	f := flag.NewFlagSet("release-manifest", flag.ContinueOnError)
	tag := f.String("tag", "", "unified release tag")
	input := f.String("input", "", "checksum manifest")
	signature := f.String("signature", "", "signature file")
	public := f.String("key", "", "independently trusted Ed25519 public PEM")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 || *input == "" || *signature == "" {
		return errors.New("input and signature required")
	}
	fh, err := os.Open(*input)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(fh, releaseverify.MaxManifest+1))
	_ = fh.Close()
	if err != nil {
		return err
	}
	message, err := releaseverify.SigningBytes(*tag, data)
	if err != nil {
		return err
	}
	if args[0] == "verify" {
		p, err := os.ReadFile(*public)
		if err != nil {
			return err
		}
		block, rest := pem.Decode(p)
		if block == nil || block.Type != "PUBLIC KEY" || len(bytes.TrimSpace(rest)) != 0 {
			return errors.New("one trusted public PEM required")
		}
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return err
		}
		key, ok := parsed.(ed25519.PublicKey)
		if !ok {
			return errors.New("Ed25519 key required")
		}
		sig, err := os.ReadFile(*signature)
		if err != nil {
			return err
		}
		_, err = releaseverify.Verify(key, *tag, data, sig)
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 8193))
	if err != nil || len(raw) > 8192 {
		return errors.New("invalid private key input")
	}
	defer clear(raw)
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return errors.New("one PKCS#8 private PEM required on stdin")
	}
	defer clear(block.Bytes)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return err
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return errors.New("Ed25519 private key required")
	}
	defer clear(key)
	out, err := os.OpenFile(*signature, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = out.Write(ed25519.Sign(key, message)); err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "release-manifest:", err)
		os.Exit(1)
	}
}
