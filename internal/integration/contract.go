// Package integration defines a product-neutral, versioned local task exchange.
// It contains data and validation only: no credentials, storage or execution.
package integration

import (
	"errors"
	"net"
	"net/url"
	"strings"
	"time"
)

const Schema = "werkbord.integration/v1"

func Identifier(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

type Project struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Remotes []string `json:"remotes"`
}
type Projects struct {
	Schema   string    `json:"schema"`
	Projects []Project `json:"projects"`
}

type Import struct {
	Schema        string   `json:"schema"`
	SourceRef     string   `json:"sourceRef"`
	SourceAliases []string `json:"sourceAliases,omitempty"`
	ProjectID     string   `json:"projectId"`
	Repository    string   `json:"repository"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	WorkBranch    string   `json:"workBranch,omitempty"`
	BaseBranch    string   `json:"baseBranch,omitempty"`
	// Previous is the last imported text. Only untouched, never-run tasks may update.
	Previous *Text `json:"previous,omitempty"`
}
type Text struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	WorkBranch  string `json:"workBranch,omitempty"`
	BaseBranch  string `json:"baseBranch,omitempty"`
}
type Imported struct {
	Schema    string `json:"schema"`
	ProjectID string `json:"projectId"`
	TaskID    string `json:"taskId"`
	Conflict  bool   `json:"conflict"`
}

// CompletionSummary contains measured/fixed facts, never agent-generated text.
type CompletionSummary struct {
	Outcome          string `json:"outcome"`
	ElapsedMillis    *int64 `json:"elapsedMillis,omitempty"`
	HandoffAvailable bool   `json:"handoffAvailable"`
}

// Execution intentionally excludes activity, errors, prompts, questions and handoff text.
type Execution struct {
	RunID            string     `json:"runId,omitempty"`
	State            string     `json:"state"`
	StartedAt        *time.Time `json:"startedAt,omitempty"`
	CompletedAt      *time.Time `json:"completedAt,omitempty"`
	RunnerOnline     bool       `json:"runnerOnline"`
	RunnerAvailable  bool       `json:"runnerAvailable"`
	Branch           string     `json:"branch,omitempty"`
	HeadCommit       string     `json:"headCommit,omitempty"`
	HandoffAvailable bool       `json:"handoffAvailable"`
	// Outcome is a fixed word, never agent-generated text.
	Outcome string             `json:"outcome,omitempty"`
	Summary *CompletionSummary `json:"summary,omitempty"`
}
type Commit struct {
	SHA         string    `json:"sha"`
	CommittedAt time.Time `json:"committedAt"`
}
type PullRequest struct {
	Number     int    `json:"number"`
	URL        string `json:"url"`
	State      string `json:"state"`
	Draft      bool   `json:"draft"`
	BaseBranch string `json:"baseBranch"`
	Mergeable  string `json:"mergeable"`
}
type Git struct {
	Branch      string       `json:"branch"`
	HeadSHA     string       `json:"headSha"`
	BaseBranch  string       `json:"baseBranch"`
	Ahead       int          `json:"ahead"`
	Behind      int          `json:"behind"`
	Commits     []Commit     `json:"commits"`
	PullRequest *PullRequest `json:"pullRequest,omitempty"`
}
type Snapshot struct {
	Schema         string    `json:"schema"`
	Cursor         int64     `json:"cursor"`
	Execution      Execution `json:"execution"`
	Git            *Git      `json:"git,omitempty"`
	GitUnavailable bool      `json:"gitUnavailable"`
}
type Event struct {
	Seq       int64      `json:"seq"`
	Execution *Execution `json:"execution,omitempty"`
}
type Feed struct {
	Schema string  `json:"schema"`
	Cursor int64   `json:"cursor"`
	Reset  bool    `json:"reset"`
	Events []Event `json:"events"`
}

func (e Execution) Valid() bool {
	switch e.State {
	case "queued", "running", "needs_input", "blocked", "completed", "failed", "canceled":
	default:
		return false
	}
	terminal := e.State == "completed" || e.State == "failed" || e.State == "canceled"
	if e.Summary != nil && (!terminal || e.Summary.Outcome != e.State || e.Summary.HandoffAvailable != e.HandoffAvailable || e.Summary.ElapsedMillis != nil && *e.Summary.ElapsedMillis < 0) {
		return false
	}
	return e.Outcome == "" || e.Outcome == e.State && terminal
}

// RepositoryIdentity validates before normalizing. Ports and path case remain significant;
// credentials, traversal, local paths, queries and escaped separators are refused.
func RepositoryIdentity(s string) (string, error) {
	bad := errors.New("repository identity requires a credential-free canonical Git address")
	if s == "" || len(s) > 2000 || strings.ContainsAny(s, " \\\t\r\n?#%") {
		return "", bad
	}
	var host, path, scheme, port string
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil || u.Host == "" || u.Opaque != "" {
			return "", bad
		}
		scheme = u.Scheme
		if scheme != "https" && scheme != "ssh" && scheme != "git" {
			return "", bad
		}
		if u.User != nil {
			if _, pass := u.User.Password(); pass || scheme != "ssh" || u.User.Username() != "git" {
				return "", bad
			}
		}
		host, port, path = u.Hostname(), u.Port(), strings.TrimPrefix(u.Path, "/")
	} else {
		if !strings.HasPrefix(s, "git@") {
			return "", bad
		}
		parts := strings.SplitN(strings.TrimPrefix(s, "git@"), ":", 2)
		if len(parts) != 2 {
			return "", bad
		}
		host, path = parts[0], parts[1]
		scheme = "ssh"
	}
	host = strings.ToLower(host)
	if host == "" || strings.ContainsAny(host, "/@:") && net.ParseIP(host) == nil {
		return "", bad
	}
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	if path == "" {
		return "", bad
	}
	for _, p := range strings.Split(path, "/") {
		if p == "" || p == "." || p == ".." {
			return "", bad
		}
	}
	if port != "" && !(scheme == "https" && port == "443" || scheme == "ssh" && port == "22" || scheme == "git" && port == "9418") {
		host = net.JoinHostPort(host, port)
	}
	return host + "/" + path, nil
}
