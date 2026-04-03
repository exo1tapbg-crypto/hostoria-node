package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// PanelClient sends callbacks from the node to the billing panel.
type PanelClient struct {
	baseURL    string
	token      string
	nodeUUID   string
	httpClient *http.Client
}

// New creates a PanelClient.
func New(baseURL, token, nodeUUID string) *PanelClient {
	return &PanelClient{
		baseURL:  baseURL,
		token:    token,
		nodeUUID: nodeUUID,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Heartbeat sends a heartbeat to the billing panel, reporting node system stats.
func (c *PanelClient) Heartbeat(ctx context.Context, stats interface{}) error {
	body := map[string]interface{}{
		"node_uuid": c.nodeUUID,
		"system":    stats,
	}
	return c.post(ctx, "/api/daemon/heartbeat", body)
}

// ReportServerStatus reports a game server's current state to the billing panel.
func (c *PanelClient) ReportServerStatus(ctx context.Context, serverUUID, state string, resources interface{}) error {
	body := map[string]interface{}{
		"status":    state,
		"resources": resources,
	}
	return c.post(ctx, "/api/daemon/servers/"+serverUUID+"/status", body)
}

func (c *PanelClient) post(ctx context.Context, path string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer extoken:"+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("panel returned HTTP %d for %s", resp.StatusCode, path)
	}
	return nil
}
