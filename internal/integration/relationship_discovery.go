package integration

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// ManifestRepositories extracts explicit credential-free Git addresses, never
// guesses an identity from a package/module name or contacts a package registry.
func ManifestRepositories(manifest string, raw []byte) ([]string, error) {
	if len(raw) > 1<<20 {
		return nil, errors.New("manifest too large")
	}
	values := []string{}
	switch manifest {
	case "package.json":
		var p struct {
			Dependencies         map[string]string `json:"dependencies"`
			DevDependencies      map[string]string `json:"devDependencies"`
			OptionalDependencies map[string]string `json:"optionalDependencies"`
		}
		if json.Unmarshal(raw, &p) != nil {
			return nil, errors.New("invalid manifest")
		}
		for _, m := range []map[string]string{p.Dependencies, p.DevDependencies, p.OptionalDependencies} {
			for _, v := range m {
				v = strings.TrimPrefix(v, "git+")
				v, _, _ = strings.Cut(v, "#")
				values = append(values, v)
			}
		}
	case "go.mod":
		// Only explicit remote replacement URLs qualify. Ordinary module names do
		// not prove repository identity, including vanity module paths.
		for _, line := range strings.Split(string(raw), "\n") {
			line, _, _ = strings.Cut(line, "// ")
			if _, right, ok := strings.Cut(line, "=>"); ok {
				f := strings.Fields(right)
				if len(f) > 0 {
					values = append(values, f[0])
				}
			}
		}
	default:
		return nil, errors.New("unsupported manifest")
	}
	seen := map[string]bool{}
	out := []string{}
	for _, v := range values {
		if !strings.Contains(v, "://") && !strings.HasPrefix(v, "git@") {
			continue
		}
		if id, e := RepositoryIdentity(v); e == nil && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}
