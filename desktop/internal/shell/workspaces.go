package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"devboard/desktop/internal/teamlink"
	"devboard/desktop/internal/workspaces"
	"devboard/internal/workspace"
)

// Everything in this file is what the app's own window (the shell page, which the app ships) may ask. The pages of a workspace
// are shown in frames of that window; WebKit gives a frame no way to call the app, and the app's origin check lets nothing
// but the shell page call it, so a workspace's page asks the shell page, which asks Relay, which decides by the kind of
// workspace the frame is and never by anything the page says about itself.

// Registry is what the shell needs of the list of workspaces (desktop/internal/workspaces).
type Registry interface {
	Refresh(ctx context.Context) workspaces.View
	Select(ctx context.Context, id string) (workspaces.View, error)
	Target(ctx context.Context, id, join string) (workspaces.Target, error)
	Overview(ctx context.Context) workspaces.Overview
	Kind(id string) (workspace.Kind, bool)
	Remember(id, place string) error
	Forget(id string)
}

// Team is what the shell asks of Team's service.
type Team interface {
	StatusOf(err error) teamlink.Status
	AddWorkspace(ctx context.Context) (string, error)
	ForgetWorkspace(ctx context.Context, id string) error
	DeliverGrant(ctx context.Context, id, token, base string) error
}

// TeamInstaller is Team's own installer (teamlink.Installer).
type TeamInstaller interface {
	Find() (string, error)
	Activate(ctx context.Context) (teamlink.Result, error)
	Service(ctx context.Context, action string) (teamlink.Result, error)
}

// Grants makes the narrow grant a Team workspace's service is given for the person's own Werkbord.
type Grants interface {
	MintExecutionGrant(ctx context.Context, name string) (token, base string, err error)
}

// Invites holds an invitation the app was opened with until the shell takes it. It is not bound to the window: only the app's
// own code puts a link in.
type Invites struct {
	mu   sync.Mutex
	link string
}

// ValidInvitation says whether link is an invitation link and not something else dressed as one.
func ValidInvitation(link string) bool {
	return strings.HasPrefix(link, "werkbord://join/") && len(link) <= 16384 && !strings.ContainsAny(link, " \r\n\t\"'<>\\")
}

// Receive keeps an invitation. It returns false for anything that is not one.
func (i *Invites) Receive(link string) bool {
	if !ValidInvitation(link) {
		return false
	}
	i.mu.Lock()
	i.link = link
	i.mu.Unlock()
	return true
}

// Take returns the invitation once.
func (i *Invites) Take() string {
	i.mu.Lock()
	defer i.mu.Unlock()
	l := i.link
	i.link = ""
	return l
}

// Peek says whether one is waiting, without taking it.
func (i *Invites) Peek() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.link != ""
}

// WorkspaceView is the switcher: the workspaces, which is open, what is wrong with a service, and whether Team's own
// installer is on this Mac.
type WorkspaceView struct {
	workspaces.View
	Team          teamlink.Status `json:"team"`
	TeamInstaller bool            `json:"teamInstaller"`
	// Invitation: the app was opened with a Team invitation that has not been used yet.
	Invitation bool `json:"invitation"`
}

func (s *Shell) timeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(s.o.Ctx, d)
}

func (s *Shell) teamStatus(v workspaces.View) teamlink.Status {
	for _, p := range v.Problems {
		if p.Source != "Team" {
			continue
		}
		switch p.Kind {
		case "not_running":
			return s.o.Team.StatusOf(workspaces.ErrNotRunning)
		case "refused":
			return s.o.Team.StatusOf(workspaces.ErrRefused)
		case "outdated":
			return s.o.Team.StatusOf(workspaces.ErrOutdated)
		case "not_set_up":
			return s.o.Team.StatusOf(workspaces.ErrNoCredential)
		}
		return teamlink.Status{State: "stopped", Detail: p.Detail}
	}
	return teamlink.Status{State: "ready"}
}

