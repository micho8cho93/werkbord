package gitrepo

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"werkbord/internal/domain"
)

type SourceBlob struct {
	Path string
	OID  string
	Size int64
}

// SourceInventory reads tree objects, never the checkout. It excludes symlinks
// and submodules by Git mode before a caller applies its source policy.
func (c *CLI) SourceInventory(ctx context.Context, root, commit string) ([]SourceBlob, error) {
	if !hexRev.MatchString(commit) {
		return nil, domain.ErrInvalid
	}
	data, _, err := c.run(ctx, root, runOpts{timeout: 10 * time.Second, maxOut: 8 << 20}, "ls-tree", "-r", "-z", "-l", commit)
	raw := string(data)
	if err != nil {
		return nil, err
	}
	out := []SourceBlob{}
	for _, line := range strings.Split(raw, "\x00") {
		if line == "" {
			continue
		}
		head, p, ok := strings.Cut(line, "\t")
		parts := strings.Fields(head)
		if !ok || len(parts) != 4 {
			return nil, fmt.Errorf("invalid source tree entry")
		}
		if parts[0] != "100644" && parts[0] != "100755" {
			continue
		}
		size, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, SourceBlob{Path: p, OID: parts[2], Size: size})
	}
	return out, nil
}

// SourceBlob reads a validated object ID, not a user-controlled revision/path.
func (c *CLI) SourceBlob(ctx context.Context, root, oid string) (string, error) {
	if !hexRev.MatchString(oid) {
		return "", domain.ErrInvalid
	}
	raw, _, err := c.run(ctx, root, runOpts{timeout: 10 * time.Second, maxOut: 256 << 10}, "cat-file", "blob", oid)
	return string(raw), err
}
