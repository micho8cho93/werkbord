package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// keys makes a key pair and returns the private key file's path and the public key as an app holds it.
func keys(t *testing.T, dir, name string) (file, pub string) {
	t.Helper()
	file = filepath.Join(dir, name)
	out := captureStdout(t, func() {
		if err := run([]string{"keygen", file}); err != nil {
			t.Fatal(err)
		}
	})
	return file, strings.TrimSpace(out)
}

func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = old
	b := make([]byte, 1<<16)
	n, _ := r.Read(b)
	return string(b[:n])
}

func sigOf(t *testing.T, keyFile, archive string) string {
	t.Helper()
	return strings.TrimSpace(captureStdout(t, func() {
		if err := run([]string{"sign", keyFile, archive}); err != nil {
			t.Fatal(err)
		}
	}))
}

const feedTemplate = `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle">
 <channel>
  <title>Werkbord</title>
  <item>
   <title>%s</title>
   <sparkle:version>%s</sparkle:version>
   <sparkle:shortVersionString>%s</sparkle:shortVersionString>
   <sparkle:minimumSystemVersion>%s</sparkle:minimumSystemVersion>
   <enclosure url="%s" length="%d" type="application/octet-stream" sparkle:edSignature="%s"/>
  </item>
  %s
 </channel>
</rss>
%s`

const signedMark = "<!-- sparkle-signatures:\nedSignature: abc\n-->\n"

