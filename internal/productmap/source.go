// Package productmap owns local, user-initiated repository analysis. Providers
// receive only bounded source projections; they never receive a repository path.
package productmap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
	"werkbord/internal/gitrepo"
	"werkbord/internal/integration"
)

const MaxFileBytes = 256 << 10
const MaxReadBytes = 8 << 20
const MaxFiles = 20000
const Rules = 1

var credential = regexp.MustCompile(`(?i)(-----BEGIN [A-Z ]*PRIVATE KEY-----|\bsk-(?:ant-|proj-)?[a-zA-Z0-9_-]{20,}|\bAKIA[A-Z0-9]{16}\b|\bgh[pousr]_[a-zA-Z0-9]{30,}|\bgithub_pat_[a-zA-Z0-9_]{30,}|(?:password|api_key|apikey|access_token|client_secret)\s*[:=]\s*["'][^"'\s]{12,}["'])`)

type source struct {
	git    *gitrepo.CLI
	root   string
	files  map[string]gitrepo.SourceBlob
	paths  []string
	read   map[string]int
	seen   map[string]int
	bytes  int
	digest string
}

func openSource(ctx context.Context, g *gitrepo.CLI, root, commit string) (*source, error) {
	blobs, err := g.SourceInventory(ctx, root, commit)
	if err != nil {
		return nil, err
	}
	if len(blobs) > MaxFiles {
		return nil, errors.New("repository exceeds the 20,000-file analysis limit")
	}
	s := &source{git: g, root: root, files: map[string]gitrepo.SourceBlob{}, read: map[string]int{}, seen: map[string]int{}}
	for _, b := range blobs {
		if !integration.ProductSourcePath(b.Path) || b.Size > MaxFileBytes {
			continue
		}
		ext := strings.ToLower(path.Ext(b.Path))
		if strings.Contains("|.png|.jpg|.jpeg|.gif|.webp|.pdf|.zip|.gz|.woff|.woff2|.ttf|.mp4|.sqlite|.db|.lock|.sum|", "|"+ext+"|") {
			continue
		}
		s.files[b.Path] = b
		s.paths = append(s.paths, b.Path)
	}
	sort.Strings(s.paths)
	h := sha256.New()
	for _, p := range s.paths {
		fmt.Fprintf(h, "%s\x00%s\n", p, s.files[p].OID)
	}
	s.digest = hex.EncodeToString(h.Sum(nil))
	return s, nil
}
func (s *source) readFile(ctx context.Context, p string) (string, error) {
	b, ok := s.files[p]
	if !ok {
		return "", errors.New("path is outside the permitted source inventory")
	}
	if s.bytes+int(b.Size) > MaxReadBytes {
		return "", errors.New("source context budget exhausted")
	}
	raw, err := s.git.SourceBlob(ctx, s.root, b.OID)
	if err != nil {
		return "", err
	}
	if len(raw) > MaxFileBytes || !utf8.ValidString(raw) || strings.ContainsRune(raw, 0) || credential.MatchString(raw) {
		return "", errors.New("file is binary, oversized, or contains credential-like material")
	}
	s.bytes += len(raw)
	s.read[p] = strings.Count(raw, "\n") + 1
	s.seen[p] = s.read[p]
	return raw, nil
}
func (s *source) validate(c integration.ProductMapContent) error {
	if err := c.Validate(); err != nil {
		return err
	}
	for _, f := range c.Features {
		for _, e := range f.Evidence {
			n, ok := s.read[e.Path]
			if !ok || e.End > n {
				return fmt.Errorf("feature %s cites an unread or invalid source location", f.ID)
			}
		}
	}
	raw, _ := json.Marshal(c)
	if credential.Match(raw) {
		return errors.New("generated map contains credential-like material")
	}
	return nil
}
