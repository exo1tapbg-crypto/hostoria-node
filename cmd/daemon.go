package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	nodeapi "github.com/hostoria/hostoria-node/internal/api"
	"github.com/hostoria/hostoria-node/internal/client"
	"github.com/hostoria/hostoria-node/internal/config"
	"github.com/hostoria/hostoria-node/internal/docker"
	"github.com/hostoria/hostoria-node/internal/server"
	"github.com/hostoria/hostoria-node/internal/system"
)

var configPath string

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start the Hostoria Node daemon",
	Long:  `Loads the configuration and starts the API server, SFTP server, and heartbeat.`,
	RunE:  runDaemon,
}

func init() {
	daemonCmd.Flags().StringVar(&configPath, "config", config.DefaultConfigPath, "Path to config file")
}

func runDaemon(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config from %s: %w", configPath, err)
	}

	fmt.Printf("[hostoria] Hostoria Node %s starting\n", Version)
	fmt.Printf("[hostoria] Node UUID : %s\n", cfg.UUID)
	fmt.Printf("[hostoria] Panel URL : %s\n", cfg.Remote)
	fmt.Printf("[hostoria] Data dir  : %s\n", cfg.System.Data)

	// Ensure data directory exists
	if err := os.MkdirAll(cfg.System.Data, 0750); err != nil {
		return fmt.Errorf("creating data directory %s: %w", cfg.System.Data, err)
	}

	// Connect to Docker
	dm, err := docker.New()
	if err != nil {
		return fmt.Errorf("connecting to Docker: %w", err)
	}
	defer dm.Close()
	fmt.Println("[hostoria] Connected to Docker daemon")

	// Restore server state from existing containers
	srvm := server.NewManager()
	ctx := context.Background()
	containers, err := dm.RestoreContainerMap(ctx)
	if err != nil {
		fmt.Printf("[hostoria] Warning: failed to restore container map: %v\n", err)
	} else {
		for uuid, containerID := range containers {
			// Create a minimal server entry so existing containers are tracked
			srv := server.New(server.CreateRequest{UUID: uuid})
			srv.SetContainerID(containerID)
			running, _ := dm.IsRunning(ctx, containerID)
			if running {
				srv.SetState(server.StateRunning)
			}
			_ = srvm.Add(srv)
			fmt.Printf("[hostoria] Restored server %s (container %s)\n", uuid, containerID[:12])
		}
	}

	// Panel client for heartbeats
	panelClient := client.New(cfg.Remote, cfg.Token, cfg.UUID)

	// Start heartbeat goroutine
	go runHeartbeat(cfg, panelClient)

	// Build and start HTTP API server
	apiServer := nodeapi.New(cfg, srvm, dm)
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- apiServer.Start()
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		fmt.Printf("[hostoria] Received signal %s, shutting down...\n", sig)
	case err := <-serverErr:
		return fmt.Errorf("API server error: %w", err)
	}

	return nil
}

func runHeartbeat(cfg *config.Config, panelClient *client.PanelClient) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Send one immediately on start
	sendHeartbeat(cfg, panelClient)

	for range ticker.C {
		sendHeartbeat(cfg, panelClient)
	}
}

func sendHeartbeat(cfg *config.Config, panelClient *client.PanelClient) {
	stats, err := system.Gather(cfg.System.Data)
	if err != nil {
		fmt.Printf("[hostoria] Warning: failed to gather stats for heartbeat: %v\n", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := panelClient.Heartbeat(ctx, stats); err != nil {
		fmt.Printf("[hostoria] Warning: heartbeat failed: %v\n", err)
	}
}
