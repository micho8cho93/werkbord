package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseInterspersed(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	name := fs.String("name", "", "")
	got, err := parseInterspersed(fs, []string{"./repo", "--name", "Demo app"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"./repo"}) || *name != "Demo app" {
		t.Fatalf("positional=%v name=%q", got, *name)
	}
}

// tokenEnv points the CLI at an empty data directory with default settings.
func tokenEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DEVBOARD_DATA_DIR", dir)
	t.Setenv("DEVBOARD_TOKEN", "")
	t.Setenv("DEVBOARD_REQUIRE_TOKEN", "")
	t.Setenv("DEVBOARD_ADDR", "")
	return dir
}

func runCLI(args ...string) (stdout string, err error) {
	var out, errOut bytes.Buffer
	err = run(args, &out, &errOut)
	return out.String(), err
}

func TestTokenCommand(t *testing.T) {
	dir := tokenEnv(t)

	if _, err := runCLI("token"); err == nil || !strings.Contains(err.Error(), "werkbord serve") {
		t.Fatalf("before the controller has run: err = %v, want a hint to start it", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("abc123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := runCLI("token"); err != nil || out != "abc123\n" {
		t.Errorf("token = %q, %v; want exactly the token and a newline", out, err)
	}
	if out, err := runCLI("token", "--url"); err != nil || out != "http://127.0.0.1:7420/#token=abc123\n" {
		t.Errorf("token --url = %q, %v", out, err)
	}
	// A controller listening on all interfaces is reached locally via loopback.
	if out, err := runCLI("token", "--url", "--addr", "0.0.0.0:9000"); err != nil || out != "http://127.0.0.1:9000/#token=abc123\n" {
		t.Errorf("token --url --addr = %q, %v", out, err)
	}
	if _, err := runCLI("token", "extra"); err == nil {
		t.Error("unexpected arguments were accepted")
	}
}

func TestTokenCommandPrefersTheConfiguredToken(t *testing.T) {
	tokenEnv(t)
	t.Setenv("DEVBOARD_TOKEN", "from-env")
	if out, err := runCLI("token"); err != nil || out != "from-env\n" {
		t.Errorf("token = %q, %v", out, err)
	}
}

func TestTokenCommandWhenAuthenticationIsOff(t *testing.T) {
	tokenEnv(t)
	t.Setenv("DEVBOARD_REQUIRE_TOKEN", "false")
	_, err := runCLI("token")
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("err = %v, want it to say authentication is disabled", err)
	}
}
