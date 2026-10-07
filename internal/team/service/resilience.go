package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// How a workspace knows its own health. Each device reports about itself, with its own credential, every little while
// (Heartbeat): that it is there, and what kind of machine it is. From that and what the database and the network
// already say, an administrator is told in words how resilient the workspace is and what to do about it.

// ProfileRefresh is how often a device's profile is written again when nothing in it changed, so that a heartbeat is
// one small write most of the time and the profile is not rewritten on every one.
const ProfileRefresh = 6 * time.Hour

// Heartbeat records that the calling device is here, and what kind of machine it says it is. Only a device can do it,
// for itself, with its own credential: a person's token reports nothing about a device. The time is the workspace's,
// never the device's, so a device cannot claim to have been seen later than it was.
func (s *Service) Heartbeat(ctx context.Context, a Actor, p domain.DeviceProfile) error {
	if a.Device == nil {
		return forbidden("report for a device: a device reports for itself, with its own credential")
	}
	p.DeviceID = a.Device.ID
	if err := p.Validate(); err != nil {
		return err
	}
	now := s.stamp()
	return s.db.Update(ctx, func(tx store.Tx) error {
		if err := tx.RecordDeviceSeen(ctx, a.Workspace.ID, a.Device.ID, now); err != nil {
			return err
		}
		have, err := tx.DeviceProfiles(ctx, a.Workspace.ID)
		if err != nil {
			return err
		}
		if cur, ok := have[a.Device.ID]; ok && cur.Same(p) && now.Sub(cur.ReportedAt) < ProfileRefresh {
			return nil
		}
		p.ReportedAt = now
		return tx.SaveDeviceProfile(ctx, a.Workspace.ID, p)
	})
}

// RecordLocalHeartbeat is Heartbeat for the Workspace Host that runs this server, speaking for itself: it needs no
// credential of its own because it is the process that checks everyone else's. It is not reachable over HTTP.
func (s *Service) RecordLocalHeartbeat(ctx context.Context, workspaceID, deviceID string, p domain.DeviceProfile) error {
	p.DeviceID = deviceID
	if err := p.Validate(); err != nil {
		return err
	}
	now := s.stamp()
	return s.db.Update(ctx, func(tx store.Tx) error {
		if err := tx.RecordDeviceSeen(ctx, workspaceID, deviceID, now); err != nil {
			return err
		}
		have, err := tx.DeviceProfiles(ctx, workspaceID)
		if err != nil {
			return err
		}
		if cur, ok := have[deviceID]; ok && cur.Same(p) && now.Sub(cur.ReportedAt) < ProfileRefresh {
			return nil
		}
		p.ReportedAt = now
		return tx.SaveDeviceProfile(ctx, workspaceID, p)
	})
}

// Resilience tells someone who manages devices how resilient the workspace is: its hosts, whether it can be written
// to, whether it can be reached from outside, whether it is backed up; and what to do about each thing that is weak.
func (s *Service) Resilience(ctx context.Context, a Actor) (domain.Resilience, error) {
	if err := a.require(domain.PermDevicesViewAll, "see how resilient the workspace is"); err != nil {
		return domain.Resilience{}, err
	}
	in := domain.ResilienceInput{Now: s.stamp(), Storage: s.storageStatus(ctx)}
	err := s.db.View(ctx, func(tx store.Tx) error {
		devs, err := tx.Devices(ctx, a.Workspace.ID, "")
		if err != nil {
			return err
		}
		in.Devices = devs
		if in.Profiles, err = tx.DeviceProfiles(ctx, a.Workspace.ID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return domain.Resilience{}, err
	}
	// The network's own verdict, if the workspace has a private network.
	if h, err := s.NetworkHealth(ctx, a); err == nil {
		in.Network = &h
	} else if !errors.Is(err, domain.ErrConflict) {
		return domain.Resilience{}, fmt.Errorf("reading the network's health: %w", err)
	}
	return domain.AssessResilience(in), nil
}
