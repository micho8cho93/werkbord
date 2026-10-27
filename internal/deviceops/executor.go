// Package deviceops executes the finite device-operation contract on the machine
// owning a repository or terminal. The controller and remote worker use this same
// executor; it owns no board, scheduler, database or network connection.
package deviceops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"werkbord/internal/clientaccess"
	"werkbord/internal/devicewire"
	"werkbord/internal/domain"
	"werkbord/internal/gitrepo"
	"werkbord/internal/terminal"
)

type Executor struct {
	Dir       string
	RunnerID  string
	Terminals *terminal.Manager
	Git       *gitrepo.CLI
	Resolve   func(context.Context, devicewire.Operation) (string, error)
	Authorize func(context.Context, devicewire.Operation) error
	Busy      func(context.Context, string) bool
	Lock      func(context.Context, string) (func(), error)
	Now       func() time.Time
	mu        sync.Mutex
	active    map[string]bool
}
type receipt struct {
	Hash   string            `json:"hash"`
	Result devicewire.Result `json:"result"`
}
type preview struct {
	Actor       string                 `json:"actor"`
	Project     string                 `json:"project"`
	Run         string                 `json:"run"`
	Root        string                 `json:"root"`
	Fingerprint string                 `json:"fingerprint"`
	Snapshot    devicewire.GitSnapshot `json:"snapshot"`
	Used        bool                   `json:"used"`
}

func (e *Executor) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now().UTC()
}
func digest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func safeID(id string) bool {
	if len(id) < 5 || len(id) > 100 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}