// Workspaces lists the workspaces on this computer and which one to open.
func (s *Shell) Workspaces() WorkspaceView {
	ctx, cancel := s.timeout(8 * time.Second)
	defer cancel()
	v := s.o.Workspaces.Refresh(ctx)
	_, err := s.o.TeamInstaller.Find()
	out := WorkspaceView{View: v, Team: s.teamStatus(v), TeamInstaller: err == nil}
	if s.o.Invites != nil {
		out.Invitation = s.o.Invites.Peek()
	}
	return out
}

// OpenWorkspace makes a workspace the open one and says where to load its page. It changes what the window shows and
// nothing in any workspace: see workspaces.Registry.Select.
func (s *Shell) OpenWorkspace(id string) (workspaces.Target, error) {
	ctx, cancel := s.timeout(8 * time.Second)
	defer cancel()
	if _, err := s.o.Workspaces.Select(ctx, id); err != nil {
		return workspaces.Target{}, err
	}
	return s.o.Workspaces.Target(ctx, id, "")
}

// LoadWorkspace says where to load a workspace's page, without making it the open one. The shell uses it to keep every
// workspace the person has opened alive in its own frame, so that switching away from one stops nothing in it.
func (s *Shell) LoadWorkspace(id string) (workspaces.Target, error) {
	ctx, cancel := s.timeout(8 * time.Second)
	defer cancel()
	return s.o.Workspaces.Target(ctx, id, "")
}

// Overview is what needs the person across every workspace they can open.
func (s *Shell) Overview() workspaces.Overview {
	ctx, cancel := s.timeout(10 * time.Second)
	defer cancel()
	return s.o.Workspaces.Overview(ctx)
}

// RememberPlace records where in a workspace the person is, so the next run opens there. The place is a link inside the
// workspace; anything else is refused.
func (s *Shell) RememberPlace(id, place string) error { return s.o.Workspaces.Remember(id, place) }

// AddTeam gives the person a place to create a Team workspace or join one, and says where to load it. It never installs
// anything: if Team's service is not running it says so, and the shell offers ActivateTeam, which asks first.
func (s *Shell) AddTeam() (workspaces.Target, error) {
	ctx, cancel := s.timeout(30 * time.Second)
	defer cancel()
	id, err := s.o.Team.AddWorkspace(ctx)
	if err != nil {
		return workspaces.Target{}, s.teamError(err)
	}
	join := ""
	if s.o.Invites != nil {
		join = s.o.Invites.Take()
	}
	if _, err := s.o.Workspaces.Select(ctx, id); err != nil {
		// The new slot is not accessible until it exists in the next listing; open it anyway: it is the one being set up.
		s.o.Log.Debug("new workspace not selectable yet", "err", err)
	}
	return s.o.Workspaces.Target(ctx, id, join)
}

func (s *Shell) teamError(err error) error {
	st := s.o.Team.StatusOf(err)
	switch st.State {
	case "not_installed", "stopped", "outdated", "refused":
		return errors.New(st.Detail)
	}
	return err
}

// ActivateTeam sets Team up on this computer, or brings its service up to date, after asking the person in a dialog a web
// page cannot press. It runs Team's own installer; macOS then asks for an administrator's authorization.
func (s *Shell) ActivateTeam() (teamlink.Status, error) {
	exe, err := s.o.TeamInstaller.Find()
	if err != nil {
		return teamlink.Status{State: "not_installed", Detail: err.Error()}, err
	}
	_ = exe
	choice := s.o.UI.Ask(Dialog{
		Kind:  Question,
		Title: "Set up Werkbord Team on this Mac?",
		Message: "Team adds shared workspaces next to your Personal one.\n\n" +
			"It installs a background service, with your administrator password, that runs the network and the shared database for your teams. " +
			"The service never runs your coding agents and never sees your Werkbord's credentials: your agents keep running as you, in your own Werkbord, " +
			"and Team can only ask it to start work you approve.\n\nNothing is installed until you choose Set up.",
		Buttons: []string{"Set up Team", "Not now"}, Default: "Not now", Cancel: "Not now",
	})
	if choice != "Set up Team" {
		return teamlink.Status{State: "not_installed", Detail: "Team was not set up."}, errors.New("Team was not set up")
	}
	s.o.Log.Info("setting up Team on the person's say-so")
	ctx, cancel := s.timeout(4 * time.Minute)
	defer cancel()
	if _, err := s.o.TeamInstaller.Activate(ctx); err != nil {
		s.o.Log.Warn("Team could not be set up", "err", err)
		return teamlink.Status{State: "not_installed", Detail: err.Error()}, err
	}
	return s.teamStatus(s.o.Workspaces.Refresh(ctx)), nil
}

