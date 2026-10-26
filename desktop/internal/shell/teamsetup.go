package shell

import (
	"errors"
	"os"
	"os/user"
	"strings"
	"time"
	"unicode/utf8"

	"werkbord/desktop/internal/teamlink"
)

// The one create-and-join flow. The window's "New team or join…" dialog asks for a name (or an invitation link), and these
// methods do what the Team console's own first-run screens used to do: make a place for the team on this computer, have Team's
// service create the team or enrol this computer in it, and say how far it has got. They never install anything (ActivateTeam
// does, after asking) and never run a person's agent: connecting the person's runner is a separate call, Relay's ConnectRunner,
// which asks in a native dialog a page cannot press.

// SetupDefaults are what the dialog starts its fields with: the person's name on this Mac, and a name for this computer.
type SetupDefaults struct {
	Name   string `json:"name"`
	Device string `json:"device"`
}

// TeamDefaults suggests the person's name and this computer's name for the dialog. Either may be empty. It reads nothing but
// the account's display name and the computer's name.
func (s *Shell) TeamDefaults() SetupDefaults {
	var d SetupDefaults
	if u, err := user.Current(); err == nil {
		d.Name = strings.TrimSpace(u.Name)
	}
	if h, err := os.Hostname(); err == nil {
		d.Device = strings.TrimSuffix(strings.TrimSpace(h), ".local")
	}
	return d
}

// CheckInvitation reads an invitation link and says which team it is for and until when. It changes nothing and contacts no
// one: the link is checked on this computer.
func (s *Shell) CheckInvitation(link string) (teamlink.Invitation, error) {
	link = strings.TrimSpace(link)
	if !ValidInvitation(link) {
		return teamlink.Invitation{}, errors.New("this is not a Werkbord Team invitation: copy the whole link, which starts with werkbord://join/")
	}
	ctx, cancel := s.timeout(15 * time.Second)
	defer cancel()
	inv, err := s.o.Team.InspectInvitation(ctx, link)
	if err != nil {
		return teamlink.Invitation{}, s.teamError(err)
	}
	return inv, nil
}

// TeamSetup is a team being created or joined: the slot it is in, which is the workspace's identity once it exists.
type TeamSetup struct {
	ID string `json:"id"`
}

const maxName = 80

func cleanName(label, v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", errors.New("enter " + label)
	}
	if utf8.RuneCountInString(v) > maxName || strings.ContainsAny(v, "\r\n\t") {
		return "", errors.New(label + " must be on one line and at most 80 characters")
	}
	return v, nil
}

// CreateTeam starts creating a team, hosted on this computer, and returns at once: TeamSetupState says when it is done. Nothing
// is left behind when it cannot start.
func (s *Shell) CreateTeam(name, owner string) (TeamSetup, error) {
	name, err := cleanName("a team name", name)
	if err != nil {
		return TeamSetup{}, err
	}
	owner, err = cleanName("your name", owner)
	if err != nil {
		return TeamSetup{}, err
	}
	ctx, cancel := s.timeout(30 * time.Second)
	defer cancel()
	id, err := s.o.Team.AddWorkspace(ctx)
	if err != nil {
		return TeamSetup{}, s.teamError(err)
	}
	// A team of more than two needs the person's key; the service keeps it for them once, before anything is created.
	s.givePremium(ctx, id)
	if err := s.o.Team.CreateWorkspace(ctx, id, name, owner); err != nil {
		s.abandon(id)
		return TeamSetup{}, err
	}
	return TeamSetup{ID: id}, nil
}

// JoinTeam starts enrolling this computer in the team an invitation is for, and returns at once. The person's unlock key goes
// with it, when they have one: the host checks it before admitting a third member.
func (s *Shell) JoinTeam(link, name, device string) (TeamSetup, error) {
	link = strings.TrimSpace(link)
	if !ValidInvitation(link) {
		return TeamSetup{}, errors.New("this is not a Werkbord Team invitation: copy the whole link, which starts with werkbord://join/")
	}
	name, err := cleanName("your name", name)
	if err != nil {
		return TeamSetup{}, err
	}
	device, err = cleanName("a name for this computer", device)
	if err != nil {
		return TeamSetup{}, err
	}
	ctx, cancel := s.timeout(30 * time.Second)
	defer cancel()
	id, err := s.o.Team.AddWorkspace(ctx)
	if err != nil {
		return TeamSetup{}, s.teamError(err)
	}
	key := ""
	if s.o.Premium != nil {
		key = s.o.Premium.Key()
	}
	if err := s.o.Team.JoinWorkspace(ctx, id, link, name, device, key); err != nil {
		s.abandon(id)
		return TeamSetup{}, err
	}
	return TeamSetup{ID: id}, nil
}

// abandon drops a slot made for a team that could not be started. It is best effort: an empty slot is harmless and the
// Workspaces page offers to remove it.
func (s *Shell) abandon(id string) {
	ctx, cancel := s.timeout(15 * time.Second)
	defer cancel()
	if err := s.o.Team.ForgetWorkspace(ctx, id); err != nil {
		s.o.Log.Debug("an empty team slot was left", "workspace", id, "err", err)
		return
	}
	s.o.Workspaces.Forget(id)
}

// SetupView is how far a team being created or joined has got.
type SetupView struct {
	// Working is what the service is doing now, in words ("" when nothing is).
	Working string `json:"working"`
	// Error is why it stopped, when it did.
	Error string `json:"error,omitempty"`
	// Done: the team exists on this computer, the person is in it and its service has loaded it (it names the team); a runner can be connected only then. Waiting: the person's enrolment waits for an administrator.
	Done    bool `json:"done"`
	Waiting bool `json:"waiting"`
	// Exists: the team is on this computer already (so a failure now is about reaching it, and it is left from its own settings).
	Exists bool `json:"exists,omitempty"`
	// Name is the team's name once it is known.
	Name string `json:"name,omitempty"`
}

// TeamSetupState says how far creating or joining the team in a slot has got.
func (s *Shell) TeamSetupState(id string) (SetupView, error) {
	ctx, cancel := s.timeout(10 * time.Second)
	defer cancel()
	st, err := s.o.Team.SetupOf(ctx, id)
	if err != nil {
		return SetupView{}, s.teamError(err)
	}
	return SetupView{Working: st.Operation, Error: st.Error, Done: st.Enrolled && st.Operation == "" && !st.Pending && st.Workspace != "", Waiting: st.Pending && st.Operation == "", Exists: st.Enrolled, Name: st.Workspace}, nil
}

// CancelTeamSetup stops creating or joining a team that is not finished: an enrolment that waits for an administrator is
// withdrawn, and the empty place made for it is removed. A team that already exists is never touched here; leaving one is its own,
// careful, action.
func (s *Shell) CancelTeamSetup(id string) error {
	ctx, cancel := s.timeout(20 * time.Second)
	defer cancel()
	st, err := s.o.Team.SetupOf(ctx, id)
	if err != nil {
		return s.teamError(err)
	}
	if st.Enrolled {
		return errors.New("this team already exists on this computer; leave it from its own settings")
	}
	if st.Operation != "" {
		return errors.New("wait for Team to finish what it is doing, then cancel")
	}
	if st.Pending {
		if err := s.o.Team.CancelJoin(ctx, id); err != nil {
			return err
		}
	}
	if err := s.o.Team.ForgetWorkspace(ctx, id); err != nil {
		return err
	}
	s.o.Workspaces.Forget(id)
	return nil
}
