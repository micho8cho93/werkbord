package enrollment

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// File names Install writes in the device's network directory.
const (
	FileCA     = "ca.crt"
	FileCert   = "node.crt"
	FileKey    = "node.key"
	FileConfig = "config.yml"
)

// Install writes a device's network credentials into dir: the authority's
// certificate, the device's certificate, its private network key (which it made, and
// which only now is written beside what it signs) and the node's configuration with
// dir filled in. The private key and configuration are readable by the owner only.
// Nothing is written outside dir, and an existing file is replaced atomically.
func Install(dir string, n *NetworkBundle, privateKeyPEM []byte) error {
	if n == nil {
		return errors.New("enrollment: there is nothing to install")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if strings.ContainsAny(abs, "\"\r\n\x00") {
		return fmt.Errorf("enrollment: %q cannot be used as a network directory", abs)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(abs, 0o700); err != nil {
		return err
	}
	cfg := strings.ReplaceAll(n.Config, ConfigDirToken, filepath.ToSlash(abs))
	for _, f := range []struct {
		name string
		data []byte
		mode os.FileMode
	}{
		{FileCA, []byte(n.CACertificate), 0o644},
		{FileCert, []byte(n.NodeCertificate), 0o644},
		{FileKey, privateKeyPEM, 0o600},
		{FileConfig, []byte(cfg), 0o600},
	} {
		if err := writeAtomic(filepath.Join(abs, f.name), f.data, f.mode); err != nil {
			return err
		}
	}
	return nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	return nil
}