// TeamService starts, stops or removes Team's background service, after asking. Stopping it keeps every workspace and its
// data; removal is refused by the service while any workspace is still joined.
func (s *Shell) TeamService(action string) error {
	var title, msg, yes string
	switch action {
	case "start":
		title, msg, yes = "Start the Team service?", "Your Team workspaces become available again.", "Start Team"
	case "stop":
		title, yes = "Stop the Team service?", "Stop Team"
		msg = "Your Team workspaces become unavailable on this Mac. If this Mac is a Workspace Host or Connectivity Host, your team loses it until you start the service again. Your agents, schedules and Personal workspace are not affected, and nothing is deleted."
	case "uninstall":
		title, yes = "Remove the Team service?", "Remove Team service"
		msg = "Leave your Team workspaces first. This removes the service and its local settings from this Mac; your Personal workspace is not affected. Archives and backups you chose to keep stay where they are."
	default:
		return errors.New("choose start, stop or uninstall")
	}
	if s.o.UI.Ask(Dialog{Kind: Question, Title: title, Message: msg, Buttons: []string{yes, "Cancel"}, Default: "Cancel", Cancel: "Cancel"}) != yes {
		return nil
	}
	ctx, cancel := s.timeout(3 * time.Minute)
	defer cancel()
	_, err := s.o.TeamInstaller.Service(ctx, action)
	return err
}

// ForgetWorkspace removes a Team workspace from this computer's list once it is empty or has been left. Team's service
// refuses a workspace that is still joined.
func (s *Shell) ForgetWorkspace(id string) error {
	if k, ok := s.o.Workspaces.Kind(id); !ok || k != workspace.KindTeam {
		return errors.New("only a Team workspace can be removed from the list")
	}
	ctx, cancel := s.timeout(15 * time.Second)
	defer cancel()
	if err := s.o.Team.ForgetWorkspace(ctx, id); err != nil {
		return err
	}
	s.o.Workspaces.Forget(id)
	return nil
}

// PendingInvitation hands the shell an invitation the app was opened with, once.
func (s *Shell) PendingInvitation() string {
	if s.o.Invites == nil {
		return ""
	}
	return s.o.Invites.Take()
}

// ---- what a workspace's page may ask ----

// relayable is, for each kind of workspace, the only things its page may ask of the app. A Personal page may ask what it
// always could (to open a link in the browser, to choose a folder for a project, to update); a Team page may ask to connect
// the person's runner to its own workspace and to start or stop the Team service. Neither may ask what the other may.
var relayable = map[workspace.Kind]map[string]bool{
	workspace.KindPersonal: {"Info": true, "OpenExternal": true, "ChooseDirectory": true, "RequestUpdate": true, "UpdateStatus": true},
	workspace.KindTeam:     {"Info": true, "OpenExternal": true, "ConnectRunner": true, "TeamService": true, "PendingInvitation": true},
}

