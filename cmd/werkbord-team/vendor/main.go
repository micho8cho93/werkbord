// Command vendor is an offline issuer. It is never packaged with the customer application.
// Private keys arrive only on standard input as PKCS#8 PEM, never as a flag or environment value.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"devboard/internal/team/license"
)

func main() {
	if err := run(os.Args[1:], os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, "offline issuer:", err)
		os.Exit(1)
	}
}

func run(args []string, keyInput io.Reader) error {
	if len(args) > 0 && args[0] == "verify-desktop-release" {
		f := flag.NewFlagSet("offline release verification", flag.ContinueOnError)
		contents := f.String("contents", "", "reviewed bundle Contents directory")
		version := f.String("version", "", "expected Team build version")
		publicKey := f.String("public-key", "", "independently trusted raw URL-base64 release public key")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if *contents == "" || *version == "" || *publicKey == "" || f.NArg() != 0 {
			return errors.New("contents, version and independently trusted public-key are required")
		}
		return verifyDesktopRelease(*contents, *version, *publicKey)
	}
	if len(args) == 0 || (args[0] != "license" && args[0] != "release" && args[0] != "desktop-release") {
		return errors.New("usage: vendor <license|release|desktop-release> --input <claims.json|checksums.txt> --out <license.json|checksums.txt.sig> [--tag werkbord-team-vX.Y.Z] < private-key.pem")
	}
	f := flag.NewFlagSet("offline issuer", flag.ContinueOnError)
	input := f.String("input", "", "public input document")
	output := f.String("out", "", "new output file (never overwritten)")
	tag := f.String("tag", "", "Team release identity")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if *input == "" || *output == "" || f.NArg() != 0 {
		return errors.New("input and output files are required")
	}
	raw, err := io.ReadAll(io.LimitReader(keyInput, 8193))
	if err != nil || len(raw) > 8192 {
		return errors.New("cannot read a PKCS#8 key from standard input")
	}
	defer clear(raw)
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return errors.New("standard input must be one PKCS#8 private key")
	}
	defer clear(block.Bytes)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return errors.New("cannot parse the signing key")
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return errors.New("the signing key must be Ed25519")
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
	var out []byte
	if args[0] == "license" {
		var claims license.Claims
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if dec.Decode(&claims) != nil || dec.Decode(new(any)) != io.EOF {
			return errors.New("input must be one supported license claims object")
		}
		out, err = license.Issue(claims, key)
		if err != nil {
			return err
		}
		out = append(out, '\n')
	} else {
		if !strings.HasPrefix(*tag, "werkbord-team-v") || strings.ContainsAny(*tag, "\r\n\x00 /\\") {
			return errors.New("a product-specific Team release tag is required")
		}
		domain := "werkbord-team/release/v1"
		if args[0] == "desktop-release" {
			domain = "werkbord-team/desktop-release/v1"
		}
		out = ed25519.Sign(key, append([]byte(domain+"\x00"+*tag+"\x00"), data...))
	}
	file, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(out); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
