// Command licensing is the vendor's operator console, never a customer binary.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"devboard/cmd/werkbord-team/licensing/operator"
	"golang.org/x/term"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Werkbord licensing:", err)
		os.Exit(1)
	}
}
func readBounded(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil || int64(len(raw)) > max {
		clear(raw)
		return nil, errors.New("file is unreadable or exceeds the size limit")
	}
	return raw, nil
}
func readConfig(path string) (operator.Config, error) {
	var c operator.Config
	raw, err := readBounded(path, 16384)
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	return c, c.Validate()
}
func password(label string) ([]byte, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, errors.New("run this command in Terminal; passphrases must be entered privately, never in arguments or environment variables")
	}
	fmt.Fprint(os.Stderr, label)
	p, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return p, err
}
func unlock(path string, fp string) (ed25519.PrivateKey, error) {
	raw, err := readBounded(path, 32768)
	if err != nil {
		return nil, err
	}
	p, err := password("Issuer recovery passphrase: ")
	if err != nil {
		return nil, err
	}
	defer clear(p)
	plain, err := operator.Decrypt(raw, string(p), 8192)
	if err != nil {
		return nil, err
	}
	defer clear(plain)
	return operator.ParseKey(plain, fp)
}
func writeNew(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		_ = f.Close()
		if !complete {
			_ = os.Remove(path)
		}
	}()
	if _, err = f.Write(raw); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return err
	}
	complete = true
	return nil
}
func run(args []string) error {
	if len(args) == 1 && args[0] == "version" {
		fmt.Println("Werkbord licensing", version)
		return nil
	}
	if len(args) == 0 {
		return errors.New("usage: licensing <serve|protect-key|key-check|backup-check> --config config.json [--key-file issuer.pem.age] [--input file] [--out file]")
	}
	f := flag.NewFlagSet("licensing "+args[0], flag.ContinueOnError)
	configPath := f.String("config", "", "public project and issuer configuration")
	keyPath := f.String("key-file", "", "encrypted age issuer key (no plaintext keys)")
	input := f.String("input", "", "input file")
	out := f.String("out", "", "new output file (never overwritten)")
	listen := f.String("listen", "127.0.0.1:7439", "IPv4 loopback address of the operator console")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected command arguments")
	}
	c, err := readConfig(*configPath)
	if err != nil {
		return err
	}
	switch args[0] {
	case "protect-key":
		raw, err := readBounded(*input, 8192)
		if err != nil {
			return err
		}
		defer clear(raw)
		key, err := operator.ParseKey(raw, c.IssuerFingerprint)
		if err != nil {
			return err
		}
		defer clear(key)
		p, err := password("New recovery passphrase (at least 14 characters): ")
		if err != nil {
			return err
		}
		defer clear(p)
		again, err := password("Repeat recovery passphrase: ")
		if err != nil {
			return err
		}
		defer clear(again)
		if !bytes.Equal(p, again) {
			return errors.New("passphrases did not match")
		}
		encrypted, err := operator.Encrypt(raw, string(p))
		if err != nil {
			return err
		}
		restored, err := operator.Decrypt(encrypted, string(p), 8192)
		if err != nil {
			return err
		}
		defer clear(restored)
		if !bytes.Equal(raw, restored) {
			return errors.New("encrypted key verification failed")
		}
		if err = writeNew(*out, encrypted); err != nil {
			return err
		}
		fmt.Println("Encrypted issuer saved and verified. Fingerprint:", c.IssuerFingerprint)
		fmt.Println("Copy this encrypted file to independent storage and run key-check against that copy. The original key has not been removed.")
		return nil
	case "key-check":
		key, err := unlock(*keyPath, c.IssuerFingerprint)
		if err != nil {
			return err
		}
		defer clear(key)
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"checkedAt": time.Now().UTC(), "fingerprint": c.IssuerFingerprint, "recoveryProof": operator.RecoveryProof(key), "result": "passed"})
	case "backup-check":
		raw, err := readBounded(*input, 50<<20)
		if err != nil {
			return err
		}
		p, err := password("Register backup passphrase: ")
		if err != nil {
			return err
		}
		defer clear(p)
		plain, err := operator.Decrypt(raw, string(p), 50<<20)
		if err != nil {
			return err
		}
		defer clear(plain)
		pub, _ := base64.RawURLEncoding.DecodeString(c.IssuerPublicKey)
		count, err := operator.VerifySnapshot(plain, []ed25519.PublicKey{pub})
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"checkedAt": time.Now().UTC(), "artifacts": count, "encryptedSHA256": operator.Hash(raw), "result": "passed"})
	case "serve":
		var key ed25519.PrivateKey
		if *keyPath != "" {
			key, err = unlock(*keyPath, c.IssuerFingerprint)
			if err != nil {
				return err
			}
			defer clear(key)
		}
		host, _, err := net.SplitHostPort(*listen)
		if err != nil || host != "127.0.0.1" {
			return errors.New("the operator console must bind to 127.0.0.1")
		}
		ln, err := net.Listen("tcp4", *listen)
		if err != nil {
			return err
		}
		defer ln.Close()
		origin := "http://" + ln.Addr().String()
		app, err := operator.NewServer(c, key, origin)
		clear(key)
		if err != nil {
			return err
		}
		defer app.Close()
		srv := &http.Server{Handler: app, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		go func() {
			<-ctx.Done()
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdown)
		}()
		fmt.Println("Werkbord licensing", version, "—", origin)
		if len(key) > 0 {
			fmt.Println("Signing is unlocked for 30 minutes. Closing Terminal locks the issuer.")
		} else {
			fmt.Println("Register access only. Signing requires --key-file and a private Terminal unlock.")
		}
		if err = srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	default:
		return errors.New("unknown licensing command")
	}
}