// Relay performs, for the page of the workspace id, something that page asked of the app. Which kind of workspace it is
// comes from the app's own list, never from the page; a method that kind may not ask is refused before anything happens.
// Methods that change anything ask the person in a dialog the page cannot press.
func (s *Shell) Relay(id, method string, args []json.RawMessage) (any, error) {
	kind, ok := s.o.Workspaces.Kind(id)
	if !ok {
		return nil, errors.New("that workspace is not on this computer")
	}
	if !relayable[kind][method] {
		s.o.Log.Warn("a page asked for something its kind of workspace may not", "workspace", id, "method", method)
		return nil, fmt.Errorf("%s is not something this workspace may ask of the app", method)
	}
	one := func(v any) error {
		if len(args) != 1 || json.Unmarshal(args[0], v) != nil {
			return errors.New("that call needs exactly one argument of the right kind")
		}
		return nil
	}
	none := func() error {
		if len(args) != 0 {
			return errors.New("that call takes no argument")
		}
		return nil
	}
	switch method {
	case "Info":
		return s.Info(), none()
	case "OpenExternal":
		var u string
		if err := one(&u); err != nil {
			return nil, err
		}
		return nil, s.OpenExternal(u)
	case "ChooseDirectory":
		if err := none(); err != nil {
			return nil, err
		}
		return s.ChooseDirectory()
	case "RequestUpdate":
		if err := none(); err != nil {
			return nil, err
		}
		return s.RequestUpdate(), nil
	case "UpdateStatus":
		var force bool
		if len(args) == 1 {
			if err := one(&force); err != nil {
				return nil, err
			}
		}
		return s.UpdateStatus(force), nil
	case "ConnectRunner":
		if err := none(); err != nil {
			return nil, err
		}
		return nil, s.connectRunner(id)
	case "TeamService":
		var action string
		if err := one(&action); err != nil {
			return nil, err
		}
		return nil, s.TeamService(action)
	case "PendingInvitation":
		if err := none(); err != nil {
			return nil, err
		}
		return s.PendingInvitation(), nil
	}
	return nil, errors.New("unknown request")
}

// connectRunner lets one Team workspace ask the person's own Werkbord to run work the person approves, after asking the
// person. The grant is made for that workspace alone, is named for it, is narrow, and can be revoked in Werkbord's
// settings at any time. Team's service receives the grant and never Werkbord's own credential.
func (s *Shell) connectRunner(id string) error {
	ctx, cancel := s.timeout(30 * time.Second)
	defer cancel()
	v := s.o.Workspaces.Refresh(ctx)
	name := ""
	for _, it := range v.Items {
		if it.ID == id {
			name = it.Name
		}
	}
	if name == "" {
		return errors.New("that workspace is not on this computer")
	}
	// Slot identity keeps identically named teams from revoking each other's grants.
	grantName := "Team:" + strings.TrimPrefix(id, "team:") + ": "
	for _, r := range name {
		if len(grantName)+len(string(r)) > 60 {
			break
		}
		grantName += string(r)
	}
	choice := s.o.UI.Ask(Dialog{
		Kind:  Question,
		Title: fmt.Sprintf("Let %q use your Personal runner?", name),
		Message: "This lets that Team workspace ask your Werkbord to prepare and start work on tickets you hold. It still cannot start anything you have not approved, " +
			"it cannot see your files, Git or sign-ins, and your Werkbord's own approvals and limits apply.\n\nYou can revoke it any time in Werkbord's settings, under Local access.",
		Buttons: []string{"Connect runner", "Cancel"}, Default: "Cancel", Cancel: "Cancel",
	})
	if choice != "Connect runner" {
		return errors.New("the runner was not connected")
	}
	token, base, err := s.o.Grants.MintExecutionGrant(ctx, grantName)
	if err != nil && token == "" {
		return err
	}
	// A leftover older grant that could not be revoked is reported after the new one has been delivered.
	revokeErr := err
	if err := s.o.Team.DeliverGrant(ctx, id, token, base); err != nil {
		return err
	}
	if revokeErr != nil {
		s.o.Log.Warn("an earlier grant remains", "err", revokeErr)
		return revokeErr
	}
	return nil
}
