package server

import (
	"context"
	"devboard/internal/team/connector"
	"devboard/internal/team/localwerkbord"
	"errors"
	"net/http"
	"strings"
	"time"
)

// A separately minted memory grant is delivered by the local owner, never
// obtained implicitly by connecting an execution runner.
func (d *Daemon) acceptMemoryGrant(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
		Base  string `json:"base"`
	}
	if !daemonDecode(w, r, &in) {
		return
	}
	d.control.Lock()
	defer d.control.Unlock()
	d.mu.RLock()
	v, mat := d.vault, d.mat
	d.mu.RUnlock()
	if v == nil || mat == nil {
		daemonFail(w, errors.New("join a workspace first"))
		return
	}
	if !strings.HasPrefix(in.Token, "wba_") || len(in.Token) > 256 {
		daemonFail(w, errors.New("a separate workspace-bound memory grant is required"))
		return
	}
	if in.Base == "" {
		in.Base = localwerkbord.DefaultBase
	}
	base, err := localwerkbord.CheckBase(in.Base)
	if err != nil {
		daemonFail(w, err)
		return
	}
	client, err := localwerkbord.New(base, in.Token)
	if err == nil {
		_, err = client.MemoryEvents(r.Context(), mat.Meta.WorkspaceID)
	}
	if err != nil {
		daemonFail(w, err)
		return
	}
	if err = v.SaveSecret("memory-access", []byte(in.Token)); err == nil {
		err = v.SaveSecret("memory-base", []byte(base))
	}
	if err != nil {
		daemonFail(w, err)
		return
	}
	d.notify()
	w.WriteHeader(204)
}
func (d *Daemon) synchronizeMemory(ctx context.Context) error {
	d.mu.RLock()
	v, mat, host := d.vault, d.mat, d.host
	d.mu.RUnlock()
	if v == nil || mat == nil || host == nil {
		return nil
	}
	token, err := v.Secret("memory-access")
	if err != nil {
		return nil
	}
	base, err := v.Secret("memory-base")
	if err != nil {
		return err
	}
	member, err := v.Secret("member")
	if err != nil {
		return err
	}
	client, err := localwerkbord.New(string(base), string(token))
	if err != nil {
		return err
	}
	timeout, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return connector.SyncMemory(timeout, host, client, mat.Meta.WorkspaceID, string(member), mat.Host.DeviceID())
}
