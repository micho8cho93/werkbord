package device

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CheckExecutionSettled reads only local receipts; the desktop links no executor.
func CheckExecutionSettled(dataDir string) error {
	for _, dir := range []string{"runner", "device-operations"} {
		paths, err := filepath.Glob(filepath.Join(dataDir, dir, "operations", "*.json"))
		if err != nil {
			return err
		}
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			var receipt struct{ Result struct{ State string } }
			if err != nil || json.Unmarshal(raw, &receipt) != nil || receipt.Result.State == "executing" || receipt.Result.State == "" {
				return fmt.Errorf("inspect interrupted device operation before continuing: %s", filepath.Base(path))
			}
		}
	}
	for _, path := range []string{filepath.Join(dataDir, "runner", "terminals.json"), filepath.Join(dataDir, "terminals.json")} {
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		var sessions []struct{ State string }
		if json.Unmarshal(raw, &sessions) != nil {
			return fmt.Errorf("cannot inspect terminal sessions")
		}
		for _, s := range sessions {
			if s.State == "running" {
				return fmt.Errorf("finish native terminals before continuing")
			}
		}
	}
	return nil
}
