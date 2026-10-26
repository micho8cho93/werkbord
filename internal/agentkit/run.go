package agentkit

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"werkbord/internal/domain"
)

// commandTimeout bounds every question put to an agent's command line.
const commandTimeout = 10 * time.Second

// Run runs path with args and returns what it printed on stdout, trimmed. It is bounded to ten seconds and runs in
// SanitizedEnv.
func Run(ctx context.Context, path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = SanitizedEnv(os.Environ())
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// RunCombined is Run for a command that says what it means on stderr as well (a status line, a usage error).
func RunCombined(ctx context.Context, path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = SanitizedEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// SanitizedEnv returns env without the variables that make Git act on a
// repository other than the one in the agent's working directory (as happens
// when the controller was started from a Git hook), and without variables that
// would confuse a nested agent. Everything else, including PATH, credentials
// and the user's own configuration, is kept: the agent runs as the user.
func SanitizedEnv(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if domain.IsControllerSecret(kv) {
			continue
		}
		if strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
			continue
		}
		switch name {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
			"GIT_COMMON_DIR", "GIT_PREFIX", "GIT_NAMESPACE", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT", "GIT_QUARANTINE_PATH",
			"GIT_IMPLICIT_WORK_TREE", "GIT_INTERNAL_SUPER_PREFIX":
			continue
		}
		out = append(out, kv)
	}
	return out
}
