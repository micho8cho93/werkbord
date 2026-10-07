package rqlite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// BecomeVoter turns a running read-only replica into a voting member, once it has caught up. rqlite
// has no call that changes a member's vote in place, so it does what its documentation describes: the
// replica is removed from the cluster's configuration (an ordinary membership change, made through the
// cluster, with all of its data kept), stopped, and started again with the join addresses and without
// the read-only flag, which adds it back as a voter; it then has only to receive what was committed
// while it was out. It never runs on a node that has not caught up.
//
// If a step after the removal fails the node is out of the cluster and stopped or not ready; the error
// says so, and Start with the same configuration (the join addresses are in it) is the way back.
func (s *Supervisor) BecomeVoter(ctx context.Context) error {
	cfg, ok := s.Config()
	if !ok {
		return errors.New("rqlite: not started")
	}
	if !cfg.NonVoter {
		return nil
	}
	if len(cfg.Join) == 0 {
		return errors.New("rqlite: a node that was not told which cluster to join cannot be made a voter")
	}
	admin := NewAdmin(cfg.HTTPAddr, cfg.Credentials)
	if err := admin.Ready(ctx, true); err != nil {
		return fmt.Errorf("rqlite: this node has not caught up with the cluster, so it is not made a voter: %w", err)
	}
	if err := admin.Remove(ctx, cfg.NodeID); err != nil {
		return fmt.Errorf("rqlite: changing this node's vote: removing it from the configuration: %w", err)
	}
	if err := s.Stop(ctx); err != nil {
		return err
	}
	cfg.NonVoter = false
	if err := s.Start(ctx, cfg); err != nil {
		return fmt.Errorf("rqlite: this node was taken out of the cluster to become a voter and did not start again (Start it with the same configuration to rejoin): %w", err)
	}
	if err := s.WaitReady(ctx, true); err != nil {
		return fmt.Errorf("rqlite: this node did not rejoin the cluster as a voter: %w", err)
	}
	return nil
}

// Readdress moves the only node of a cluster to other addresses (a workspace starts on loopback, and moves
// onto its private network's address when it is about to have a second host), keeping its data. rqlite keeps a
// node's address in its Raft configuration, so this is done the way its documentation describes for changing
// a configuration: the node is stopped, a peers.json naming it alone at the new address is written in its raft
// directory, and it is started again; it reads the file once and removes it. It is allowed only for a cluster of
// exactly this one node: with more members the other members hold the old address and the change would split
// the cluster, so it is refused.
func (s *Supervisor) Readdress(ctx context.Context, cfg NodeConfig) error {
	old, ok := s.Config()
	if !ok {
		return errors.New("rqlite: not started")
	}
	if cfg.NodeID != old.NodeID || cfg.DataDir != old.DataDir {
		return errors.New("rqlite: a node keeps its ID and its directory when it is moved")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if len(cfg.Join) > 0 || cfg.NonVoter {
		return errors.New("rqlite: a node that is moved to other addresses is not told to join anything")
	}
	admin := NewAdmin(old.HTTPAddr, old.Credentials)
	nodes, err := admin.Nodes(ctx)
	if err != nil {
		return fmt.Errorf("rqlite: cannot tell whether this node is alone in its cluster: %w", err)
	}
	if len(nodes) != 1 || nodes[0].ID != old.NodeID {
		return fmt.Errorf("rqlite: this node has %d members in its cluster, and only a node that is alone can be moved to other addresses", len(nodes))
	}
	if err := s.Stop(ctx); err != nil {
		return err
	}
	peers := fmt.Sprintf(`[{"id":%q,"address":%q,"non_voter":false}]`+"\n", cfg.NodeID, cfg.raftAdv().String())
	dir := filepath.Join(cfg.nodeDir(), "raft")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(dir, "peers.json"), []byte(peers), 0o600); err != nil {
		return err
	}
	if err := s.Start(ctx, cfg); err != nil {
		return fmt.Errorf("rqlite: this node was stopped to be moved and did not start at its new address: %w", err)
	}
	return s.WaitReady(ctx, false)
}
