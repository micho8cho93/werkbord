// Package agentcatalog holds reviewed CLI entry points. It never downloads or runs a program.
package agentcatalog

import (
	_ "embed"
	"encoding/json"
)

type Entry struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	DocsURL string   `json:"docsUrl"`
	Setup   string   `json:"setup"`
	Adapter bool     `json:"adapter"`
}

//go:embed agents.json
var data []byte

func Entries() []Entry {
	var out []Entry
	if err := json.Unmarshal(data, &out); err != nil {
		panic(err)
	}
	return out
}
func Known(id string) bool {
	for _, e := range Entries() {
		if e.ID == id {
			return true
		}
	}
	return false
}
