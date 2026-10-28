package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"werkbord/internal/clientaccess"
	"werkbord/internal/device"
	"werkbord/internal/runnerwire"
)

// configureDevice adds a graphical entry point to join/connect, not another
// pairing or switching implementation. Codes arrive only over private stdin.
func (a *app) configureDevice(ctx context.Context, args []string, input io.Reader) error {
	fs := flag.NewFlagSet("device configure", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	commonFlags(fs, &a.cfg)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("device configure takes a JSON request on standard input")
	}
	var req device.SetupRequest
	dec := json.NewDecoder(io.LimitReader(input, 8193))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return errors.New("invalid device setup request")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return errors.New("invalid device setup request")
	}
	if err := req.Validate(); err != nil {
		return err
	}
	if err := a.checkRunnerSettled(); err != nil {
		return err
	}
	state, err := device.Load(a.cfg.DataDir)
	if err != nil {
		return err
	}
	if state.Remote() && state.Controller != req.Controller {
		return errors.New("switch back to this computer's controller before selecting another controller")
	}
	if !a.cfg.RequireToken || !a.cfg.IsLoopback() {
		return errors.New("desktop control requires authenticated loopback networking")
	}
	// Validate both grants before consuming either. A retry after partial pairing
	// reuses saved identities rather than consuming a one-time code twice.
	if req.RunnerCode != "" {
		address, _, err := runnerwire.DecodeCode(req.RunnerCode)
		if err != nil || address != req.Controller {
			return errors.New("runner code belongs to another controller or is invalid")
		}
	}
	saved, credErr := device.LoadCredential(a.cfg.DataDir)
	if credErr != nil && !os.IsNotExist(credErr) {
		return credErr
	}
	if req.Code == "" && (credErr != nil || saved.Controller != req.Controller) {
		return errors.New("enter a desktop pairing code from the controller")
	}
	if req.Runner {
		identity, err := loadIdentity(a.cfg)
		if err == nil {
			if identity.Controller != req.Controller {
				return errors.New("this runner belongs to another controller; resolve its pairing first")
			}
		}
		if err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			if req.RunnerCode == "" {
				return errors.New("enter the separate runner pairing code")
			}
			manager := a.runnerDaemon(ctx)
			if def, ok := a.describeRunner(ctx); ok && def.DataDir != "" && filepath.Clean(def.DataDir) != filepath.Clean(a.cfg.DataDir) {
				return errors.New("runner service belongs to another installation")
			}
			before, err := manager.Status(ctx)
			if err != nil {
				return err
			}
			if before.Running {
				if err = manager.Stop(ctx); err != nil {
					return err
				}
				defer func() { _ = manager.Start(context.Background()) }()
			}
			if err = cmdJoin(a.cfg, []string{req.RunnerCode, "--pair-only", "--name", req.Name}, a.out, a.errOut); err != nil {
				return err
			}
		}
	}
	sum := sha256.Sum256([]byte(req.Code))
	hash := hex.EncodeToString(sum[:])
	var receipt struct {
		Hash string `json:"hash"`
	}
	receiptPath := filepath.Join(a.cfg.DataDir, "device", "last-pairing.json")
	raw, _ := os.ReadFile(receiptPath)
	_ = json.Unmarshal(raw, &receipt)
	// connect may save the credential before refusing a busy controller. Remember
	// that consumed code even on failure so the same guided request is recoverable.
	args = []string{"connect", req.Controller, "--name", req.Name}
	if req.Runner {
		args = append(args, "--runner")
	}
	if req.Code != "" && (receipt.Hash != hash || credErr != nil) {
		args = append(args, "--pair")
	}
	err = a.deviceCommand(ctx, args, strings.NewReader(req.Code+"\n"))
	if after, e := device.LoadCredential(a.cfg.DataDir); req.Code != "" && e == nil && after.Controller == req.Controller && (credErr != nil || after.Token != saved.Token || receipt.Hash == hash) {
		if e = clientaccess.WriteJSON(receiptPath, struct {
			Hash string `json:"hash"`
		}{hash}); e != nil && err == nil {
			return e
		}
	}
	return err
}

func (a *app) bindDeviceRepository(ctx context.Context, args []string, input io.Reader) error {
	fs := flag.NewFlagSet("device bind", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	commonFlags(fs, &a.cfg)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("device bind takes JSON on standard input")
	}
	var req struct{ Project, Path string }
	dec := json.NewDecoder(io.LimitReader(input, 8193))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil {
		return errors.New("invalid clone binding")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return errors.New("invalid clone binding")
	}
	if err := a.checkRunnerSettled(); err != nil {
		return err
	}
	return cmdRunner(a.cfg, []string{"repo", req.Project, req.Path}, a.out, a.errOut)
}
