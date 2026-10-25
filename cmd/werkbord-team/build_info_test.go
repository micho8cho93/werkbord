package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"devboard/internal/team/infra/nebula"
	"devboard/internal/team/infra/rqlite"
)

func TestPublicBuildMetadataReportsTheActualRuntimeTrustAnchors(t *testing.T) {
	var out bytes.Buffer
	if err := writeBuildInfo(&out); err != nil {
		t.Fatal(err)
	}
	var result map[string]string
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	network, _ := nebula.ThisPlatform()
	database, _ := rqlite.ThisPlatform()
	if len(result) != 3 || result["version"] != version || result["nebulaSHA256"] != network.BinarySHA256 || result["rqliteSHA256"] != database.BinarySHA256 {
		t.Fatal("metadata does not match compiled runtime trust anchors")
	}
}
