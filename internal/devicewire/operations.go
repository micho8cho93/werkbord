// Package devicewire defines finite, semantic device operations carried by the
// existing signed runner heartbeat. No command, executable, credential or path
// to a checkout can be selected by a caller.
package devicewire

import (
	"strings"
	"time"
	"werkbord/internal/domain"
)

const MaxPending = 64
const MaxResultBytes = 128 << 10

type Terminal struct {
	ID         string `json:"id"`
	ProjectID  string `json:"projectId"`
	AgentID    string `json:"agentId"`
	Device     string `json:"device"`
	Repository string `json:"repository"`
	// TaskID associates the terminal with a task of its project. It is optional and changes nothing about the
	// process: an unassociated terminal is a standalone session.
	TaskID    string    `json:"taskId,omitempty"`
	State     string    `json:"state"`
	Phones    []string  `json:"phones,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}
type Output struct {
	Metadata Terminal `json:"session"`
	Data     []byte   `json:"data"`
	Next     uint64   `json:"next"`
	Reset    bool     `json:"reset"`
	Replay   bool     `json:"replay"`
}
type Request struct {
	Confirmed bool     `json:"confirmed,omitempty"`
	RequestID string   `json:"requestId"`
	RunnerID  string   `json:"runnerId"`
	ProjectID string   `json:"projectId"`
	RunID     string   `json:"runId,omitempty"`
	Kind      string   `json:"kind"`
	Action    string   `json:"action,omitempty"` // commit or push, for git.prepare
	PreviewID string   `json:"previewId,omitempty"`
	Files     []string `json:"files,omitempty"`
	Message   string   `json:"message,omitempty"`
	SessionID string   `json:"sessionId,omitempty"`
	AgentID   string   `json:"agentId,omitempty"`
	// TaskID, on terminal.create, associates the new terminal with a task of the project (metadata only).
	TaskID   string `json:"taskId,omitempty"`
	Cols     int    `json:"cols,omitempty"`
	Rows     int    `json:"rows,omitempty"`
	After    uint64 `json:"after,omitempty"`
	Data     string `json:"data,omitempty"`
	Client   string `json:"client,omitempty"`
	Sequence uint64 `json:"sequence,omitempty"`
	Rendered uint64 `json:"rendered,omitempty"`
}
type Operation struct {
	ID        string    `json:"id"`
	Actor     string    `json:"actor"`
	Request   Request   `json:"request"`
	RemoteURL string    `json:"remoteUrl,omitempty"` // supplied by controller, never a client
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type GitSnapshot struct {
	RunnerID  string                `json:"runnerId"`
	ProjectID string                `json:"projectId"`
	RunID     string                `json:"runId,omitempty"`
	Status    domain.GitWorkingTree `json:"status"`
	PreviewID string                `json:"previewId,omitempty"`
	Action    string                `json:"action,omitempty"`
	Files     []string              `json:"files,omitempty"`
	Message   string                `json:"message,omitempty"`
	Remote    string                `json:"remote,omitempty"`
	ExpiresAt time.Time             `json:"expiresAt,omitempty"`
}
type Result struct {
	ID       string       `json:"id"`
	State    string       `json:"state"` // pending, done, failed, uncertain
	Code     string       `json:"code,omitempty"`
	Error    string       `json:"error,omitempty"`
	Terminal *Terminal    `json:"terminal,omitempty"`
	Output   *Output      `json:"output,omitempty"`
	Git      *GitSnapshot `json:"git,omitempty"`
	Outcome  string       `json:"outcome,omitempty"`
	Message  string       `json:"message,omitempty"`
}
type Record struct {
	Operation Operation `json:"operation"`
	Hash      string    `json:"-"`
	Result    Result    `json:"result"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (r Request) Validate() error {
	if len(r.RequestID) < 16 || len(r.RequestID) > 80 || r.ProjectID == "" || r.RunnerID == "" || len(r.Files) > 200 || len(r.Message) > 4096 || len(r.Data) > 16<<10 || len(r.Client) > 80 {
		return domain.ErrInvalid
	}
	for _, c := range r.RequestID {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return domain.ErrInvalid
		}
	}
	for _, path := range r.Files {
		if len(path) > 4096 {
			return domain.ErrInvalid
		}
	}
	switch r.Kind {
	case "git.status", "git.prepare", "git.commit", "git.push", "terminal.create", "terminal.output", "terminal.input", "terminal.resize", "terminal.stop":
	default:
		return domain.ErrInvalid
	}
	if r.Kind == "git.prepare" && r.Action != "commit" && r.Action != "push" {
		return domain.ErrInvalid
	}
	if (r.Kind == "git.commit" || r.Kind == "git.push") && (r.PreviewID == "" || !r.Confirmed) {
		return domain.ErrInvalid
	}
	if strings.HasPrefix(r.Kind, "terminal.") && r.Kind != "terminal.create" && r.SessionID == "" {
		return domain.ErrInvalid
	}
	if r.Kind == "terminal.input" && (len(r.Client) < 16 || r.Sequence == 0 || r.Data == "") {
		return domain.ErrInvalid
	}
	return nil
}
func (r Request) Durable() bool {
	return r.Kind != "git.status" && r.Kind != "terminal.output" && r.Kind != "terminal.resize"
}
