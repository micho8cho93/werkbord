// Command appcast is the part of publishing the Mac app's updates that has to be checked by something other than
// Sparkle's own tools: whether an update archive's EdDSA signature is the one the app will accept, and whether
// a published appcast says what the release says. It uses nothing but the Go standard library, runs anywhere Go
// does (the release check runs on Linux), and never reads or prints a private key except to make or use one
// for a test.
//
//	appcast keygen <private-key-file>            a new key pair, for tests: writes the private key in the format
//	                                             sign_update --ed-key-file reads, prints the public key
//	appcast sign <private-key-file> <archive>    prints the EdDSA signature of the archive (base64), as sign_update does
//	appcast verify <public-key> <archive> <sig>  exits 0 only if sig is the signature of archive by that key
//	appcast check [flags] <appcast.xml>          whether an appcast is what a release publishes (see below)
//
// check flags:
//
//	-tag werkbord-v1.2.3      the release the appcast is for (required)
//	-base <url>               what every enclosure URL must start with
//	                          (default https://github.com/micho8cho93/werkbord/releases/download/<tag>/)
//	-pubkey <base64>          the public key the signatures must verify with (required with -archive)
//	-archive <file>           the update archive, which must be the one the enclosure describes: its length, and
//	                          a signature that verifies with the key
//	-bundle-version <v>       the CFBundleVersion of the app inside, which must be the appcast item's sparkle:version
//	-loopback                 tests only: accept an http://127.0.0.1 enclosure (a local feed); nothing else but https
//
// It always requires: one item, over https, in the release's own place; an EdDSA signature; the minimum system
// version; and that the feed itself is signed (Sparkle's "SURequireSignedFeed" rejects one that is not).
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/xml"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "appcast:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: appcast keygen|sign|verify|check …")
	}
	switch args[0] {
	case "keygen":
		if len(args) != 2 {
			return fmt.Errorf("usage: appcast keygen <private-key-file>")
		}
		return keygen(args[1])
	case "sign":
		if len(args) != 3 {
			return fmt.Errorf("usage: appcast sign <private-key-file> <archive>")
		}
		return sign(args[1], args[2])
	case "verify":
		if len(args) != 4 {
			return fmt.Errorf("usage: appcast verify <public-key> <archive> <signature>")
		}
		return verify(args[1], args[2], args[3])
	case "check":
		return check(args[1:])
	}
	return fmt.Errorf("unknown command %q", args[0])
}

// Sparkle's private key (what generate_keys -x writes and sign_update --ed-key-file reads) is the base64 of the
// 32-byte Ed25519 seed, and its signatures are the plain Ed25519 ones: checked against sign_update, byte for byte.
func keygen(path string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(priv.Seed())), 0o600); err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
	return nil
}

func readPrivate(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil {
		return nil, fmt.Errorf("%s is not base64", path)
	}
	if len(raw) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s holds %d bytes, not an Ed25519 seed", path, len(raw))
	}
	return ed25519.NewKeyFromSeed(raw), nil
}

func sign(keyFile, archive string) error {
	priv, err := readPrivate(keyFile)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)))
	return nil
}

func publicKey(b64 string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("not an Ed25519 public key (base64 of 32 bytes)")
	}
	return ed25519.PublicKey(raw), nil
}

func verifyBytes(pubB64 string, data []byte, sigB64 string) error {
	pub, err := publicKey(pubB64)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("the signature is not base64 of 64 bytes")
	}
	if !ed25519.Verify(pub, data, sig) {
		return fmt.Errorf("the signature is not this archive's by this key")
	}
	return nil
}

func verify(pubB64, archive, sig string) error {
	data, err := os.ReadFile(archive)
	if err != nil {
		return err
	}
	return verifyBytes(pubB64, data, sig)
}

type appcast struct {
	Channel struct {
		Items []struct {
			Title       string `xml:"title"`
			Version     string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle version"`
			ShortString string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle shortVersionString"`
			MinSystem   string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle minimumSystemVersion"`
			Enclosure   struct {
				URL       string `xml:"url,attr"`
				Length    string `xml:"length,attr"`
				Signature string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle edSignature,attr"`
			} `xml:"enclosure"`
		} `xml:"item"`
	} `xml:"channel"`
}

