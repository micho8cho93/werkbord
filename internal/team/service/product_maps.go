package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"werkbord/internal/integration"
	"werkbord/internal/team/domain"
	"werkbord/internal/team/store"
)

type ProductMapView struct {
	Repository string                    `json:"repository"`
	Records    []domain.ProductMapRecord `json:"records"`
	CanPropose bool                      `json:"canPropose"`
	CanPublish bool                      `json:"canPublish"`
	Freshness  string                    `json:"freshness"`
}

func (s *Service) ProductMaps(ctx context.Context, a Actor, p string) (ProductMapView, error) {
	out := ProductMapView{Records: []domain.ProductMapRecord{}, Freshness: "unknown"}
	if a.MCPGrantID != "" {
		return out, domain.ErrForbidden
	}
	err := s.view(AuthenticatedContext(ctx, a), a, p, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPProductMapRead, "read product maps"); err != nil {
			return err
		}
		out.Repository = domain.RepositoryKey(x.Project.Repository)
		out.CanPropose = x.can(domain.PPProductMapPropose) && !x.Project.Archived
		out.CanPublish = x.can(domain.PPProductMapPublish) && !x.Project.Archived
		records, err := tx.ProductMaps(ctx, a.Workspace.ID, p)
		if err != nil {
			return err
		}
		for _, r := range records {
			if r.Snapshot.Repository != out.Repository {
				continue
			}
			if r.State == "published" || r.AuthorID == a.Member.ID || out.CanPublish {
				r.Digest = ProductMapDigest(r.Snapshot)
				out.Records = append(out.Records, r)
			}
		}

		var latest *domain.ProductMapRecord
		for i := range out.Records {
			r := &out.Records[i]
			if r.State == "published" && (latest == nil || r.Version > latest.Version) {
				latest = r
			}
		}
		if latest != nil {
			branches, err := tx.Branches(ctx, a.Workspace.ID, p)
			if err != nil {
				return err
			}
			for _, b := range branches {
				if b.Name != latest.Snapshot.Branch || b.HeadSHA == "" || s.stamp().Sub(b.ReportedAt) > 15*time.Minute || b.ReportedAt.After(s.stamp()) {
					continue
				}
				member, err := tx.Member(ctx, a.Workspace.ID, b.ReportedBy)
				if err != nil {
					continue
				}
				role, on, err := tx.ProjectRole(ctx, a.Workspace.ID, p, b.ReportedBy)
				if err != nil || !on && !member.Can(domain.PermProjectsManage) {
					continue
				}
				if !role.Can(domain.PPGitReport) && !member.Can(domain.PermProjectsManage) {
					continue
				}
				if b.HeadSHA != latest.Snapshot.Commit {
					out.Freshness = "updates_available"
					break
				}
				out.Freshness = "current_as_reported"
			}
		}

		return nil
	})
	return out, err
}

// Proposal IDs are chosen before delivery and bound to the exact document, making
// an uncertain retry idempotent without permitting a different snapshot replay.
type ProductMapProposal struct {
	ID              string                         `json:"id"`
	ExpectedVersion int64                          `json:"expectedVersion"`
	Snapshot        integration.ProductMapSnapshot `json:"snapshot"`
}

