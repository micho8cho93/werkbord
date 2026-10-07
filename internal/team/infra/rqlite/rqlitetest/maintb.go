//go:build !windows

package rqlitetest

import (
	"fmt"
	"os"
)

// MainTB lets a TestMain start a cluster: it is the TB of something that has no test to attach to. A failure ends the
// process (after the cleanups); Close runs the cleanups, newest first.
type MainTB struct{ cleanups []func() }

func (m *MainTB) Helper() {}

func (m *MainTB) Cleanup(f func()) { m.cleanups = append(m.cleanups, f) }

// Close runs the cleanups.
func (m *MainTB) Close() {
	for i := len(m.cleanups) - 1; i >= 0; i-- {
		m.cleanups[i]()
	}
	m.cleanups = nil
}

func (m *MainTB) Fatal(args ...any) {
	fmt.Fprintln(os.Stderr, args...)
	m.Close()
	os.Exit(1)
}

func (m *MainTB) Fatalf(format string, args ...any) { m.Fatal(fmt.Sprintf(format, args...)) }

func (m *MainTB) Skip(args ...any) { m.Fatal(append([]any{"cannot run:"}, args...)...) }

func (m *MainTB) Skipf(format string, args ...any) { m.Skip(fmt.Sprintf(format, args...)) }
