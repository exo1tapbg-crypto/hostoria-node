package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/hostoria/hostoria-node/internal/config"
	"github.com/hostoria/hostoria-node/internal/server"
	"github.com/hostoria/hostoria-node/internal/system"
)

// SystemHandler handles GET /api/system
func SystemHandler(mgr *server.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := config.Get()
		dataDir := "/var/lib/hostoria"
		if cfg != nil {
			dataDir = cfg.System.Data
		}

		stats, err := system.Gather(dataDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to gather system stats: "+err.Error())
			return
		}

		resp := map[string]interface{}{
			"node_uuid":    cfg.UUID,
			"version":      Version,
			"server_count": mgr.Len(),
			"system":       stats,
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// Version is the daemon version string, set at build time via ldflags.
var Version = "dev"

// --- helpers ---

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
