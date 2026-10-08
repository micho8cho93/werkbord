package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

const (
	MaxSearchQuery = 160
	searchLimit    = 20
)

// SearchResult is a small navigation index of stored coordination data. It
// carries no ticket bodies, credentials or facts from somebody's computer.
type SearchResult struct {
	Projects     []ProjectRef   `json:"projects"`
	Tickets      []SearchTicket `json:"tickets"`
	MoreProjects bool           `json:"moreProjects"`
	MoreTickets  bool           `json:"moreTickets"`
}

type SearchTicket struct {
	ID       string              `json:"id"`
	Key      string              `json:"key"`
	Title    string              `json:"title"`
	Status   domain.TicketStatus `json:"status"`
	Project  ProjectRef          `json:"project"`
	Archived bool                `json:"archived"`
}

// Search only reads the actor's workspace and visible projects in one snapshot.
// The storage query enforces the same membership restriction before limiting
// results, so hidden projects cannot consume the limit or leak through tickets.
func (s *Service) Search(ctx context.Context, a Actor, query string) (SearchResult, error) {
	out := SearchResult{Projects: []ProjectRef{}, Tickets: []SearchTicket{}}
	if utf8.RuneCountInString(query) > MaxSearchQuery {
		return out, fmt.Errorf("%w: search is longer than %d characters; shorten your query", domain.ErrInvalid, MaxSearchQuery)
	}
	query = strings.ToLower(strings.Join(strings.Fields(query), " "))
	only := a.Member.ID
	if a.Member.Can(domain.PermProjectsViewAll) {
		only = ""
	}
	err := s.db.View(ctx, func(tx store.Tx) error {
		if query == "" {
			return nil
		}
		projects, err := tx.Projects(ctx, a.Workspace.ID, only)
		if err != nil {
			return err
		}
		byID := map[string]ProjectRef{}
		for _, p := range projects {
			ref := ProjectRef{ID: p.ID, Name: p.Name, Repository: p.Repository, Archived: p.Archived}
			byID[p.ID] = ref
			text := strings.ToLower(p.Name + "\n" + p.Description + "\n" + p.Repository)
			matched := true
			for _, term := range strings.Fields(query) {
				if !strings.Contains(text, term) {
					matched = false
					break
				}
			}
			if matched {
				out.Projects = append(out.Projects, ref)
			}
		}
		sort.SliceStable(out.Projects, func(i, j int) bool {
			a, b := out.Projects[i], out.Projects[j]
			an, bn := strings.ToLower(a.Name), strings.ToLower(b.Name)
			if (an == query) != (bn == query) {
				return an == query
			}
			if a.Archived != b.Archived {
				return !a.Archived
			}
			return an < bn
		})
		if len(out.Projects) > searchLimit {
			out.MoreProjects = true
			out.Projects = out.Projects[:searchLimit]
		}
		tickets, err := tx.SearchTickets(ctx, a.Workspace.ID, only, query, searchLimit+1)
		if err != nil {
			return err
		}
		if len(tickets) > searchLimit {
			out.MoreTickets = true
			tickets = tickets[:searchLimit]
		}
		for _, k := range tickets {
			if p, ok := byID[k.ProjectID]; ok {
				out.Tickets = append(out.Tickets, SearchTicket{ID: k.ID, Key: domain.TicketKey(k.Number), Title: k.Title, Status: k.Status, Project: p, Archived: k.Archived})
			}
		}
		return nil
	})
	return out, err
}