func (s *Service) ProposeProductMap(ctx context.Context, a Actor, p string, in ProductMapProposal) (domain.ProductMapRecord, error) {
	var out domain.ProductMapRecord
	if a.MCPGrantID != "" {
		return out, domain.ErrForbidden
	}

	err := s.mutate(AuthenticatedContext(ctx, a), a, p, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPProductMapPropose, "propose product maps"); err != nil {
			return err
		}
		if x.Project.Archived {
			return domain.ErrConflict
		}
		if !integration.Identifier(in.ID) || in.ExpectedVersion < 0 || in.Snapshot.Validate() != nil || strings.HasPrefix(in.Snapshot.Repository, "local:") {
			return domain.ErrInvalid
		}
		if domain.RepositoryKey(x.Project.Repository) != in.Snapshot.Repository {
			return fmt.Errorf("%w: snapshot repository does not match this project", domain.ErrConflict)
		}
		if old, err := tx.ProductMap(ctx, a.Workspace.ID, p, in.ID); err == nil {
			left, _ := json.Marshal(old.Snapshot)
			right, _ := json.Marshal(in.Snapshot)
			if old.AuthorID != a.Member.ID || old.BaseVersion != in.ExpectedVersion || string(left) != string(right) {
				return domain.ErrConflict
			}
			out = old
			return nil
		}
		records, err := tx.ProductMaps(ctx, a.Workspace.ID, p)
		if err != nil {
			return err
		}
		version := int64(0)
		pending := 0
		for _, r := range records {
			if r.State == "published" && r.Version > version {
				version = r.Version
			}
			if r.State == "proposed" {
				pending++
			}
		}
		if version != in.ExpectedVersion || pending >= 20 {
			return domain.ErrConflict
		}
		out = domain.ProductMapRecord{ID: in.ID, ProjectID: p, AuthorID: a.Member.ID, State: "proposed", BaseVersion: version, CreatedAt: s.stamp(), Snapshot: in.Snapshot}
		if err := tx.SaveProductMap(ctx, a.Workspace.ID, p, out, true); err != nil {
			return err
		}
		return tx.AddActivity(ctx, a.Workspace.ID, domain.Activity{ProjectID: p, ActorID: a.Member.ID, Kind: domain.ActProductMapProposed, Detail: out.ID, CreatedAt: s.stamp()})
	})
	return out, err
}

type ProductMapDecision struct {
	Decision        string `json:"decision"`
	Digest          string `json:"digest"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

func ProductMapDigest(snapshot integration.ProductMapSnapshot) string {
	raw, _ := json.Marshal(snapshot)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (s *Service) ReviewProductMap(ctx context.Context, a Actor, p, id string, in ProductMapDecision) (domain.ProductMapRecord, error) {
	var out domain.ProductMapRecord
	if a.MCPGrantID != "" {
		return out, domain.ErrForbidden
	}

	err := s.mutate(AuthenticatedContext(ctx, a), a, p, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPProductMapPublish, "publish product maps"); err != nil {
			return err
		}
		if x.Project.Archived {
			return domain.ErrConflict
		}
		if in.Decision != "publish" && in.Decision != "decline" {
			return domain.ErrInvalid
		}
		record, err := tx.ProductMap(ctx, a.Workspace.ID, p, id)
		if err != nil {
			return err
		}
		if record.Snapshot.Repository != domain.RepositoryKey(x.Project.Repository) || in.Digest != ProductMapDigest(record.Snapshot) {
			return domain.ErrConflict
		}
		target := "published"
		if in.Decision == "decline" {
			target = "declined"
		}
		if record.State == target && record.ReviewerID == a.Member.ID {
			out = record
			return nil
		}
		if record.State != "proposed" {
			return domain.ErrConflict
		}
		records, err := tx.ProductMaps(ctx, a.Workspace.ID, p)
		if err != nil {
			return err
		}
		version := int64(0)
		for _, r := range records {
			if r.State == "published" && r.Version > version {
				version = r.Version
			}
		}
		if in.Decision == "publish" && (record.BaseVersion != version || in.ExpectedVersion != version) {
			return domain.ErrConflict
		}
		now := s.stamp()
		record.State = target
		record.Version = version + 1
		record.ReviewerID = a.Member.ID
		record.ReviewedAt = &now
		out = record
		if err := tx.SaveProductMap(ctx, a.Workspace.ID, p, record, false); err != nil {
			return err
		}
		kind := domain.ActProductMapPublished
		if in.Decision == "decline" {
			kind = domain.ActProductMapDeclined
		}
		return tx.AddActivity(ctx, a.Workspace.ID, domain.Activity{ProjectID: p, ActorID: a.Member.ID, Kind: kind, Detail: record.ID, CreatedAt: s.stamp()})
	})
	return out, err
}
