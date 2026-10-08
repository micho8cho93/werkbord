package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"devboard/internal/team/config"
	"devboard/internal/team/license"
)

func readLicenseFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 16385))
}

func cmdLicense(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "import" {
		return errors.New("usage: werkbord-team license import --file license.json (local workspace owner's token required)")
	}
	fs := flag.NewFlagSet("license import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var a apiFlags
	a.bind(fs)
	file := fs.String("file", "", "signed offline license document")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	raw, err := readLicenseFile(*file)
	if err != nil {
		return err
	}
	if _, err := license.Verify(raw, cfg.LicenseKey, time.Now()); err != nil {
		return err
	}
	base, hc, err := a.client()
	if err != nil {
		return err
	}
	if err := doJSON(ctx, hc, "PUT", base+"/api/team/v1/license", a.token, map[string]any{"document": json.RawMessage(raw)}, nil); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "The workspace's signed license was replaced and replicated.")
	return nil
}
