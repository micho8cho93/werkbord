//go:build !darwin && !linux

package terminal

import (
	"devboard/internal/agent"
	"devboard/internal/domain"
)

const supported = false

type process struct{}

func start(string, string, int, int) (*process, error) { return nil, domain.ErrForbidden }
func (*process) info() agent.ProcessInfo               { return agent.ProcessInfo{} }
func (*process) read([]byte) (int, error)              { return 0, domain.ErrForbidden }
func (*process) write([]byte) error                    { return domain.ErrForbidden }
func (*process) resize(int, int) error                 { return domain.ErrForbidden }
func (*process) kill()                                 {}
func (*process) wait() error                           { return nil }
func (*process) close()                                {}
