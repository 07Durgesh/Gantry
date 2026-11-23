package ingress

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CaddyClient interacts with Caddy's Admin REST API (http://localhost:2019)
// to dynamically register, update, and remove reverse proxy routes with zero downtime.
//
// Analogy: An airport flight information board.
// Instead of tearing down the physical board and rebuilding it whenever a flight's gate changes (like Nginx reload),
// Caddy lets us update a single gate number digitally in 1 millisecond.
type CaddyClient struct {
	adminURL   string
	httpClient *http.Client
}

func NewCaddyClient(adminURL string) *CaddyClient {
	if adminURL == "" {
		adminURL = "http://127.0.0.1:2019"
	}
	return &CaddyClient{
		adminURL: adminURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

type Upstream struct {
	Dial string `json:"dial"`
}

type ReverseProxyHandle struct {
	Handler   string     `json:"handler"`
	Upstreams []Upstream `json:"upstreams"`
}

type RouteMatch struct {
	Host []string `json:"host"`
}

type CaddyRoute struct {
	ID     string               `json:"@id"`
	Match  []RouteMatch         `json:"match"`
	Handle []ReverseProxyHandle `json:"handle"`
}

// RegisterRoute adds a new route for <appName>.localhost -> host.docker.internal:<hostPort>.
// It uses Caddy's route insertion at index 0 so it takes precedence over default fallbacks.
func (c *CaddyClient) RegisterRoute(ctx context.Context, appName string, hostPort int) error {
	routeID := fmt.Sprintf("gantry-app-%s", appName)
	route := CaddyRoute{
		ID: routeID,
		Match: []RouteMatch{
			{Host: []string{fmt.Sprintf("%s.localhost", appName)}},
		},
		Handle: []ReverseProxyHandle{
			{
				Handler: "reverse_proxy",
				Upstreams: []Upstream{
					{Dial: fmt.Sprintf("host.docker.internal:%d", hostPort)},
				},
			},
		},
	}

	data, err := json.Marshal(route)
	if err != nil {
		return fmt.Errorf("failed to serialize route: %w", err)
	}

	// 1. Try updating if route already exists via ID
	updateURL := fmt.Sprintf("%s/id/%s", c.adminURL, routeID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, updateURL, bytes.NewBuffer(data))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.httpClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil // Updated in-place with zero downtime!
			}
		}
	}

	// 2. If route doesn't exist, insert at the beginning of routes list (srv0/routes/0)
	insertURL := fmt.Sprintf("%s/config/apps/http/servers/srv0/routes/0", c.adminURL)
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, insertURL, bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	putReq.Header.Set("Content-Type", "application/json")

	putResp, err := c.httpClient.Do(putReq)
	if err != nil {
		return fmt.Errorf("failed to send route to caddy: %w", err)
	}
	defer putResp.Body.Close()

	if putResp.StatusCode >= 400 {
		body, _ := io.ReadAll(putResp.Body)
		return fmt.Errorf("caddy route insertion failed (%d): %s", putResp.StatusCode, string(body))
	}

	return nil
}

// RemoveRoute deletes a route by its ID when a container is destroyed.
// This prevents "orphaned routes" and eliminates 502 Bad Gateway errors.
func (c *CaddyClient) RemoveRoute(ctx context.Context, appName string) error {
	routeID := fmt.Sprintf("gantry-app-%s", appName)
	deleteURL := fmt.Sprintf("%s/id/%s", c.adminURL, routeID)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
