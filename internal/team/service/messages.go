package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"devboard/internal/envelope"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// The mailbox: how one of a person's devices asks another of their own to do something.
//
// A person on their phone, or on another computer, can ask their runner "open this ticket" or "start that approved task".
// The asking device signs the request, with a key only it holds (internal/envelope). The workspace's host stores it here
// and hands it to the device it is for; that device checks the signature itself, against the sender's registered public
// key, and decides under its own policy whether to act. So:
//
//   - A host that routes a request cannot forge one. It has no sender's key, a request it changed would no longer verify,
//     and a request it made up would have to be signed by a key registered to the person it names.
//   - Nobody can ask someone else's device. Both devices must belong to the person who signed, and the target must be a
//     runner.
//   - The workspace never runs anything. It stores a request made of identifiers and hands it over. The device it is for
//     passes it to that person's own Werkbord, which applies its own execution policies and approvals.
//
// There is no message that carries a command, a path, an environment or a script: the actions are the closed list in
// envelope/actions.go, and the payload of each is identifiers.

// inbox is the key a device's inbox waits on.
func inboxKey(deviceID string) string { return "inbox:" + deviceID }

// MaxInboxWait bounds how long a device's wait for its inbox is held open.
const MaxInboxWait = MaxSyncWait

// SendMessage stores a signed request for another of the sender's devices. The request must be signed by the device that
// sends it, for a runner of the same person, in this workspace.
func (s *Service) SendMessage(ctx context.Context, a Actor, env envelope.Envelope) (domain.DeviceMessage, error) {
	if a.Device == nil {
		return domain.DeviceMessage{}, forbidden("send a request: a device sends its own, with its own credential")
	}
	if err := a.require(domain.PermDevicesOwn, "ask your devices to do something"); err != nil {
		return domain.DeviceMessage{}, err
	}
	switch {
	case env.DeviceID != a.Device.ID:
		return domain.DeviceMessage{}, forbidden("send a request that another device signed")
	case env.WorkspaceID != a.Workspace.ID || env.UserID != a.Member.ID:
		return domain.DeviceMessage{}, forbidden("send a request that names another person or workspace")
	case !env.Action.NeedsTarget():
		return domain.DeviceMessage{}, fmt.Errorf("%w: %s is not a request for a device", domain.ErrInvalid, env.Action)
	}
	now := s.stamp()
	// What the database can say about the target is looked at before the signature is spent against the replay cache, so that
	// a request refused for being unreachable is not also marked as used.
	err := s.db.View(ctx, func(tx store.Tx) error {
		target, err := tx.Device(ctx, a.Workspace.ID, env.TargetDeviceID)
		if err != nil || target.MemberID != a.Member.ID || target.Revoked() {
			// Another person's device, a revoked one and one that does not exist are the same answer.
			return fmt.Errorf("%w: device", domain.ErrNotFound)
		}
		if !target.Has(domain.CapabilityRunner) {
			return fmt.Errorf("%w: %s is not a runner, so it has no work to open or start", domain.ErrConflict, target.Name)
		}
		n, err := tx.CountQueuedMessagesFor(ctx, a.Workspace.ID, target.ID, now)
		if err != nil {
			return err
		}
		if n >= domain.MaxQueuedPerDevice {
			return fmt.Errorf("%w: %s has %d requests waiting already; it has to catch up first", domain.ErrBusy, target.Name, n)
		}
		return nil
	})
	if err != nil {
		return domain.DeviceMessage{}, err
	}
	v, err := envelope.NewVerifier(s.DeviceDirectory(), s.replay, "")
	if err != nil {
		return domain.DeviceMessage{}, err
	}
	v.Now = s.now
	if _, err := v.Verify(ctx, env); err != nil {
		return domain.DeviceMessage{}, verifyError(err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return domain.DeviceMessage{}, err
	}
	m := domain.DeviceMessage{ID: env.MessageID, WorkspaceID: a.Workspace.ID, FromDeviceID: env.DeviceID, ToDeviceID: env.TargetDeviceID, MemberID: a.Member.ID,
		Action: string(env.Action), State: domain.MessageQueued, CreatedAt: now, ExpiresAt: env.ExpiryTime().Truncate(time.Millisecond), Envelope: raw}
	err = s.db.Update(ctx, func(tx store.Tx) error {
		if _, err := tx.PurgeMessages(ctx, a.Workspace.ID, now.Add(-domain.MessageKeep)); err != nil {
			return err
		}
		return tx.InsertMessage(ctx, m)
	})
	if err != nil {
		return domain.DeviceMessage{}, err
	}
	s.hub.notify(inboxKey(m.ToDeviceID))
	return m.WithoutEnvelope(), nil
}

// verifyError is what a sender is told about a request that did not verify. It says what is wrong with the request and
// nothing about anyone's key.
func verifyError(err error) error {
	switch {
	case errors.Is(err, envelope.ErrReplay):
		return fmt.Errorf("%w: this request was already sent", domain.ErrConflict)
	case errors.Is(err, envelope.ErrExpired):
		return fmt.Errorf("%w: this request has expired; make a new one (check this device's clock)", domain.ErrInvalid)
	case errors.Is(err, envelope.ErrNotYetValid):
		return fmt.Errorf("%w: this request is dated in the future; check this device's clock", domain.ErrInvalid)
	case errors.Is(err, envelope.ErrReplayCacheFull):
		return fmt.Errorf("%w: the workspace is busy; try again in a moment", domain.ErrBusy)
	}
	return fmt.Errorf("%w: this request does not verify (%v)", domain.ErrInvalid, err)
}

// Inbox returns the requests waiting for the calling device, oldest first, waiting up to wait for one to arrive when there
// is none. Only a device collects, and only its own: it is handed the signed requests and checks them itself.
func (s *Service) Inbox(ctx context.Context, a Actor, wait time.Duration) ([]domain.DeviceMessage, error) {
	if a.Device == nil {
		return nil, forbidden("collect requests: a device collects its own, with its own credential")
	}
	if wait > MaxInboxWait {
		wait = MaxInboxWait
	}
	release, err := s.hub.acquire(a.Member.ID)
	if err != nil {
		return nil, err
	}
	defer release()
	ch, cancel := s.hub.subscribe(inboxKey(a.Device.ID))
	defer cancel()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	sessionTick := time.NewTicker(time.Second)
	defer sessionTick.Stop()
	for {
		var out []domain.DeviceMessage
		err := s.db.View(ctx, func(tx store.Tx) (err error) {
			out, err = tx.QueuedMessagesFor(ctx, a.Workspace.ID, a.Device.ID, s.stamp(), 20)
			return
		})
		if err != nil || len(out) > 0 || wait <= 0 {
			return out, err
		}
		select {
		case <-ch:
		case <-sessionTick.C:
		case <-timer.C:
			err := s.db.View(ctx, func(tx store.Tx) error { return nil })
			return out, err
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
}

// AckMessage records what the calling device did with a request it was sent: done, or refused, with a short JSON answer.
// Only the device a request is for can say so, and only once.
func (s *Service) AckMessage(ctx context.Context, a Actor, id string, state domain.MessageState, result json.RawMessage) (domain.DeviceMessage, error) {
	if a.Device == nil {
		return domain.DeviceMessage{}, forbidden("answer a request: a device answers its own, with its own credential")
	}
	if !state.Valid() {
		return domain.DeviceMessage{}, fmt.Errorf("%w: a request is answered as done or refused", domain.ErrInvalid)
	}
	if len(result) > domain.MaxResultBytes {
		return domain.DeviceMessage{}, fmt.Errorf("%w: an answer is at most %d bytes", domain.ErrInvalid, domain.MaxResultBytes)
	}
	if len(result) > 0 {
		var obj map[string]any
		if err := json.Unmarshal(result, &obj); err != nil {
			return domain.DeviceMessage{}, fmt.Errorf("%w: an answer is a JSON object", domain.ErrInvalid)
		}
	}
	var out domain.DeviceMessage
	err := s.db.Update(ctx, func(tx store.Tx) error {
		m, err := tx.Message(ctx, a.Workspace.ID, id)
		if err != nil || m.ToDeviceID != a.Device.ID {
			return fmt.Errorf("%w: message", domain.ErrNotFound)
		}
		ok, err := tx.DecideMessage(ctx, a.Workspace.ID, id, a.Device.ID, state, string(result), s.stamp())
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: this request has already been answered or has expired", domain.ErrConflict)
		}
		out, err = tx.Message(ctx, a.Workspace.ID, id)
		return err
	})
	return out.WithoutEnvelope(), err
}

// Message returns a request the actor made, with what happened to it. It is the person's own: another person's is not found.
func (s *Service) Message(ctx context.Context, a Actor, id string) (domain.DeviceMessage, error) {
	var out domain.DeviceMessage
	err := s.db.View(ctx, func(tx store.Tx) error {
		m, err := tx.Message(ctx, a.Workspace.ID, id)
		if err != nil || m.MemberID != a.Member.ID {
			return fmt.Errorf("%w: message", domain.ErrNotFound)
		}
		out = m
		return nil
	})
	if err != nil {
		return domain.DeviceMessage{}, err
	}
	return s.asSeen(out), nil
}

// Messages lists the actor's own most recent requests, newest first, with what happened to each.
func (s *Service) Messages(ctx context.Context, a Actor) ([]domain.DeviceMessage, error) {
	var out []domain.DeviceMessage
	err := s.db.View(ctx, func(tx store.Tx) (err error) {
		out, err = tx.MessagesOfMember(ctx, a.Workspace.ID, a.Member.ID, 50)
		return
	})
	for i := range out {
		out[i] = s.asSeen(out[i])
	}
	return out, err
}

func (s *Service) asSeen(m domain.DeviceMessage) domain.DeviceMessage {
	m = m.WithoutEnvelope()
	m.State = m.StateAt(s.stamp())
	return m
}
