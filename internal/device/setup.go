package device

import (
	"encoding/hex"
	"errors"
	"strings"
)

// SetupRequest is delivered through stdin to the existing CLI. It contains no
// executable or filesystem path and never becomes a command-line argument.
type SetupRequest struct {
	Controller string `json:"controller"`
	Name       string `json:"name"`
	Code       string `json:"code"`
	Runner     bool   `json:"runner"`
	RunnerCode string `json:"runnerCode"`
}

func (r *SetupRequest) Validate() error {
	r.Controller = strings.TrimSuffix(strings.TrimSpace(r.Controller), "/")
	r.Name = strings.TrimSpace(r.Name)
	r.Code = strings.TrimSpace(r.Code)
	r.RunnerCode = strings.TrimSpace(r.RunnerCode)
	if err := ValidateController(r.Controller); err != nil {
		return err
	}
	if len(r.Name) > 120 || strings.ContainsAny(r.Name, "\x00\r\n") || len(r.RunnerCode) > 4096 {
		return errors.New("invalid device name or pairing code")
	}
	if r.Code != "" {
		b, e := hex.DecodeString(r.Code)
		if e != nil || len(b) != 32 {
			return errors.New("desktop pairing code must contain 64 hexadecimal characters")
		}
	}
	if !r.Runner && r.RunnerCode != "" {
		return errors.New("enable the runner role before supplying its pairing code")
	}
	return nil
}