func check(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	tag := fs.String("tag", "", "the release's tag")
	base := fs.String("base", "", "what the enclosure URL must start with")
	pubkey := fs.String("pubkey", "", "the public key (base64)")
	archive := fs.String("archive", "", "the update archive")
	bundleVersion := fs.String("bundle-version", "", "the app's CFBundleVersion")
	loopback := fs.Bool("loopback", false, "tests only: accept an http://127.0.0.1 enclosure")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || *tag == "" {
		return fmt.Errorf("usage: appcast check -tag <tag> [flags] <appcast.xml>")
	}
	if !strings.HasPrefix(*tag, "werkbord-v") || strings.HasPrefix(*tag, "werkbord-team-v") {
		return fmt.Errorf("%s is not an individual release tag", *tag)
	}
	if *base == "" {
		*base = "https://github.com/micho8cho93/werkbord/releases/download/" + *tag + "/"
	}
	raw, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	var a appcast
	if err := xml.Unmarshal(raw, &a); err != nil {
		return fmt.Errorf("the appcast is not valid XML: %w", err)
	}
	if len(a.Channel.Items) != 1 {
		return fmt.Errorf("the appcast has %d items; a release publishes exactly one (the newest), so that no older item can be offered", len(a.Channel.Items))
	}
	it := a.Channel.Items[0]
	want := strings.TrimPrefix(*tag, "werkbord-v")
	if it.Version != want && it.ShortString != want {
		return fmt.Errorf("the appcast offers version %q (short %q), the release is %s", it.Version, it.ShortString, want)
	}
	if *bundleVersion != "" && it.Version != *bundleVersion {
		return fmt.Errorf("the appcast's sparkle:version is %q but the app's CFBundleVersion is %q: Sparkle would compare the wrong numbers", it.Version, *bundleVersion)
	}
	if !strings.HasPrefix(it.Enclosure.URL, "https://") && !(*loopback && strings.HasPrefix(it.Enclosure.URL, "http://127.0.0.1:")) {
		return fmt.Errorf("the enclosure %q is not https", it.Enclosure.URL)
	}
	if !strings.HasPrefix(it.Enclosure.URL, *base) {
		return fmt.Errorf("the enclosure %q is not under %s", it.Enclosure.URL, *base)
	}
	if !strings.HasSuffix(it.Enclosure.URL, ".zip") {
		return fmt.Errorf("the enclosure %q is not the update archive (.zip)", it.Enclosure.URL)
	}
	if it.Enclosure.Signature == "" {
		return fmt.Errorf("the enclosure has no EdDSA signature: Sparkle would refuse it, and so must we")
	}
	if it.MinSystem == "" {
		return fmt.Errorf("the item has no sparkle:minimumSystemVersion")
	}
	if !strings.Contains(string(raw), "sparkle-signatures") {
		return fmt.Errorf("the appcast itself is not signed (no sparkle-signatures): the app requires a signed feed")
	}
	if n, err := strconv.ParseInt(it.Enclosure.Length, 10, 64); err != nil || n <= 0 {
		return fmt.Errorf("the enclosure has no length")
	}
	if *archive != "" {
		fi, err := os.Stat(*archive)
		if err != nil {
			return err
		}
		if strconv.FormatInt(fi.Size(), 10) != it.Enclosure.Length {
			return fmt.Errorf("the enclosure says %s bytes but %s is %d", it.Enclosure.Length, *archive, fi.Size())
		}
		if *pubkey == "" {
			return fmt.Errorf("-archive needs -pubkey")
		}
		data, err := os.ReadFile(*archive)
		if err != nil {
			return err
		}
		if err := verifyBytes(*pubkey, data, it.Enclosure.Signature); err != nil {
			return fmt.Errorf("the archive does not verify with the app's public key: %w", err)
		}
	}
	fmt.Printf("appcast: ok: %s, one item (%s), %s bytes, signed\n", *tag, it.Version, it.Enclosure.Length)
	return nil
}
