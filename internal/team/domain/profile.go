package domain

import (
	"fmt"
	"strings"
	"time"
)

// A device's profile is what it says about the kind of machine it is, so that the workspace can tell an
// administrator whether it makes a good host before they ask it to be one. It is coordination metadata like the
// rest of the registry: no path, no hostname, no address, no credential.

// DeviceForm is what kind of computer a device is.
type DeviceForm string

const (
	// FormDesktop: a desktop computer or a Mac mini that stays where it is and stays on.
	FormDesktop DeviceForm = "desktop"
	// FormLaptop: a portable computer, which sleeps, travels and is shut.
	FormLaptop DeviceForm = "laptop"
	// FormServer: an office server, a NAS, a machine in a rack: built to stay on.
	FormServer DeviceForm = "server"
	// FormUnknown: nobody has said, and the device could not tell.
	FormUnknown DeviceForm = "unknown"
)

// Valid reports whether f is a form that exists.
func (f DeviceForm) Valid() bool {
	switch f {
	case FormDesktop, FormLaptop, FormServer, FormUnknown:
		return true
	}
	return false
}

// MaxDeviceVersionLen bounds the version a device reports.
const MaxDeviceVersionLen = 64

// DeviceProfile is a device's description of itself.
type DeviceProfile struct {
	DeviceID string `json:"deviceId"`
	// Platform is darwin, linux, windows or other.
	Platform string     `json:"platform"`
	Form     DeviceForm `json:"form"`
	// Sleeps: the device has been seen to go to sleep, or its owner says it sleeps automatically.
	Sleeps bool `json:"sleeps"`
	// SleepEvents is how many times it was seen to wake in the last seven days.
	SleepEvents int `json:"sleepEvents"`
	// Version is the Team software on the device.
	Version string `json:"version,omitempty"`
	// HostConflict: the device already hosts another Team workspace, and a computer hosts for only one.
	HostConflict bool      `json:"hostConflict,omitempty"`
	ReportedAt   time.Time `json:"reportedAt"`
}

// Validate checks a profile is well formed.
func (p DeviceProfile) Validate() error {
	switch p.Platform {
	case "darwin", "linux", "windows", "other":
	default:
		return invalid("platform %q: want darwin, linux, windows or other", p.Platform)
	}
	if !p.Form.Valid() {
		return invalid("form %q: want desktop, laptop, server or unknown", p.Form)
	}
	if p.SleepEvents < 0 || p.SleepEvents > 100000 {
		return invalid("sleep events must be between 0 and 100000")
	}
	if len(p.Version) > MaxDeviceVersionLen || strings.ContainsAny(p.Version, "\r\n\x00") {
		return invalid("the version is at most %d characters on one line", MaxDeviceVersionLen)
	}
	return nil
}

// Same reports whether two profiles say the same thing about a device (when they were reported is not part of it).
func (p DeviceProfile) Same(o DeviceProfile) bool {
	return p.Platform == o.Platform && p.Form == o.Form && p.Sleeps == o.Sleeps && p.SleepEvents == o.SleepEvents && p.Version == o.Version && p.HostConflict == o.HostConflict
}

// HostFit says how good a device is as a Workspace Host: whether it can be one at all, whether it is a good
// choice, and what an administrator should know before choosing it. The reasons are words for a person.
type HostFit struct {
	// Possible: nothing known about the device rules it out.
	Possible bool `json:"possible"`
	// Ideal: it stays on, stays put and is a computer built for it.
	Ideal   bool     `json:"ideal"`
	Reasons []string `json:"reasons,omitempty"`
}

// The words a device or an administrator is told. They are constants so the screens, the API and the tests say
// the same thing.
const (
	AdviceSleeps           = "This device sleeps automatically. It is not ideal as a Workspace Host."
	AdviceLaptop           = "This is a laptop. A laptop that travels or is shut is not ideal as a Workspace Host."
	AdviceUnknownKind      = "Nobody has said what kind of computer this is. A desktop, a Mac mini or a server is the best choice for a Workspace Host."
	AdviceWindowsNoHost    = "Windows cannot be a Workspace Host yet: the private network program Team ships runs on macOS and Linux."
	AdviceNoProfileYet     = "This device has not reported what kind of computer it is yet."
	AdviceNoNetworkProgram = "This device cannot run the workspace's private network."
	AdviceHostsElsewhere   = "This computer already hosts another Team workspace. A computer can host for only one workspace, so it cannot also host this one."
)

// HostFitOf judges a profile; nil means the device has not reported one.
func HostFitOf(p *DeviceProfile) HostFit {
	if p == nil {
		return HostFit{Possible: true, Reasons: []string{AdviceNoProfileYet}}
	}
	fit := HostFit{Possible: true, Ideal: true}
	if p.HostConflict {
		fit.Possible, fit.Ideal = false, false
		fit.Reasons = append(fit.Reasons, AdviceHostsElsewhere)
		return fit
	}
	if p.Platform == "windows" {
		fit.Possible, fit.Ideal = false, false
		fit.Reasons = append(fit.Reasons, AdviceWindowsNoHost)
		return fit
	}
	if p.Platform == "other" {
		fit.Possible, fit.Ideal = false, false
		fit.Reasons = append(fit.Reasons, AdviceNoNetworkProgram)
		return fit
	}
	if p.Sleeps {
		fit.Ideal = false
		fit.Reasons = append(fit.Reasons, AdviceSleeps)
	} else if p.Form == FormLaptop {
		fit.Ideal = false
		fit.Reasons = append(fit.Reasons, AdviceLaptop)
	}
	if p.Form == FormUnknown {
		fit.Ideal = false
		fit.Reasons = append(fit.Reasons, AdviceUnknownKind)
	}
	return fit
}

// String is for logs.
func (p DeviceProfile) String() string {
	return fmt.Sprintf("%s %s sleeps=%v events=%d", p.Platform, p.Form, p.Sleeps, p.SleepEvents)
}
