// Package devicestate is what the Team daemon remembers on this computer between runs, that is about this computer
// and its owner and not about the workspace: the settings the person chose here, which of their other devices may send
// requests to this one, which tasks were opened here from elsewhere and which of those the person has allowed to be
// started, what has already been done with each request, and the key the app uses to talk to the daemon.
//
// None of it is the workspace's: the workspace holds no copy of it, and a Workspace Host cannot read or change any of it. Each
// file is in one directory only its owner can read, written whole and atomically.
package devicestate

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"devboard/internal/envelope"
)

// RemoteStart says whether this computer's runner may be asked to start work from another of its owner's devices.
type RemoteStart string

const (
	// RemoteStartOff: requests to start a run are always refused here.
	RemoteStartOff RemoteStart = "off"
	// RemoteStartAsk: the person allows each task on this computer first; the request must name that approval. The default.
	RemoteStartAsk RemoteStart = "ask"
	// RemoteStartAuto: a task opened here from one of the person's own trusted devices is allowed to be started, once, for an hour.
	RemoteStartAuto RemoteStart = "auto"
)

// Valid reports whether r is a policy that exists.
func (r RemoteStart) Valid() bool {
	return r == RemoteStartOff || r == RemoteStartAsk || r == RemoteStartAuto
}

// Settings are the choices made on this computer.
type Settings struct {
	// RemoteStart is the policy for starting work from another device. "ask" until the person says otherwise.
	RemoteStart RemoteStart `json:"remoteStart"`
	// RemoteOpen: whether another of the person's trusted devices may open a ticket here (as a task in Werkbord's backlog).
	RemoteOpen bool `json:"remoteOpen"`
	// Form is what the person says this computer is: desktop, laptop, server, or "" for not said.
	Form string `json:"form,omitempty"`
	// WerkbordBase is where Werkbord listens on this computer, when it is not the usual place.
	WerkbordBase string `json:"werkbordBase,omitempty"`
}

// DefaultSettings are what a computer has until its person chooses.
func DefaultSettings() Settings { return Settings{RemoteStart: RemoteStartAsk, RemoteOpen: true} }

// Sender is one of the person's other devices.
type Sender struct {
	DeviceID string `json:"deviceId"`
	Name     string `json:"name"`
	// PublicKey is pinned here when the person trusts the device. A routing Host cannot replace it.
	PublicKey string    `json:"publicKey,omitempty"`
	SeenAt    time.Time `json:"seenAt"`
	// ApprovedAt is when the person allowed it to send requests to this computer; zero while it is waiting for that.
	ApprovedAt time.Time `json:"approvedAt,omitempty"`
}

// Approved reports whether the person has allowed this device.
func (s Sender) Approved() bool { return !s.ApprovedAt.IsZero() }

// Opened is a task a request opened here, from another device.
type Opened struct {
	TaskID    string    `json:"taskId"`
	ProjectID string    `json:"projectId"`
	Ticket    string    `json:"ticket"`
	FromName  string    `json:"fromName"`
	At        time.Time `json:"at"`
}

// Approval is the person's permission, given on this computer, to start one task from another device.
type Approval struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"taskId"`
	ProjectID string    `json:"projectId"`
	Ticket    string    `json:"ticket"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Used      bool      `json:"used"`
}

// Outcome is what was done with a request, kept so that a request handed over twice is answered the same way and done once.
type Outcome struct {
	MessageID string          `json:"messageId"`
	State     string          `json:"state"` // done or refused
	Result    json.RawMessage `json:"result,omitempty"`
	At        time.Time       `json:"at"`
}

// Limits.
const (
	// ApprovalLife is how long an approval to start a task stays good.
	ApprovalLife = time.Hour
	maxOutcomes  = 500
	maxReplays   = 5000
	maxApprovals = 200
	maxOpened    = 100
	maxSenders   = 100
	keepOutcomes = 7 * 24 * time.Hour
)

// Errors a person can be told.
var (
	ErrNoApproval = errors.New("this computer has not approved starting that task")
)

type filedata struct {
	Settings  Settings             `json:"settings"`
	Senders   []Sender             `json:"senders"`
	Opened    []Opened             `json:"opened"`
	Approvals []Approval           `json:"approvals"`
	Outcomes  []Outcome            `json:"outcomes"`
	Replays   map[string]time.Time `json:"replays"`
	LocalKey  string               `json:"localKey"`
}

// State is the daemon's memory on this computer.
type State struct {
	path string
	now  func() time.Time

	mu sync.Mutex
	d  filedata
}

// Open reads (or starts) the state kept in dir.
func Open(dir string) (*State, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	s := &State{path: filepath.Join(dir, "device.json"), now: time.Now}
	b, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		s.d = filedata{Settings: DefaultSettings()}
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, &s.d); err != nil {
			return nil, fmt.Errorf("devicestate: %s is not readable (it is not replaced, so nothing it holds is lost): %w", s.path, err)
		}
		if !s.d.Settings.RemoteStart.Valid() {
			s.d.Settings.RemoteStart = RemoteStartAsk
		}
	}
	if s.d.Replays == nil {
		s.d.Replays = map[string]time.Time{}
	}
	if s.d.LocalKey == "" {
		var raw [32]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return nil, err
		}
		s.d.LocalKey = hex.EncodeToString(raw[:])
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// SetClock is for tests.
func (s *State) SetClock(now func() time.Time) { s.mu.Lock(); s.now = now; s.mu.Unlock() }

