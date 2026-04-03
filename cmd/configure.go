package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/hostoria/hostoria-node/internal/config"
)

var (
	configurePanelURL string
	configureToken    string
	configureNodeID   string
)

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Fetch and save node configuration from the billing panel",
	Long: `Downloads the node configuration from the Hostoria billing panel and
saves it to ` + config.DefaultConfigPath + `.

Example:
  hostoria-node configure \
    --panel-url https://hostoria.space \
    --token     hbgp_xxx...          \
    --node-id   node1-o1jrfw`,
	RunE: runConfigure,
}

func init() {
	configureCmd.Flags().StringVar(&configurePanelURL, "panel-url", "", "Billing panel URL (required)")
	configureCmd.Flags().StringVar(&configureToken, "token", "", "Node deployment token (required)")
	configureCmd.Flags().StringVar(&configureNodeID, "node-id", "", "Node identifier (required)")
	_ = configureCmd.MarkFlagRequired("panel-url")
	_ = configureCmd.MarkFlagRequired("token")
	_ = configureCmd.MarkFlagRequired("node-id")
}

func runConfigure(_ *cobra.Command, _ []string) error {
	url := fmt.Sprintf("%s/api/application/nodes/%s/configuration", configurePanelURL, configureNodeID)
	fmt.Printf("Fetching configuration from %s ...\n", url)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer extoken:"+configureToken)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("contacting panel: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("panel returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Unmarshal JSON response into config struct
	var cfg config.Config
	if err := json.Unmarshal(body, &cfg); err != nil {
		return fmt.Errorf("parsing panel response: %w", err)
	}

	if cfg.UUID == "" {
		return fmt.Errorf("panel response did not include node UUID — check your token and node ID")
	}

	// Ensure config directory exists
	configDir := filepath.Dir(config.DefaultConfigPath)
	if err := os.MkdirAll(configDir, 0750); err != nil {
		return fmt.Errorf("creating config directory %s: %w", configDir, err)
	}

	// Convert to YAML and write to disk
	yamlData, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("serialising config: %w", err)
	}
	if err := os.WriteFile(config.DefaultConfigPath, yamlData, 0640); err != nil {
		return fmt.Errorf("writing config to %s: %w", config.DefaultConfigPath, err)
	}

	fmt.Printf("Configuration saved to %s\n", config.DefaultConfigPath)
	fmt.Printf("  Node UUID : %s\n", cfg.UUID)
	fmt.Printf("  Panel URL : %s\n", cfg.Remote)
	fmt.Printf("  API port  : %d\n", cfg.API.Port)
	fmt.Printf("  SFTP port : %d\n", cfg.System.SFTP.BindPort)
	fmt.Println("Run 'hostoria-node daemon' to start the node.")
	return nil
}
