package main

import (
	"os"
	"path/filepath"
)

func bundledComponents() string {
	if app := appBundle(); app != "" {
		b, err := os.ReadFile(filepath.Join(app, "Contents", "Resources", "components.txt"))
		if err == nil && len(b) < 4096 {
			return string(b)
		}
	}
	return "Shell: " + version + "\nPersonal: " + version + "\nTeam: separately installed (development)"
}
