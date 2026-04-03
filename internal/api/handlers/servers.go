package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/hostoria/hostoria-node/internal/config"
	"github.com/hostoria/hostoria-node/internal/docker"
	"github.com/hostoria/hostoria-node/internal/server"
)

// ServerDeps bundles the dependencies passed to server handlers.
type ServerDeps struct {
	Servers *server.Manager
	Docker  *docker.Manager
}

// ListServers — GET /api/servers
func ListServers(deps *ServerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list := deps.Servers.All()
		data := make([]map[string]interface{}, 0, len(list))
		for _, s := range list {
			data = append(data, s.ToMap())
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": data})
	}
}

// CreateServer — POST /api/servers
// Creates the server's data directory, optionally runs the install script, and registers the server.
func CreateServer(deps *ServerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req server.CreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
		if req.UUID == "" || req.DockerImage == "" {
			writeError(w, http.StatusBadRequest, "uuid and docker_image are required")
			return
		}

		if deps.Servers.Get(req.UUID) != nil {
			writeError(w, http.StatusConflict, "server "+req.UUID+" already exists")
			return
		}

		cfg := config.Get()
		dataDir := cfg.System.Data

		// Prepare server data directory
		serverDir := filepath.Join(dataDir, req.UUID)
		if err := os.MkdirAll(serverDir, 0750); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create server directory: "+err.Error())
			return
		}

		srv := server.New(req)
		if err := deps.Servers.Add(srv); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}

		// Run install script asynchronously if provided
		if req.Install != nil {
			srv.SetState(server.StateInstalling)
			go func() {
				err := deps.Docker.RunInstallScript(srv.Context(), srv, dataDir, req.Install, func(line string) {
					srv.BroadcastConsole("[install] " + line)
				})
				if err != nil {
					srv.SetState(server.StateInstallFailed)
					srv.BroadcastConsole("[hostoria] Installation failed: " + err.Error())
				} else {
					srv.SetState(server.StateOffline)
					srv.BroadcastConsole("[hostoria] Installation complete")
				}
			}()
		}

		writeJSON(w, http.StatusCreated, srv.ToMap())
	}
}

// GetServer — GET /api/servers/{uuid}
func GetServer(deps *ServerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		srv := getServer(w, r, deps)
		if srv == nil {
			return
		}
		writeJSON(w, http.StatusOK, srv.ToMap())
	}
}

// DeleteServer — DELETE /api/servers/{uuid}
// Stops the container (if running), removes it, removes the server's data directory, and deregisters.
func DeleteServer(deps *ServerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		srv := getServer(w, r, deps)
		if srv == nil {
			return
		}
		uuid := chi.URLParam(r, "uuid")

		ctx := context.Background()
		containerID := srv.GetContainerID()
		if containerID != "" {
			_ = deps.Docker.StopContainer(ctx, containerID)
			_ = deps.Docker.RemoveContainer(ctx, containerID)
		}

		// Remove data directory
		cfg := config.Get()
		serverDir := filepath.Join(cfg.System.Data, uuid)
		_ = os.RemoveAll(serverDir)

		deps.Servers.Remove(uuid)
		w.WriteHeader(http.StatusNoContent)
	}
}

// PowerAction — POST /api/servers/{uuid}/power
// Body: {"action": "start"|"stop"|"restart"|"kill"}
func PowerAction(deps *ServerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		srv := getServer(w, r, deps)
		if srv == nil {
			return
		}

		var body struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		cfg := config.Get()
		ctx := context.Background()

		switch body.Action {
		case "start":
			if err := startServer(ctx, srv, deps, cfg); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}

		case "stop":
			containerID := srv.GetContainerID()
			if containerID != "" {
				srv.SetState(server.StateStopping)
				if err := deps.Docker.StopContainer(ctx, containerID); err != nil {
					writeError(w, http.StatusInternalServerError, "stop failed: "+err.Error())
					return
				}
				srv.SetState(server.StateOffline)
			}

		case "restart":
			containerID := srv.GetContainerID()
			if containerID != "" {
				srv.SetState(server.StateStopping)
				_ = deps.Docker.StopContainer(ctx, containerID)
				_ = deps.Docker.RemoveContainer(ctx, containerID)
				srv.SetContainerID("")
			}
			if err := startServer(ctx, srv, deps, cfg); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}

		case "kill":
			containerID := srv.GetContainerID()
			if containerID != "" {
				_ = deps.Docker.KillContainer(ctx, containerID)
				srv.SetState(server.StateCrashed)
			}

		default:
			writeError(w, http.StatusBadRequest, "unknown action: "+body.Action)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": string(srv.GetState())})
	}
}

// GetResources — GET /api/servers/{uuid}/resources
func GetResources(deps *ServerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		srv := getServer(w, r, deps)
		if srv == nil {
			return
		}

		containerID := srv.GetContainerID()
		if containerID == "" || srv.GetState() != server.StateRunning {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"current_state": string(srv.GetState()),
				"resources":     server.Resources{},
			})
			return
		}

		resources, err := deps.Docker.GetStats(r.Context(), containerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get stats: "+err.Error())
			return
		}
		srv.SetResources(*resources)

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"current_state": string(srv.GetState()),
			"resources":     resources,
		})
	}
}

// --- helpers ---

func getServer(w http.ResponseWriter, r *http.Request, deps *ServerDeps) *server.Server {
	uuid := chi.URLParam(r, "uuid")
	srv := deps.Servers.Get(uuid)
	if srv == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("server %q not found", uuid))
	}
	return srv
}

func startServer(ctx context.Context, srv *server.Server, deps *ServerDeps, cfg *config.Config) error {
	if srv.GetState() == server.StateInstalling {
		return fmt.Errorf("server is still installing, cannot start")
	}

	// Remove stale container if exists
	existingID := srv.GetContainerID()
	if existingID != "" {
		_ = deps.Docker.RemoveContainer(ctx, existingID)
		srv.SetContainerID("")
	}

	srv.SetState(server.StateStarting)

	// Pull image (non-blocking output to console)
	go func() { srv.BroadcastConsole("[hostoria] Pulling image: " + srv.DockerImage) }()
	if err := deps.Docker.PullImage(ctx, srv.DockerImage, func(line string) {
		srv.BroadcastConsole("[hostoria] " + line)
	}); err != nil {
		srv.SetState(server.StateCrashed)
		return fmt.Errorf("pulling image: %w", err)
	}

	containerID, err := deps.Docker.CreateContainer(ctx, srv, cfg.System.Data)
	if err != nil {
		srv.SetState(server.StateCrashed)
		return fmt.Errorf("creating container: %w", err)
	}
	srv.SetContainerID(containerID)

	if err := deps.Docker.StartContainer(ctx, containerID); err != nil {
		srv.SetState(server.StateCrashed)
		return fmt.Errorf("starting container: %w", err)
	}
	srv.SetState(server.StateRunning)

	// Watch for exit
	deps.Docker.WatchContainer(srv.Context(), containerID, func(exitCode int) {
		if exitCode == 0 {
			srv.SetState(server.StateOffline)
		} else {
			srv.SetState(server.StateCrashed)
			srv.BroadcastConsole(fmt.Sprintf("[hostoria] Server crashed (exit code %d)", exitCode))
		}
	})

	// Stream console logs
	go func() {
		_ = deps.Docker.StreamLogs(srv.Context(), containerID, func(line string) {
			srv.BroadcastConsole(line)
		})
	}()

	return nil
}
