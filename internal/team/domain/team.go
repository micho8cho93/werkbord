package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ID prefixes make identifiers self-describing in logs and URLs.
const (
	PrefixWorkspace = "tws"
	PrefixMember    = "tmb"
	PrefixProject   = "tpj"
)

var idEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewID returns a random identifier such as "tpj_k3j9x2m4q7p1a8z5".
func NewID(prefix string) string {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("team: crypto/rand failed: " + err.Error())
	}
	return prefix + "_" + strings.ToLower(idEncoding.EncodeToString(b[:]))
}

// Workspace is a team's shared space: the people and the projects.
type Workspace struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// Member is a person in a workspace. Their token is how they sign in; only its
// hash is kept.
type Member struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	Name        string    `json:"name"`
	Email       string    `json:"email,omitempty"`
	Role        Role      `json:"role"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Can reports whether the member's role allows p.
func (m Member) Can(p Permission) bool { return m.Role.Can(p) }

// Project is something the team works on together. It is a record the team
// shares, not a checkout: Repository is only where the code lives, and each
// member's own Werkbord decides where they have it.
type Project struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Repository  string    `json:"repository,omitempty"`
	Archived    bool      `json:"archived"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ProjectMember records that a member takes part in a project.
type ProjectMember struct {
	ProjectID string    `json:"projectId"`
	MemberID  string    `json:"memberId"`
	AddedBy   string    `json:"addedBy"`
	AddedAt   time.Time `json:"addedAt"`
}

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// ---- tokens ----

// TokenPrefix marks a Werkbord Team token, so a leaked one is recognisable.
const TokenPrefix = "wbt_"

// NewToken returns a new random token (256 bits) and the hash to store.
func NewToken() (token, hash string) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("team: crypto/rand failed: " + err.Error())
	}
	token = TokenPrefix + hex.EncodeToString(b[:])
	return token, HashToken(token)
}

// HashToken is what is stored in place of a token. The tokens are random and
// long, so a plain SHA-256 is enough: there is nothing to guess and nothing to
// make slower.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// ---- validation ----

// Limits on what people may type.
const (
	MaxNameLen        = 80
	MaxDescriptionLen = 2000
	MaxEmailLen       = 254
	MaxRepositoryLen  = 2048
)

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return true
		}
	}
	return false
}

// CleanName trims a name and checks it is usable as a workspace, member or project name.
func CleanName(what, s string) (string, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "", invalid("%s name is required", what)
	case utf8.RuneCountInString(s) > MaxNameLen:
		return "", invalid("%s name is longer than %d characters", what, MaxNameLen)
	case hasControl(s) || strings.ContainsAny(s, "\n\t"):
		return "", invalid("%s name contains a control character", what)
	}
	return s, nil
}

// CleanEmail trims an optional email address. It only rejects what cannot be one;
// whether it is reachable is not Werkbord's business.
func CleanEmail(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	at := strings.IndexByte(s, '@')
	switch {
	case len(s) > MaxEmailLen:
		return "", invalid("email is longer than %d characters", MaxEmailLen)
	case at <= 0 || at == len(s)-1 || strings.ContainsAny(s, " \t\r\n<>,;") || strings.Count(s, "@") != 1:
		return "", invalid("%q is not an email address", s)
	}
	return s, nil
}

// CleanDescription trims an optional description.
func CleanDescription(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > MaxDescriptionLen {
		return "", invalid("description is longer than %d characters", MaxDescriptionLen)
	}
	if hasControl(s) {
		return "", invalid("description contains a control character")
	}
	return s, nil
}

// CleanRepository checks an optional repository address: an https, ssh or git URL,
// or the scp-style git@host:owner/repo. It refuses one with a password or a token
// in it, because the address is shown to everyone on the team and a credential
// has no business in it: each member authenticates to Git with their own.
func CleanRepository(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if len(s) > MaxRepositoryLen || hasControl(s) || strings.ContainsAny(s, " \t\r\n") {
		return "", invalid("repository is not a usable address")
	}
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil || u.Host == "" {
			return "", invalid("repository %q is not a usable address", redact(s))
		}
		switch u.Scheme {
		case "https", "ssh", "git":
		default:
			return "", invalid("repository must be an https, ssh or git address (got %q)", u.Scheme)
		}
		if _, hasPassword := u.User.Password(); hasPassword {
			return "", invalid("repository must not contain a password: each member signs in to Git with their own credentials")
		}
		if u.User != nil && u.Scheme != "ssh" {
			return "", invalid("repository must not contain a user name or token: each member signs in to Git with their own credentials")
		}
		if u.RawQuery != "" || u.Fragment != "" {
			return "", invalid("repository must not have a query or fragment")
		}
		return s, nil
	}
	// scp-style: user@host:path
	at, colon := strings.IndexByte(s, '@'), strings.IndexByte(s, ':')
	if at <= 0 || colon < at+2 || colon == len(s)-1 || strings.Contains(s[:colon], "/") {
		return "", invalid("repository %q is not a usable address", redact(s))
	}
	return s, nil
}

// redact keeps a rejected address from echoing a credential back in an error.
func redact(s string) string {
	if i := strings.Index(s, "://"); i >= 0 {
		if at := strings.LastIndexByte(s, '@'); at > i {
			return s[:i+3] + "…" + s[at:]
		}
	}
	return s
}
