// Offline distribution verification runs in the vendor tool, not a customer backend.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type vendorDesktopManifest struct {
	Tag   string            `json:"tag"`
	Files map[string]string `json:"files"`
}

var vendorReleaseFiles = []string{"Helpers/werkbord-team", "Helpers/nebula", "Helpers/rqlited", "Helpers/werkbord", "Resources/rqlited.build"}

func verifyDesktopRelease(contents, version, encodedKey string) error {
	key, err := base64.RawURLEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return errors.New("invalid offline Team release public key")
	}
	raw, err := os.ReadFile(filepath.Join(contents, "Resources", "team-release.json"))
	if err != nil || len(raw) > 16384 {
		return errors.New("a signed offline Team desktop release manifest is required")
	}
	sig, err := os.ReadFile(filepath.Join(contents, "Resources", "team-release.json.sig"))
	if err != nil {
		return errors.New("the offline Team release signature is missing")
	}
	tag := "werkbord-team-" + version
	if !ed25519.Verify(key, append([]byte("werkbord-team/desktop-release/v1\x00"+tag+"\x00"), raw...), sig) {
		return errors.New("the offline Team release signature is invalid")
	}
	var manifest vendorDesktopManifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&manifest) != nil || dec.Decode(new(any)) != io.EOF || manifest.Tag != tag || len(manifest.Files) != len(vendorReleaseFiles) {
		return errors.New("invalid Team desktop release manifest")
	}
	for _, name := range vendorReleaseFiles {
		want, ok := manifest.Files[name]
		if !ok || len(want) != 64 {
			return fmt.Errorf("missing signed component: %s", name)
		}
		path := filepath.Join(contents, filepath.FromSlash(name))
		fi, err := os.Lstat(path)
		if err != nil || !fi.Mode().IsRegular() {
			return fmt.Errorf("invalid signed component: %s", name)
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil || hex.EncodeToString(h.Sum(nil)) != want {
			return fmt.Errorf("offline Team component checksum mismatch: %s", name)
		}
	}
	return nil
}
