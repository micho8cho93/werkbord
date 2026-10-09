package server

import (
	"devboard/internal/httpkit"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
)

// Native service replacement prepares a root-owned marker before stopping the
// service. Fence mutations in both the old and new generation until health has
// been verified, so enrollment cannot race installer backup/rollback.
func (h *Hub) installationFence(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := os.ReadFile(filepath.Join(filepath.Dir(h.o.Config.DataDir), "install-transaction.json"))
		var marker struct {
			Phase string `json:"phase"`
		}
		if !errors.Is(err, os.ErrNotExist) && (err != nil || json.Unmarshal(b, &marker) != nil || (marker.Phase != "verified" && marker.Phase != "rolled_back")) {
			// A healthy candidate must still trigger native recovery on the next
			// activation if the installer crashed before committing its marker.
			w.Header().Set("X-Werkbord-Installation-Pending", "1")
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				httpkit.WriteError(w, http.StatusServiceUnavailable, "maintenance", "Team installation is being verified; retry after maintenance finishes")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