func (s *State) saveLocked() error {
	b, err := json.MarshalIndent(s.d, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".device.*")
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return err
	}
	ok = true
	return nil
}

func (s *State) update(fn func(d *filedata) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.d
	// fn works on a copy of the slices it appends to: the previous state is put back if saving fails.
	prev.Senders = append([]Sender(nil), s.d.Senders...)
	prev.Opened = append([]Opened(nil), s.d.Opened...)
	prev.Approvals = append([]Approval(nil), s.d.Approvals...)
	prev.Outcomes = append([]Outcome(nil), s.d.Outcomes...)
	prev.Replays = map[string]time.Time{}
	for k, v := range s.d.Replays {
		prev.Replays[k] = v
	}
	if err := fn(&s.d); err != nil {
		s.d = prev
		return err
	}
	if err := s.saveLocked(); err != nil {
		s.d = prev
		return err
	}
	return nil
}

// ---- the key the app uses to talk to the daemon ----

// LocalKey is the secret the app presents to the daemon on this computer. It is made once and stays in this directory.
func (s *State) LocalKey() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.LocalKey
}

// CheckLocalKey compares a presented key in constant time.
func (s *State) CheckLocalKey(k string) bool {
	return subtle.ConstantTimeCompare([]byte(k), []byte(s.LocalKey())) == 1
}

// ---- settings ----

// Settings returns the choices made here.
func (s *State) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.Settings
}

// SetSettings replaces them, after checking.
func (s *State) SetSettings(v Settings) error {
	if !v.RemoteStart.Valid() {
		return fmt.Errorf("devicestate: starting work from another device is off, ask or auto, not %q", v.RemoteStart)
	}
	switch v.Form {
	case "", "desktop", "laptop", "server":
	default:
		return fmt.Errorf("devicestate: a computer is a desktop, a laptop or a server, not %q", v.Form)
	}
	return s.update(func(d *filedata) error { d.Settings = v; return nil })
}

// ResetWorkspace forgets permissions and request history belonging to a workspace this device left.
// The local desktop credential and this computer's settings remain so its service can be used again.
func (s *State) ResetWorkspace() error {
	return s.update(func(d *filedata) error {
		d.Senders, d.Opened, d.Approvals, d.Outcomes = nil, nil, nil, nil
		d.Replays = map[string]time.Time{}
		return nil
	})
}

// ---- who may send requests here ----

// Senders lists the person's other devices this computer has heard of, approved or waiting.
func (s *State) Senders() []Sender {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Sender(nil), s.d.Senders...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// NoteSender records that the workspace lists another device of this person. It is waiting for the person's say-so until
// Approve is called; noting it again changes nothing but its name.
func (s *State) NoteSender(deviceID, name string) error {
	return s.NoteDevice(deviceID, name, "")
}

// NoteDevice remembers a device's public key. An approved key is never changed by workspace metadata.
func (s *State) NoteDevice(deviceID, name, publicKey string) error {
	return s.update(func(d *filedata) error {
		for i := range d.Senders {
			if d.Senders[i].DeviceID == deviceID {
				d.Senders[i].Name = name
				if !d.Senders[i].Approved() && publicKey != "" {
					d.Senders[i].PublicKey = publicKey
				}
				return nil
			}
		}
		if len(d.Senders) >= maxSenders {
			return errors.New("devicestate: too many devices")
		}
		d.Senders = append(d.Senders, Sender{DeviceID: deviceID, Name: name, PublicKey: publicKey, SeenAt: s.now().UTC()})
		return nil
	})
}

// Approve lets a device of the person's send requests to this computer. Only the person, at this computer, does.
func (s *State) Approve(deviceID string) error {
	return s.update(func(d *filedata) error {
		for i := range d.Senders {
			if d.Senders[i].DeviceID == deviceID {
				d.Senders[i].ApprovedAt = s.now().UTC()
				return nil
			}
		}
		return fmt.Errorf("devicestate: %s is not one of your devices that this computer knows", deviceID)
	})
}

// Revoke stops a device from sending requests here. It is back to waiting.
func (s *State) Revoke(deviceID string) error {
	return s.update(func(d *filedata) error {
		for i := range d.Senders {
			if d.Senders[i].DeviceID == deviceID {
				d.Senders[i].ApprovedAt = time.Time{}
				return nil
			}
		}
		return nil
	})
}

// IsApprovedSender reports whether a device may send requests to this computer.
func (s *State) IsApprovedSender(deviceID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.d.Senders {
		if x.DeviceID == deviceID {
			return x.Approved()
		}
	}
	return false
}

// ---- tasks opened here, and permission to start them ----

// NoteOpened records a task that a request opened here, so the person can see it and allow it.
func (s *State) NoteOpened(o Opened) error {
	return s.update(func(d *filedata) error {
		o.At = s.now().UTC()
		d.Opened = append(d.Opened, o)
		if len(d.Opened) > maxOpened {
			d.Opened = d.Opened[len(d.Opened)-maxOpened:]
		}
		return nil
	})
}

// OpenedTasks lists them, newest first.
func (s *State) OpenedTasks() []Opened {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Opened, 0, len(s.d.Opened))
	for i := len(s.d.Opened) - 1; i >= 0; i-- {
		out = append(out, s.d.Opened[i])
	}
	return out
}

