package domain

import "time"

// RunnerKind says where a runner's agents execute.
type RunnerKind string

// RunnerLocal is the computer the controller runs on: and can execute tasks itself.
const RunnerLocal RunnerKind = "local"

const RunnerRemote RunnerKind = "remote"

// Runner is a computer that can run agents. The controller registers the one
// it runs on when it starts, so that finishing setup always leaves a working
// runner. Everything about a run (its worktree, its processes) happens on its
// runner, including paired remote machines.
type Runner struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Kind     RunnerKind `json:"kind"`
	Hostname string     `json:"hostname"`
	OS       string     `json:"os"`
	Arch     string     `json:"arch"`
	// Version is the Werkbord version it last registered with.
	Version    string    `json:"version"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	// Online is derived from a recent authenticated heartbeat; the local runner
	// is online while its controller serves requests.
	Online       bool               `json:"online"`
	Disabled     bool               `json:"disabled"`
	Removed      bool               `json:"removed"`
	Capacity     int                `json:"capacity"`
	Automatic    bool               `json:"automatic"`
	Projects     []string           `json:"projects"`
	AllowClone   bool               `json:"allowClone"`
	Capabilities RunnerCapabilities `json:"capabilities"`
	CurrentRuns  int                `json:"currentRuns"`
	PublicKey    string             `json:"-"`
	LastSequence int64              `json:"-"`
}

type RunnerCapabilities struct {
	CloneEnabled      bool           `json:"cloneEnabled"`
	CPU               int            `json:"cpu"`
	RAMBytes          *int64         `json:"ramBytes,omitempty"`
	AvailableRAMBytes *int64         `json:"availableRamBytes,omitempty"`
	StorageBytes      *int64         `json:"storageBytes,omitempty"`
	Agents            []Agent        `json:"agents"`
	Options           []AgentOptions `json:"options"`
	Repositories      []string       `json:"repositories"`
	Diagnostics       string         `json:"diagnostics,omitempty"`
}

// Settings keys. Each value is one small JSON document.
const (
	SettingExecution  = "execution"  // ExecutionConfig: the global defaults
	SettingOnboarding = "onboarding" // Onboarding
	SettingNetwork    = "network"    // NetworkSetting
)

// NetworkSetting is whether the user turned the private network on. It is
// stored because the user changes it in the app, and decides at start-up unless
// config.json says otherwise.
type NetworkSetting struct {
	Enabled bool `json:"enabled"`
}

// Onboarding records how far first-time setup has got. It decides only whether
// the app opens the setup flow; nothing is ever blocked on it.
type Onboarding struct {
	// CompletedAt is set when the user finished or skipped setup.
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	// Skipped lists steps the user chose to skip ("network", "github"), so the app
	// does not nag about them and a health check can still mention them.
	Skipped []string `json:"skipped,omitempty"`
	// Steps is the state of each guided-setup step: StepDone or StepSkipped. A step
	// absent from the map is still to do. Steps complete from what the app verifies;
	// the map only remembers the answer, so setup resumes where it stopped.
	Steps map[string]string `json:"steps,omitempty"`
	// Variant is the path the guided setup takes: OnboardingGitHub or OnboardingLocal.
	Variant string `json:"variant,omitempty"`
	// Tours records when each surface's short tour was shown, per device.
	Tours map[string]time.Time `json:"tours,omitempty"`
	// Example is the project and task the guided setup made, so it resumes where it stopped.
	Example *OnboardingExample `json:"example,omitempty"`
}

// OnboardingExample names the example project and its ticket.
type OnboardingExample struct {
	ProjectID string `json:"projectId"`
	TaskID    string `json:"taskId,omitempty"`
}

// Guided-setup step states and paths.
const (
	StepDone         = "done"
	StepSkipped      = "skipped"
	OnboardingGitHub = "github"
	OnboardingLocal  = "local"
)

// OnboardingSteps are the guided-setup steps, in order.
var OnboardingSteps = []string{"computer", "agents", "github", "repository", "ticket", "assistant", "publish", "phone"}

// TourSurfaces are the places that show a first-visit tour, once each per device.
var TourSurfaces = []string{"desktop", "phone", "settings", "analytics"}
