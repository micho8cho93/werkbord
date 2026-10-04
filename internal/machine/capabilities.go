// Package machine reports safe host measurements. Missing measurements remain
// unknown rather than being inferred from Go's own heap or fabricated values.
package machine

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"devboard/internal/agent"
	"devboard/internal/domain"
)

func Capabilities(ctx context.Context, registry *agent.Registry, dir string) domain.RunnerCapabilities {
	c := domain.RunnerCapabilities{CPU: runtime.NumCPU(), Agents: registry.Detect(ctx), Options: []domain.AgentOptions{}, Repositories: []string{}}
	for _, a := range c.Agents {
		if a.Available {
			if opts, ok := registry.Options(ctx, a.ID); ok {
				c.Options = append(c.Options, opts)
			}
		}
	}
	if runtime.GOOS == "linux" {
		if data, e := os.ReadFile("/proc/meminfo"); e == nil {
			for _, line := range strings.Split(string(data), "\n") {
				f := strings.Fields(line)
				if len(f) < 2 {
					continue
				}
				n, e := strconv.ParseInt(f[1], 10, 64)
				if e != nil {
					continue
				}
				n *= 1024
				switch f[0] {
				case "MemTotal:":
					c.RAMBytes = &n
				case "MemAvailable:":
					c.AvailableRAMBytes = &n
				}
			}
		}
	}
	if runtime.GOOS == "darwin" {
		if data, e := exec.CommandContext(ctx, "sysctl", "-n", "hw.memsize").Output(); e == nil {
			n, e := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
			if e == nil {
				c.RAMBytes = &n
			}
		}
		if data, e := exec.CommandContext(ctx, "vm_stat").Output(); e == nil {
			lines := strings.Split(string(data), "\n")
			page := int64(4096)
			if len(lines) > 0 {
				for _, v := range strings.Fields(lines[0]) {
					if n, e := strconv.ParseInt(v, 10, 64); e == nil {
						page = n
					}
				}
			}
			var pages int64
			for _, line := range lines {
				if strings.HasPrefix(line, "Pages free:") || strings.HasPrefix(line, "Pages inactive:") || strings.HasPrefix(line, "Pages speculative:") {
					parts := strings.Fields(line)
					if len(parts) > 0 {
						n, _ := strconv.ParseInt(strings.TrimSuffix(parts[len(parts)-1], "."), 10, 64)
						pages += n
					}
				}
			}
			n := pages * page
			if n > 0 {
				c.AvailableRAMBytes = &n
			}
		}
	}
	c.StorageBytes = storage(dir)
	return c
}