// Allow gives permission, on this computer, to start a task from another device, once, for ApprovalLife.
func (s *State) Allow(taskID, projectID, ticket string) (Approval, error) {
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Approval{}, err
	}
	var a Approval
	err := s.update(func(d *filedata) error {
		now := s.now().UTC()
		a = Approval{ID: "apr_" + hex.EncodeToString(id[:]), TaskID: taskID, ProjectID: projectID, Ticket: ticket, CreatedAt: now, ExpiresAt: now.Add(ApprovalLife)}
		d.Approvals = append(d.Approvals, a)
		if len(d.Approvals) > maxApprovals {
			d.Approvals = d.Approvals[len(d.Approvals)-maxApprovals:]
		}
		return nil
	})
	return a, err
}

// Approvals lists the permissions that are still good, newest first.
func (s *State) Approvals() []Approval {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	var out []Approval
	for i := len(s.d.Approvals) - 1; i >= 0; i-- {
		if a := s.d.Approvals[i]; !a.Used && now.Before(a.ExpiresAt) {
			out = append(out, a)
		}
	}
	return out
}

// Approval returns a permission that is good for this task now, without spending it.
func (s *State) Approval(approvalID, taskID string) (Approval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for _, a := range s.d.Approvals {
		if subtle.ConstantTimeCompare([]byte(a.ID), []byte(approvalID)) == 1 && a.TaskID == taskID && !a.Used && now.Before(a.ExpiresAt) {
			return a, nil
		}
	}
	return Approval{}, ErrNoApproval
}

// Consume spends a permission: it must exist, be for this task, be unused and unexpired. It returns the task's project.
func (s *State) Consume(approvalID, taskID string) (projectID string, err error) {
	err = s.update(func(d *filedata) error {
		now := s.now()
		for i := range d.Approvals {
			a := &d.Approvals[i]
			if subtle.ConstantTimeCompare([]byte(a.ID), []byte(approvalID)) != 1 || a.TaskID != taskID {
				continue
			}
			if a.Used || !now.Before(a.ExpiresAt) {
				return ErrNoApproval
			}
			a.Used = true
			projectID = a.ProjectID
			return nil
		}
		return ErrNoApproval
	})
	return projectID, err
}

// ---- what was done with each request ----

// Outcome returns what was done with a request, if anything was.
func (s *State) Outcome(messageID string) (Outcome, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.d.Outcomes {
		if o.MessageID == messageID {
			return o, true
		}
	}
	return Outcome{}, false
}

// RecordOutcome keeps what was done with a request, and forgets the old.
func (s *State) RecordOutcome(o Outcome) error {
	return s.update(func(d *filedata) error {
		now := s.now().UTC()
		o.At = now
		d.Outcomes = append(d.Outcomes, o)
		kept := d.Outcomes[:0]
		for _, x := range d.Outcomes {
			if now.Sub(x.At) < keepOutcomes {
				kept = append(kept, x)
			}
		}
		d.Outcomes = kept
		if len(d.Outcomes) > maxOutcomes {
			d.Outcomes = d.Outcomes[len(d.Outcomes)-maxOutcomes:]
		}
		return nil
	})
}

// ---- the replay cache ----

var _ envelope.ReplayCache = (*State)(nil)

// errUnchanged ends an update that has nothing to write.
var errUnchanged = errors.New("devicestate: unchanged")

// Seen is envelope.ReplayCache, kept on disk so that a request cannot be replayed after the daemon restarts. Checking and
// recording are one step, so two copies of one request cannot both be new.
func (s *State) Seen(deviceID, messageID, nonce string, expires time.Time) (bool, error) {
	key := deviceID + "\x00" + messageID + "\x00" + nonce
	replayed := false
	err := s.update(func(d *filedata) error {
		now := s.now()
		if exp, ok := d.Replays[key]; ok && exp.After(now) {
			replayed = true
			return errUnchanged
		}
		if len(d.Replays) >= maxReplays {
			for k, exp := range d.Replays {
				if !exp.After(now) {
					delete(d.Replays, k)
				}
			}
			if len(d.Replays) >= maxReplays {
				return envelope.ErrReplayCacheFull
			}
		}
		d.Replays[key] = expires.UTC()
		return nil
	})
	if errors.Is(err, errUnchanged) {
		return replayed, nil
	}
	return false, err
}
