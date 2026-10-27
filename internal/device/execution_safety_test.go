package device

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutionSafetyRefusesInterruptedReceiptsAndNativeProcesses(t *testing.T) {
	for _, file := range []string{"runner/operations/op_test.json", "device-operations/operations/op_test.json", "runner/terminals.json", "terminals.json"} {
		t.Run(file, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, file)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			raw := `{"result":{"state":"executing"}}`
			safe := `{"result":{"state":"uncertain"}}`
			if filepath.Base(path) == "terminals.json" {
				raw = `[{"state":"running"}]`
				safe = `[{"state":"interrupted"}]`
			}
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if err := CheckExecutionSettled(dir); err == nil {
				t.Fatal("interrupted work permitted update")
			}
			if err := os.WriteFile(path, []byte(safe), 0600); err != nil {
				t.Fatal(err)
			}
			if err := CheckExecutionSettled(dir); err != nil {
				t.Fatal(err)
			}
		})
	}
}
