package main

import (
	"encoding/json"
	"io"

	"werkbord/internal/team/infra/nebula"
	"werkbord/internal/team/infra/rqlite"
)

// writeBuildInfo emits only public build metadata. It never initializes a
// workspace, reads keys, starts a sidecar, or takes a runtime pin override.
func writeBuildInfo(out io.Writer) error {
	network, err := nebula.ThisPlatform()
	if err != nil {
		return err
	}
	database, err := rqlite.ThisPlatform()
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(struct {
		Version      string `json:"version"`
		NebulaSHA256 string `json:"nebulaSHA256"`
		RqliteSHA256 string `json:"rqliteSHA256"`
	}{version, network.BinarySHA256, database.BinarySHA256})
}
