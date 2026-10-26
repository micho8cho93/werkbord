package service

import (
	"fmt"
	"regexp"
	"sync"

	"werkbord/internal/domain"
)

// DisplayContext is a Team the person's computer has connected, by the name the shell shows it under. It exists so that the
// assistant can offer it as a place for a generated workspace to appear, from a phone as much as from the computer. It is
// display only: no address, credential or permission, and the controller never reaches the Team through it.
type DisplayContext struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var displayContextID = regexp.MustCompile(`^[A-Za-z0-9:_-]{1,40}$`)

type displayContexts struct {
	sync.RWMutex
	list []DisplayContext
}

// SetDisplayContexts replaces what the computer last said it has connected. It is kept in memory: the computer says it again
// whenever it starts or its Teams change, and until it does a phone is simply not offered a Team.
func (s *Personal) SetDisplayContexts(list []DisplayContext) error {
	if len(list) > 20 {
		return fmt.Errorf("%w: at most 20 display contexts", domain.ErrInvalid)
	}
	seen := map[string]bool{}
	for _, c := range list {
		if !displayContextID.MatchString(c.ID) || c.Name == "" || len(c.Name) > 120 || seen[c.ID] {
			return fmt.Errorf("%w: invalid display context", domain.ErrInvalid)
		}
		seen[c.ID] = true
	}
	s.contexts.Lock()
	defer s.contexts.Unlock()
	s.contexts.list = append([]DisplayContext(nil), list...)
	return nil
}

// DisplayContexts is the Teams the computer last said it has connected.
func (s *Personal) DisplayContexts() []DisplayContext {
	s.contexts.RLock()
	defer s.contexts.RUnlock()
	return append([]DisplayContext(nil), s.contexts.list...)
}
