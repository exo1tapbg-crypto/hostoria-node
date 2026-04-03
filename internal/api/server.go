package api

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/hostoria/hostoria-node/internal/api/handlers"
	"github.com/hostoria/hostoria-node/internal/api/middleware"
	"github.com/hostoria/hostoria-node/internal/config"
	"github.com/hostoria/hostoria-node/internal/docker"
	"github.com/hostoria/hostoria-node/internal/server"
)

// Server is the Hostoria Node HTTP API server.
type Server struct {
	cfg     *config.Config
	servers *server.Manager
	docker  *docker.Manager
	router  *chi.Mux
}

// New creates and configures the API server with all routes.
func New(cfg *config.Config, srvm *server.Manager, dm *docker.Manager) *Server {
	s := &Server{
		cfg:     cfg,
		servers: srvm,
		docker:  dm,
	}
	s.router = s.buildRouter()
	return s
}

func (s *Server) buildRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Use(chiMiddleware.RealIP)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Logger)

	srvDeps := &handlers.ServerDeps{Servers: s.servers, Docker: s.docker}
	fileDeps := &handlers.FilesDeps{Servers: s.servers}
	consoleDeps := &handlers.ConsoleDeps{Servers: s.servers}

	r.Route("/api", func(r chi.Router) {
		// Unauthenticated health check
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		})

		// All other routes require the node token
		r.Group(func(r chi.Router) {
			r.Use(middleware.NodeAuth)

			// Node system info
			r.Get("/system", handlers.SystemHandler(s.servers))

			// Server management
			r.Get("/servers", handlers.ListServers(srvDeps))
			r.Post("/servers", handlers.CreateServer(srvDeps))

			r.Route("/servers/{uuid}", func(r chi.Router) {
				r.Get("/", handlers.GetServer(srvDeps))
				r.Delete("/", handlers.DeleteServer(srvDeps))
				r.Post("/power", handlers.PowerAction(srvDeps))
				r.Get("/resources", handlers.GetResources(srvDeps))

				// Console WebSocket
				r.Get("/ws", handlers.ConsoleWebSocket(consoleDeps))

				// File management
				r.Get("/files", handlers.ListFiles(fileDeps))
				r.Get("/files/contents", handlers.ReadFile(fileDeps))
				r.Put("/files/write", handlers.WriteFile(fileDeps))
				r.Post("/files/delete", handlers.DeleteFiles(fileDeps))
				r.Put("/files/rename", handlers.RenameFile(fileDeps))
				r.Post("/files/create-folder", handlers.CreateFolder(fileDeps))
				r.Get("/files/download", handlers.DownloadFile(fileDeps))
			})
		})
	})

	return r
}

// Start begins listening. Blocks until the server stops.
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.API.Host, s.cfg.API.Port)

	if s.cfg.API.SSL.Enabled && s.cfg.API.SSL.Cert != "" && s.cfg.API.SSL.Key != "" {
		fmt.Printf("[hostoria] API server listening (TLS) on %s\n", addr)
		return http.ListenAndServeTLS(addr, s.cfg.API.SSL.Cert, s.cfg.API.SSL.Key, s.router)
	}

	fmt.Printf("[hostoria] API server listening on %s\n", addr)
	return http.ListenAndServe(addr, s.router)
}
