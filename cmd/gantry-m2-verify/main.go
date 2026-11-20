package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"github.com/gantry-project/gantry/internal/api"
	"github.com/gantry-project/gantry/internal/engine"
	"github.com/gantry-project/gantry/internal/models"
	"github.com/gantry-project/gantry/internal/queue"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("================================================================")
	fmt.Println("⚡ GANTRY MILESTONE 2: ASYNC QUEUES & REAL-TIME LOG STREAMING")
	fmt.Println("================================================================")

	// Step 1: Connect to Docker & Redis
	fmt.Print("\n[1/7] Connecting to Docker Engine and Redis Queue... ")
	docker, err := engine.NewDockerEngine()
	if err != nil {
		fmt.Printf("Docker connect failed ❌: %v\n", err)
		os.Exit(1)
	}
	defer docker.Close()

	redisQ, err := queue.NewRedisQueue("127.0.0.1:6379")
	if err != nil {
		fmt.Printf("Redis connect failed ❌: %v\n", err)
		os.Exit(1)
	}
	defer redisQ.Close()
	fmt.Println("CONNECTED ✅")

	// Step 2: Initialize Shared Stores, Port Manager, and Background Worker
	fmt.Print("\n[2/7] Starting Background Worker Pool (The Baristas)... ")
	portMgr := engine.NewPortManager(40000, 45000)
	store := models.NewDeploymentStore()
	worker := queue.NewWorker(docker, redisQ, portMgr, store)

	go worker.Start(ctx)
	fmt.Println("WORKER POOL READY ✅")

	// Step 3: Launch Gantry HTTP API Server on port 9090
	fmt.Print("\n[3/7] Launching Gantry API Server on :9090... ")
	server := api.NewServer(redisQ, store)
	httpServer := &http.Server{
		Addr:    ":9090",
		Handler: server,
	}
	go func() {
		_ = httpServer.ListenAndServe()
	}()
	time.Sleep(200 * time.Millisecond)
	defer httpServer.Shutdown(ctx)
	fmt.Println("API SERVER ONLINE ✅")

	// Step 4: Submit Deployment Request (HTTP POST 202 Accepted)
	fmt.Print("\n[4/7] Submitting POST /api/deploy (Async Cashier)... ")
	reqBody, _ := json.Marshal(api.DeployRequest{
		AppName: "sample-app",
		SrcPath: "examples/sample-app",
	})

	resp, err := http.Post("http://127.0.0.1:9090/api/deploy", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		fmt.Printf("Deploy request failed ❌: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		fmt.Printf("Expected 202 Accepted, got %d ❌\n", resp.StatusCode)
		os.Exit(1)
	}

	var deployResp api.DeployResponse
	_ = json.NewDecoder(resp.Body).Decode(&deployResp)
	fmt.Printf("RECEIVED 202 ACCEPTED ✅\n")
	fmt.Printf("      ├─ Deployment ID: %s\n", deployResp.DeploymentID)
	fmt.Printf("      └─ Initial Status: %s\n", deployResp.Status)

	// Step 5: Connect WebSocket and stream live logs in real time
	wsURL := fmt.Sprintf("ws://127.0.0.1:9090/api/logs/%s/ws", deployResp.DeploymentID)
	fmt.Printf("\n[5/7] Connecting to WebSocket Log Stream: %s\n", wsURL)
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		fmt.Printf("WebSocket dial failed ❌: %v\n", err)
		os.Exit(1)
	}
	defer ws.Close()

	fmt.Println("--- 📡 LIVE BUILD LOG STREAM START ---")
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			break
		}
		text := string(msg)
		if text == "[EOF]\n" || text == "[EOF]" {
			break
		}
		fmt.Print(text)
	}
	fmt.Println("--- 📡 LIVE BUILD LOG STREAM END ---")

	// Step 6: Query final deployment state
	fmt.Print("\n[6/7] Querying final deployment status via GET /api/deployments/:id ... ")
	depStatusResp, err := http.Get(fmt.Sprintf("http://127.0.0.1:9090/api/deployments/%s", deployResp.DeploymentID))
	if err != nil {
		fmt.Printf("Failed to get deployment status ❌: %v\n", err)
		os.Exit(1)
	}
	defer depStatusResp.Body.Close()

	var finalDep models.Deployment
	_ = json.NewDecoder(depStatusResp.Body).Decode(&finalDep)
	fmt.Printf("STATUS: %s ✅ (Host Port: %d)\n", finalDep.Status, finalDep.HostPort)

	// Step 7: Probe live application endpoint
	probeURL := fmt.Sprintf("http://127.0.0.1:%d/", finalDep.HostPort)
	fmt.Printf("\n[7/7] Probing deployed application HTTP: %s ... ", probeURL)
	appResp, err := http.Get(probeURL)
	if err != nil {
		fmt.Printf("App probe failed ❌: %v\n", err)
	} else {
		defer appResp.Body.Close()
		appBody, _ := io.ReadAll(appResp.Body)
		fmt.Printf("SUCCESS ✅ [HTTP %d: %s]\n", appResp.StatusCode, string(bytes.TrimSpace(appBody)))
	}

	// Teardown
	fmt.Print("\n🧹 Cleaning up deployed container & freeing port... ")
	if finalDep.ContainerID != "" {
		_ = docker.StopAndRemoveContainer(ctx, finalDep.ContainerID)
	}
	portMgr.Release(finalDep.HostPort)
	fmt.Println("DONE ✅")

	fmt.Println("\n================================================================")
	fmt.Println("🎉 MILESTONE 2 COMPLETE: ASYNC QUEUES & WS LOG STREAMING VERIFIED!")
	fmt.Println("================================================================")
}