func (e *Executor) Run(ctx context.Context, op devicewire.Operation) devicewire.Result {
	result := devicewire.Result{ID: op.ID, State: "failed"}
	if !safeID(op.ID) || op.Request.RunnerID != e.RunnerID || op.Request.Validate() != nil {
		result.Code = "invalid"
		result.Error = "invalid device operation"
		return result
	}
	hash := digest(op)
	path := filepath.Join(e.Dir, "operations", op.ID+".json")
	e.mu.Lock()
	if e.active == nil {
		e.active = map[string]bool{}
	}
	if e.active[op.ID] {
		e.mu.Unlock()
		return devicewire.Result{ID: op.ID, State: "pending"}
	}
	if op.Request.Durable() {
		raw, err := os.ReadFile(path)
		if err == nil {
			var previous receipt
			if json.Unmarshal(raw, &previous) != nil || previous.Hash != hash {
				e.mu.Unlock()
				result.State = "uncertain"
				result.Error = "operation receipt does not match; inspect the execution machine"
				return result
			}
			e.mu.Unlock()
			if previous.Result.State == "executing" {
				previous.Result.State = "uncertain"
				previous.Result.Error = "execution was interrupted; inspect the outcome before issuing another command"
			}
			return previous.Result
		}
		if !errors.Is(err, os.ErrNotExist) {
			e.mu.Unlock()
			result.Error = "cannot read operation receipt"
			return result
		}
	}
	e.active[op.ID] = true
	e.mu.Unlock()
	defer func() { e.mu.Lock(); delete(e.active, op.ID); e.mu.Unlock() }()
	if !e.now().Before(op.ExpiresAt) {
		result.Code = "expired"
		result.Error = "operation expired before execution"
		return result
	}
	if e.Authorize != nil {
		if err := e.Authorize(ctx, op); err != nil {
			result.Code = "forbidden"
			result.Error = "device or project permission ended"
			return result
		}
	}
	if op.Request.Durable() {
		if err := clientaccess.WriteJSON(path, receipt{hash, devicewire.Result{ID: op.ID, State: "executing"}}); err != nil {
			result.Error = "cannot persist execution receipt"
			return result
		}
	}
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	value, err := e.execute(runCtx, op)
	value.ID = op.ID
	if err != nil {
		value.State = "failed"
		value.Code = "refused"
		value.Error = err.Error()
		if len(value.Error) > 1000 {
			value.Error = value.Error[:1000]
		}
	} else {
		value.State = "done"
	}
	raw, _ := json.Marshal(value)
	if len(raw) > devicewire.MaxResultBytes {
		value = devicewire.Result{ID: op.ID, State: "failed", Code: "too_large", Error: "device result exceeded its message budget"}
	}
	if op.Request.Durable() {
		if err := clientaccess.WriteJSON(path, receipt{hash, value}); err != nil {
			return devicewire.Result{ID: op.ID, State: "uncertain", Error: "execution may have completed but its receipt could not be saved; inspect the machine"}
		}
	}
	return value
}
func (e *Executor) execute(ctx context.Context, op devicewire.Operation) (devicewire.Result, error) {
	r := op.Request
	var unlock func()
	if e.Lock != nil && (strings.HasPrefix(r.Kind, "git.") || r.Kind == "terminal.create") {
		release, err := e.Lock(ctx, r.ProjectID)
		if err != nil {
			return devicewire.Result{}, err
		}
		unlock = release
		defer unlock()
	}
	if !e.now().Before(op.ExpiresAt) {
		return devicewire.Result{}, fmt.Errorf("%w: operation expired while waiting for repository access", domain.ErrConflict)
	}
	if e.Authorize != nil {
		if err := e.Authorize(ctx, op); err != nil {
			return devicewire.Result{}, err
		}
	}
	if strings.HasPrefix(r.Kind, "terminal.") {
		return e.terminal(ctx, op)
	}
	if e.Git == nil || e.Resolve == nil {
		return devicewire.Result{}, domain.ErrForbidden
	}
	root, err := e.Resolve(ctx, op)
	if err != nil {
		return devicewire.Result{}, err
	}
	repo, err := e.Git.Inspect(ctx, root)
	if err != nil {
		return devicewire.Result{}, err
	}
	if repo.RootPath != root {
		return devicewire.Result{}, domain.ErrForbidden
	}
	if op.RemoteURL != "" {
		actual := ""
		for _, remote := range repo.Remotes {
			if remote.Name == "origin" {
				actual = remote.URL
			}
		}
		if actual != op.RemoteURL {
			return devicewire.Result{}, fmt.Errorf("%w: repository origin changed", domain.ErrConflict)
		}
	}
	state, err := e.Git.Status(ctx, root)
	if err != nil {
		return devicewire.Result{}, err
	}
	snapshot := devicewire.GitSnapshot{RunnerID: e.RunnerID, ProjectID: r.ProjectID, RunID: r.RunID, Status: *state, Remote: "origin"}
	if r.Kind == "git.status" {
		return devicewire.Result{Git: &snapshot}, nil
	}
	if e.Busy != nil && e.Busy(ctx, r.ProjectID) {
		return devicewire.Result{}, fmt.Errorf("%w: wait for execution on this repository to finish", domain.ErrConflict)
	}
	if state.Detached || state.Head == "" || state.Operation != "" || state.Counts.Conflicted != 0 || state.Truncated {
		return devicewire.Result{}, fmt.Errorf("%w: checkout must have a known branch and no conflicts or unfinished Git operation", domain.ErrConflict)
	}
	if r.Kind == "git.prepare" {
		snapshot.Action = r.Action
		snapshot.Files = append([]string{}, r.Files...)
		sort.Strings(snapshot.Files)
		snapshot.Message = r.Message
		snapshot.ExpiresAt = e.now().Add(5 * time.Minute)
		snapshot.PreviewID = domain.NewID("gp")
		if r.Action == "commit" {
			if len(snapshot.Files) == 0 || strings.TrimSpace(r.Message) == "" {
				return devicewire.Result{}, domain.ErrInvalid
			}
			changed := map[string]bool{}
			for _, list := range [][]domain.GitFileChange{state.Staged, state.Unstaged, state.Untracked} {
				for _, f := range list {
					changed[f.Path] = true
					if f.OldPath != "" {
						changed[f.OldPath] = true
					}
				}
			}
			seen := map[string]bool{}
			for _, f := range snapshot.Files {
				if !changed[f] || seen[f] {
					return devicewire.Result{}, domain.ErrInvalid
				}
				seen[f] = true
			}
		} else if op.RemoteURL == "" {
			return devicewire.Result{}, fmt.Errorf("%w: this repository has no authorised origin", domain.ErrInvalid)
		}
		fingerprint, err := e.Git.WorkingFingerprint(ctx, root, snapshot.Files)
		if err != nil {
			return devicewire.Result{}, err
		}
		p := preview{Actor: op.Actor, Project: r.ProjectID, Run: r.RunID, Root: root, Fingerprint: fingerprint, Snapshot: snapshot}
		if err = clientaccess.WriteJSON(filepath.Join(e.Dir, "previews", snapshot.PreviewID+".json"), p); err != nil {
			return devicewire.Result{}, err
		}
		return devicewire.Result{Git: &snapshot}, nil
	}
	if !safeID(r.PreviewID) {
		return devicewire.Result{}, domain.ErrInvalid
	}
	previewPath := filepath.Join(e.Dir, "previews", r.PreviewID+".json")
	raw, err := os.ReadFile(previewPath)
	if err != nil {
		return devicewire.Result{}, fmt.Errorf("%w: Git confirmation not found", domain.ErrConflict)
	}
	var p preview
	if json.Unmarshal(raw, &p) != nil {
		return devicewire.Result{}, domain.ErrInvalid
	}
	if p.Used || p.Actor != op.Actor || p.Project != r.ProjectID || p.Run != r.RunID || p.Root != root || !e.now().Before(p.Snapshot.ExpiresAt) || r.Kind != "git."+p.Snapshot.Action {
		return devicewire.Result{}, fmt.Errorf("%w: Git confirmation is expired, spent or belongs to another target", domain.ErrConflict)
	}
	fingerprint, err := e.Git.WorkingFingerprint(ctx, root, p.Snapshot.Files)
	if err != nil {
		return devicewire.Result{}, err
	}
	if fingerprint != p.Fingerprint || state.Head != p.Snapshot.Status.Head || state.Branch != p.Snapshot.Status.Branch {
		return devicewire.Result{}, fmt.Errorf("%w: repository changed since confirmation; review it again", domain.ErrConflict)
	}
	p.Used = true
	if err = clientaccess.WriteJSON(previewPath, p); err != nil {
		return devicewire.Result{}, err
	}
	if r.Kind == "git.commit" {
		head, err := e.Git.CommitFiles(ctx, root, p.Snapshot.Status.Head, p.Snapshot.Message, p.Snapshot.Files)
		if err != nil {
			return devicewire.Result{}, err
		}
		snapshot.Status.Head = head
		return devicewire.Result{Git: &snapshot, Outcome: domain.OutcomeDone, Message: "Committed the reviewed files on the owning machine."}, nil
	}
	pushed, err := e.Git.Push(ctx, root, gitrepo.PushRequest{Remote: "origin", Branch: p.Snapshot.Status.Branch, Sha: p.Snapshot.Status.Head})
	if err != nil {
		return devicewire.Result{}, err
	}
	return devicewire.Result{Git: &snapshot, Outcome: pushed.Outcome, Message: pushed.Message}, nil
}
func (e *Executor) terminal(ctx context.Context, op devicewire.Operation) (devicewire.Result, error) {
	r := op.Request
	m := e.Terminals
	if m == nil || !m.Enabled() {
		return devicewire.Result{}, domain.ErrForbidden
	}
	if r.Kind == "terminal.create" {
		if e.Resolve == nil {
			return devicewire.Result{}, domain.ErrForbidden
		}
		if _, err := e.Resolve(ctx, op); err != nil {
			return devicewire.Result{}, err
		}
		meta, err := m.Create(ctx, r.ProjectID, r.AgentID, e.RunnerID, r.Cols, r.Rows)
		return devicewire.Result{Terminal: &meta}, err
	}
	if err := m.Access(ctx, r.ProjectID, r.SessionID, ""); err != nil {
		return devicewire.Result{}, err
	}
	switch r.Kind {
	case "terminal.output":
		out, err := m.OutputNow(ctx, r.ProjectID, r.SessionID, r.After)
		out.Metadata.Phones = nil
		return devicewire.Result{Output: &out}, err
	case "terminal.input":
		return devicewire.Result{}, m.InputOnce(r.ProjectID, r.SessionID, r.Client, r.Sequence, []byte(r.Data), r.Rendered)
	case "terminal.resize":
		return devicewire.Result{}, m.Resize(r.ProjectID, r.SessionID, r.Cols, r.Rows)
	case "terminal.stop":
		return devicewire.Result{}, m.Stop(r.ProjectID, r.SessionID)
	}
	return devicewire.Result{}, domain.ErrInvalid
}
