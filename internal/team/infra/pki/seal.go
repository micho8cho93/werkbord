// Package pki is the key material of a Werkbord Team workspace and the code that
// makes and checks it: the workspace's own trust identity, the private network's
// certificate authority and the certificates it issues, a host's own keys, and the
// sealing that moves the authority's secrets from one Workspace Host to another.
//
// There are three families of keys here, and they are never the same key:
//
//   - the workspace key (Ed25519): the identity a device pins when it joins. It
//     signs invitations and the TLS certificates of the bootstrap endpoints. It is
//     Werkbord's own, and has nothing to do with the network;
//   - the network authority's key (Nebula's, via Nebula's own cert package): signs
//     the certificates that let a device onto the private network. It is held by
//     Workspace Hosts only;
//   - a host's own keys: an application identity (Ed25519, how a host is a registered
//     device), a network key (X25519, whose certificate the authority signs) and a
//     sealing key (X25519, to which secrets are encrypted when handed to this host).
//
// Whoever holds the workspace key and the authority's key can speak for the
// workspace and put any device on its network. That is what a Workspace Host is
// trusted with, and why a Workspace Host is a high-trust machine (docs/TEAM_NETWORK.md).
// Nothing here is in an HTTP response: the keys exist on a Workspace Host's disk,
// sealed, and in transit between two of them, encrypted to the receiver.
//
// This package starts no process and opens no connection; it reads and writes files
// only through a Vault.
package pki

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/crypto/argon2"
)

// Sealer protects key material at rest: what it seals can only be opened with what
// the Sealer holds (a key kept apart from the sealed files, or a passphrase). The
// label is bound into the sealed text, so a sealed key cannot be passed off as a
// different one.
type Sealer interface {
	Seal(label string, plaintext []byte) ([]byte, error)
	Open(label string, sealed []byte) ([]byte, error)
}

// ErrCannotOpen: the sealed text is not valid, was not sealed for this label, or the
// wrong key or passphrase was used. It does not say which.
var ErrCannotOpen = errors.New("pki: cannot open sealed key material (wrong key or passphrase, or the file was altered)")

const (
	sealMagic     = "WBK1"
	kdfRawKey     = 0
	kdfArgon2id   = 1
	saltSize      = 16
	nonceSize     = 12
	argonTime     = 3
	argonMemoryKB = 64 * 1024
	argonThreads  = 4
)

// FileSealer seals with a random 256-bit key kept in its own file.
//
// What this gives: the sealed key files, copied alone (a stray backup of the pki
// directory, a screenshot of a listing, a repository committed by mistake), are
// useless. What it does not: someone who can read both files, or run as the user the
// server runs as, has everything; the key file is kept next to the data and is as safe
// as that account. Use a PassphraseSealer, or put the key file on storage that is not
// backed up with the data, when that matters.
type FileSealer struct{ key []byte }

// NewFileSealer loads the key at path, creating it (0600, in a 0700 directory) if it
// does not exist yet.
func NewFileSealer(path string) (*FileSealer, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		b = make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, b); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(b); err != nil {
			f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("pki: %s is not a sealing key (want 32 bytes, found %d)", path, len(b))
	}
	if err := checkPrivateFile(path); err != nil {
		return nil, err
	}
	return &FileSealer{key: b}, nil
}

// Seal implements Sealer.
func (s *FileSealer) Seal(label string, plaintext []byte) ([]byte, error) {
	return seal(kdfRawKey, nil, s.key, label, plaintext)
}

// Open implements Sealer.
func (s *FileSealer) Open(label string, sealed []byte) ([]byte, error) {
	return open(s.key, kdfRawKey, label, sealed, nil)
}

// PassphraseSealer seals with a key derived from a passphrase by Argon2id, with a
// fresh random salt for everything it seals.
type PassphraseSealer struct{ pass []byte }

// NewPassphraseSealer returns a Sealer for a passphrase, which must not be empty.
func NewPassphraseSealer(passphrase []byte) (*PassphraseSealer, error) {
	if len(passphrase) < 8 {
		return nil, errors.New("pki: a passphrase of at least 8 characters is required")
	}
	return &PassphraseSealer{pass: append([]byte(nil), passphrase...)}, nil
}

// Seal implements Sealer.
func (s *PassphraseSealer) Seal(label string, plaintext []byte) ([]byte, error) {
	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	return seal(kdfArgon2id, salt, argon2.IDKey(s.pass, salt, argonTime, argonMemoryKB, argonThreads, 32), label, plaintext)
}

// Open implements Sealer.
func (s *PassphraseSealer) Open(label string, sealed []byte) ([]byte, error) {
	return open(nil, kdfArgon2id, label, sealed, func(salt []byte) []byte {
		return argon2.IDKey(s.pass, salt, argonTime, argonMemoryKB, argonThreads, 32)
	})
}

func header(kdf byte, salt []byte) []byte {
	return append(append([]byte(sealMagic), kdf), salt...)
}

func seal(kdf byte, salt, key []byte, label string, plaintext []byte) ([]byte, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	h := header(kdf, salt)
	out := append(h, nonce...)
	return gcm.Seal(out, nonce, plaintext, append(h, label...)), nil
}

func open(key []byte, kdf byte, label string, sealed []byte, derive func(salt []byte) []byte) ([]byte, error) {
	hl := len(sealMagic) + 1
	if len(sealed) < hl || string(sealed[:len(sealMagic)]) != sealMagic || sealed[len(sealMagic)] != kdf {
		return nil, ErrCannotOpen
	}
	var salt []byte
	if kdf == kdfArgon2id {
		if len(sealed) < hl+saltSize {
			return nil, ErrCannotOpen
		}
		salt = sealed[hl : hl+saltSize]
		hl += saltSize
		key = derive(salt)
	}
	if len(sealed) < hl+nonceSize+16 {
		return nil, ErrCannotOpen
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	h := sealed[:hl]
	pt, err := gcm.Open(nil, sealed[hl:hl+nonceSize], sealed[hl+nonceSize:], append(append([]byte(nil), h...), label...))
	if err != nil {
		return nil, ErrCannotOpen
	}
	return pt, nil
}

// checkPrivateFile refuses a secret file that other users can read or write, on the
// systems where that can be known. It is a check, not a repair: a key that has
// already been readable by others should be treated as exposed, so it says so.
func checkPrivateFile(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("pki: %s is not a regular file", path)
	}
	if fi.Mode().Perm()&0o077 != 0 && runtime.GOOS != "windows" {
		return fmt.Errorf("pki: %s is accessible to other users (mode %o): it must be 0600; treat what it protects as exposed if others could have read it", path, fi.Mode().Perm())
	}
	return nil
}