// feed writes an appcast for version v whose enclosure is url, and an archive that it describes, and returns both paths and the key.
func feed(t *testing.T, v, url string, mutate func(feed *string, archive *[]byte, sig *string)) (appcast, archive, pub string) {
	t.Helper()
	dir := t.TempDir()
	keyFile, pub := keys(t, dir, "key")
	data := []byte("the update archive of " + v)
	archive = filepath.Join(dir, "Werkbord_"+v+"_darwin_universal.zip")
	if err := os.WriteFile(archive, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sig := sigOf(t, keyFile, archive)
	text := fmt.Sprintf(feedTemplate, "Werkbord "+v, v, v, "13.0", url, len(data), sig, "", signedMark)
	if mutate != nil {
		mutate(&text, &data, &sig)
		_ = os.WriteFile(archive, data, 0o600)
	}
	appcast = filepath.Join(dir, "appcast.xml")
	if err := os.WriteFile(appcast, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return appcast, archive, pub
}

const base = "https://github.com/micho8cho93/werkbord/releases/download/werkbord-v1.2.0/"

func runCheck(args ...string) error { return run(append([]string{"check"}, args...)) }

func TestAGoodFeedPasses(t *testing.T) {
	a, z, pub := feed(t, "1.2.0", base+"Werkbord_1.2.0_darwin_universal.zip", nil)
	if err := runCheck("-tag", "werkbord-v1.2.0", "-pubkey", pub, "-archive", z, "-bundle-version", "1.2.0", a); err != nil {
		t.Fatal(err)
	}
}

func TestSignaturesAreStandardEd25519AndOnlyTheRightKeyVerifies(t *testing.T) {
	dir := t.TempDir()
	keyFile, pub := keys(t, dir, "k")
	_, otherPub := keys(t, dir, "other")
	archive := filepath.Join(dir, "a.zip")
	if err := os.WriteFile(archive, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	sig := sigOf(t, keyFile, archive)
	if err := run([]string{"verify", pub, archive, sig}); err != nil {
		t.Fatalf("the key's own signature: %v", err)
	}
	if err := run([]string{"verify", otherPub, archive, sig}); err == nil {
		t.Fatal("another key's verification accepted it")
	}
	if err := os.WriteFile(archive, []byte("hellO"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"verify", pub, archive, sig}); err == nil {
		t.Fatal("a changed archive verified")
	}
	// the private key file is the base64 of a 32-byte seed, which is what Sparkle's own tools read and write
	b, _ := os.ReadFile(keyFile)
	raw, err := base64.StdEncoding.DecodeString(string(b))
	if err != nil || len(raw) != ed25519.SeedSize {
		t.Fatalf("key file: %d bytes, %v", len(raw), err)
	}
}

// A known answer from Sparkle itself: this seed, as `sign_update --ed-key-file` reads it, signs this message (the bytes of
// "hello archive") with exactly this signature. If this ever fails, our signatures are no longer the ones the app verifies.
func TestOurSignaturesAreTheOnesSparklesToolMakes(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "k")
	if err := os.WriteFile(keyFile, []byte("uy8+UNlYLhZlehX+3f8AbxjoCLa3cSJrrgCGAN6ACq8="), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(archive, []byte("hello archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	const sparkles = "g/0WB5sMxEnAsFu3diG/pD7ivgvQTeNEc98HH09NOTZrjii3XuflLjCcYAbWANsDdgGTuj4qA5n+T+5faGRUBQ=="
	if got := sigOf(t, keyFile, archive); got != sparkles {
		t.Fatalf("signature %s, Sparkle's sign_update made %s", got, sparkles)
	}
}

func TestEachWayAFeedCanBeWrongIsRefused(t *testing.T) {
	good := base + "Werkbord_1.2.0_darwin_universal.zip"
	for _, tc := range []struct {
		name, want string
		url        string
		tag        string
		mutate     func(feed *string, archive *[]byte, sig *string)
		bundle     string
	}{
		{"a team tag", "not an individual release tag", good, "werkbord-team-v1.2.0", nil, "1.2.0"},
		{"another version than the release", "the release is 1.2.1", good, "werkbord-v1.2.1", nil, "1.2.0"},
		{"the app says another version", "CFBundleVersion", good, "werkbord-v1.2.0", nil, "1.2.9"},
		{"plain http", "not https", strings.Replace(good, "https://", "http://", 1), "werkbord-v1.2.0", nil, "1.2.0"},
		{"somewhere else", "is not under", "https://example.com/Werkbord_1.2.0_darwin_universal.zip", "werkbord-v1.2.0", nil, "1.2.0"},
		{"another release's directory", "is not under", strings.Replace(good, "werkbord-v1.2.0", "werkbord-v1.1.0", 1), "werkbord-v1.2.0", nil, "1.2.0"},
		{"a disk image instead of the update archive", "not the update archive", base + "Werkbord_1.2.0_darwin_universal.dmg", "werkbord-v1.2.0", nil, "1.2.0"},
		{"no signature on the archive", "no EdDSA signature", good, "werkbord-v1.2.0", func(f *string, _ *[]byte, sig *string) {
			*f = strings.Replace(*f, `sparkle:edSignature="`+*sig+`"`, "", 1)
		}, "1.2.0"},
		{"the feed itself unsigned", "not signed", good, "werkbord-v1.2.0", func(f *string, _ *[]byte, _ *string) { *f = strings.Replace(*f, signedMark, "", 1) }, "1.2.0"},
		{"no minimum system version", "minimumSystemVersion", good, "werkbord-v1.2.0", func(f *string, _ *[]byte, _ *string) {
			*f = strings.Replace(*f, "<sparkle:minimumSystemVersion>13.0</sparkle:minimumSystemVersion>", "", 1)
		}, "1.2.0"},
		{"an archive that is not what was signed", "does not verify", good, "werkbord-v1.2.0", func(_ *string, a *[]byte, _ *string) { (*a)[0] ^= 1 }, "1.2.0"},
		{"an archive of another length", "bytes but", good, "werkbord-v1.2.0", func(_ *string, a *[]byte, _ *string) { *a = append(*a, 'x') }, "1.2.0"},
		{"a second, older item", "2 items", good, "werkbord-v1.2.0", func(f *string, _ *[]byte, _ *string) {
			*f = strings.Replace(*f, "</channel>", `<item><sparkle:version>1.0.0</sparkle:version><enclosure url="https://github.com/micho8cho93/werkbord/releases/download/werkbord-v1.0.0/x.zip" length="1" sparkle:edSignature="x"/></item></channel>`, 1)
		}, "1.2.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, z, pub := feed(t, "1.2.0", tc.url, tc.mutate)
			err := runCheck("-tag", tc.tag, "-pubkey", pub, "-archive", z, "-bundle-version", tc.bundle, a)
			if err == nil {
				t.Fatalf("a feed with %s was accepted", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to say %q", err, tc.want)
			}
		})
	}
}

func TestAnotherKeysSignatureIsRefused(t *testing.T) {
	a, z, _ := feed(t, "1.2.0", base+"Werkbord_1.2.0_darwin_universal.zip", nil)
	_, otherPub := keys(t, t.TempDir(), "other")
	if err := runCheck("-tag", "werkbord-v1.2.0", "-pubkey", otherPub, "-archive", z, a); err == nil || !strings.Contains(err.Error(), "does not verify") {
		t.Fatalf("a feed signed by someone else: %v", err)
	}
}

func TestOnlyTheLoopbackFlagAllowsAPlainHTTPFeedAndOnlyOnThisComputer(t *testing.T) {
	local := "http://127.0.0.1:8123/"
	a, z, pub := feed(t, "1.2.0", local+"Werkbord_1.2.0_darwin_universal.zip", nil)
	if err := runCheck("-tag", "werkbord-v1.2.0", "-base", local, "-pubkey", pub, "-archive", z, a); err == nil {
		t.Fatal("a local http feed passed without the flag")
	}
	if err := runCheck("-tag", "werkbord-v1.2.0", "-base", local, "-pubkey", pub, "-archive", z, "-loopback", a); err != nil {
		t.Fatal(err)
	}
	remote := "http://example.com/"
	a, z, pub = feed(t, "1.2.0", remote+"Werkbord_1.2.0_darwin_universal.zip", nil)
	if err := runCheck("-tag", "werkbord-v1.2.0", "-base", remote, "-pubkey", pub, "-archive", z, "-loopback", a); err == nil {
		t.Fatal("the loopback flag let a feed on another computer be plain http")
	}
}
