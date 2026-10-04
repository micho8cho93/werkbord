// Package logging builds the controller's structured logger.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// New returns a slog.Logger writing to w in "text" or "json" format.
func New(w io.Writer, level, format string) (*slog.Logger, error) {
	var lv slog.Level
	if err := lv.UnmarshalText([]byte(strings.ToLower(level))); err != nil {
		return nil, fmt.Errorf("log level %q: %w", level, err)
	}
	opts := &slog.HandlerOptions{Level: lv}
	switch format {
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	case "text", "":
		return slog.New(slog.NewTextHandler(w, opts)), nil
	default:
		return nil, fmt.Errorf("log format %q: want text or json", format)
	}
}
