package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gorilla/websocket"
	"github.com/gantry-project/gantry/internal/api"
	"github.com/gantry-project/gantry/internal/models"
)

func defaultServerURL() string {
	if v := os.Getenv("GANTRY_SERVER"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://127.0.0.1:9090"
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "deploy":
		err = runDeploy(os.Args[2:])
	case "logs":
		err = runLogs(os.Args[2:])
	case "ps":
		err = runPS(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`Gantry CLI - container deployment & dynamic ingress engine

Usage:
  gantry <command> [flags] [args]

Commands:
  deploy [path]   Build and deploy an application directory (default: .)
  logs <id>       Stream live build logs for a deployment ID
  ps              List tracked deployments and their status
  help            Show this help message

Environment:
  GANTRY_SERVER   Base URL of gantryd (default: http://127.0.0.1:9090)
`)
}

func runDeploy(args []string) error {
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	serverURL := fs.String("server", defaultServerURL(), "gantryd server URL")
	appName := fs.String("name", "", "application name (defaults to directory name)")
	detach := fs.Bool("detach", false, "return immediately after queuing without streaming logs")
	if err := fs.Parse(args); err != nil {
		return err
	}

	targetPath := "."
	if fs.NArg() > 0 {
		targetPath = fs.Arg(0)
	}

	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("source path %q is not an accessible directory", absPath)
	}

	name := strings.TrimSpace(*appName)
	if name == "" {
		name = sanitizeAppName(filepath.Base(absPath))
	}

	payload, err := json.Marshal(api.DeployRequest{
		AppName: name,
		SrcPath: absPath,
	})
	if err != nil {
		return err
	}

	base := strings.TrimRight(*serverURL, "/")
	resp, err := http.Post(base+"/api/deploy", "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("connect to gantryd at %s: %w", base, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("deploy rejected (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var deployResp api.DeployResponse
	if err := json.NewDecoder(resp.Body).Decode(&deployResp); err != nil {
		return fmt.Errorf("decode deploy response: %w", err)
	}

	fmt.Printf("Queued deployment %s for app %q (%s)\n", deployResp.DeploymentID, deployResp.AppName, deployResp.Status)
	if *detach {
		return nil
	}

	if err := streamLogs(base, deployResp.DeploymentID); err != nil {
		return err
	}

	dep, err := fetchDeployment(base, deployResp.DeploymentID)
	if err != nil {
		return err
	}
	if dep.Status == models.StatusFailed {
		return fmt.Errorf("deployment %s failed: %s", dep.ID, dep.ErrorMessage)
	}
	fmt.Printf("\nLive: http://%s (port %d, status %s)\n", dep.Subdomain, dep.HostPort, dep.Status)
	return nil
}

func runLogs(args []string) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	serverURL := fs.String("server", defaultServerURL(), "gantryd server URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: gantry logs <deployment-id>")
	}
	return streamLogs(strings.TrimRight(*serverURL, "/"), fs.Arg(0))
}

func runPS(args []string) error {
	fs := flag.NewFlagSet("ps", flag.ContinueOnError)
	serverURL := fs.String("server", defaultServerURL(), "gantryd server URL")
	if err := fs.Parse(args); err != nil {
		return err
	}

	base := strings.TrimRight(*serverURL, "/")
	resp, err := http.Get(base + "/api/deployments")
	if err != nil {
		return fmt.Errorf("connect to gantryd at %s: %w", base, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("list deployments failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var deps []*models.Deployment
	if err := json.NewDecoder(resp.Body).Decode(&deps); err != nil {
		return fmt.Errorf("decode deployments: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tAPP\tSTATUS\tPORT\tURL\tAGE")
	for _, d := range deps {
		portStr := "-"
		if d.HostPort > 0 {
			portStr = fmt.Sprintf("%d", d.HostPort)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\thttp://%s\t%s\n",
			d.ID, d.AppName, d.Status, portStr, d.Subdomain, formatAge(time.Since(d.CreatedAt)))
	}
	return w.Flush()
}

func streamLogs(baseURL, depID string) error {
	wsURL, err := toWebSocketURL(baseURL, fmt.Sprintf("/api/logs/%s/ws", depID))
	if err != nil {
		return err
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("connect log stream %s: %w", wsURL, err)
	}
	defer conn.Close()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				return nil
			}
			return nil
		}
		text := string(msg)
		if strings.TrimSpace(text) == "[EOF]" {
			return nil
		}
		fmt.Print(text)
	}
}

func fetchDeployment(baseURL, depID string) (*models.Deployment, error) {
	resp, err := http.Get(fmt.Sprintf("%s/api/deployments/%s", baseURL, depID))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch deployment %s returned HTTP %d", depID, resp.StatusCode)
	}
	var dep models.Deployment
	if err := json.NewDecoder(resp.Body).Decode(&dep); err != nil {
		return nil, err
	}
	return &dep, nil
}

func toWebSocketURL(baseURL, path string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	return u.String(), nil
}

func sanitizeAppName(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	if s == "" {
		return "app"
	}
	return s
}

func formatAge(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}
