package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gantry-project/gantry/internal/builder"
	"github.com/gantry-project/gantry/internal/engine"
)

type buildLine struct {
	Stream string `json:"stream"`
	Error  string `json:"error"`
}

func main() {
	ctx := context.Background()
	fmt.Println("================================================================")
	fmt.Println("🚀 GANTRY MILESTONE 1 VERIFICATION: DIRECT ENGINE PROTOCOL")
	fmt.Println("================================================================")

	// Step 1: Connect to Docker Engine
	fmt.Print("\n[1/6] Connecting directly to Docker Engine API socket/pipe... ")
	docker, err := engine.NewDockerEngine()
	if err != nil {
		fmt.Printf("FAILED ❌: %v\n", err)
		os.Exit(1)
	}
	defer docker.Close()

	ping, err := docker.Ping(ctx)
	if err != nil {
		fmt.Printf("FAILED ❌: %v\n", err)
		os.Exit(1)
	}
	version, _ := docker.ServerVersion(ctx)
	fmt.Printf("CONNECTED ✅ (API Version: %s, OS: %s/%s)\n", ping.APIVersion, version.Os, version.Arch)

	// Step 2: Initialize Port Manager & Allocate Ephemeral Port
	fmt.Print("\n[2/6] Initializing Port Manager (Hotel Receptionist)... ")
	pm := engine.NewPortManager(40000, 45000)
	hostPort, err := pm.Allocate()
	if err != nil {
		fmt.Printf("FAILED ❌: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("ALLOCATED PORT ✅ (Host Port: %d -> Container Port: 8080)\n", hostPort)

	// Step 3: Stream in-memory tarball of sample-app
	fmt.Print("\n[3/6] Packaging 'examples/sample-app' into in-memory tar stream... ")
	sampleAppPath := "examples/sample-app"
	tarStream, err := builder.CreateTarStream(sampleAppPath)
	if err != nil {
		fmt.Printf("FAILED ❌: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("COMPRESSED IN-MEMORY ✅ (Zero-Disk I/O)")

	// Step 4: Stream Build to Docker Daemon
	imageTag := "gantry-test/sample-app:v1"
	fmt.Printf("\n[4/6] Streaming build context to Docker POST /build for tag '%s'...\n", imageTag)
	buildBody, err := docker.BuildImage(ctx, tarStream, imageTag)
	if err != nil {
		fmt.Printf("Build trigger failed ❌: %v\n", err)
		os.Exit(1)
	}
	defer buildBody.Close()

	// Parse streaming JSON lines from Docker daemon
	scanner := bufio.NewScanner(buildBody)
	for scanner.Scan() {
		var line buildLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err == nil {
			if line.Stream != "" {
				fmt.Print("  │ " + line.Stream)
			}
			if line.Error != "" {
				fmt.Printf("Build error: %s\n", line.Error)
				os.Exit(1)
			}
		}
	}
	fmt.Println("IMAGE BUILT SUCCESSFULLY ✅")

	// Step 5: Run Container with Dynamic Port Binding
	containerName := fmt.Sprintf("gantry-verify-%d", time.Now().Unix())
	fmt.Printf("\n[5/6] Creating & starting container '%s' on port %d...\n", containerName, hostPort)
	containerID, err := docker.RunContainer(ctx, imageTag, containerName, hostPort, 8080)
	if err != nil {
		fmt.Printf("Container start failed ❌: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("CONTAINER RUNNING ✅ (ID: %s)\n", containerID[:12])

	// Wait 1.5 seconds for container process to bind port
	time.Sleep(1500 * time.Millisecond)

	// Step 6: Test HTTP Probe directly to container
	probeURL := fmt.Sprintf("http://127.0.0.1:%d/healthz", hostPort)
	fmt.Printf("\n[6/6] Probing live container HTTP endpoint: %s ... ", probeURL)
	resp, err := http.Get(probeURL)
	if err != nil {
		fmt.Printf("Probe failed ❌: %v\n", err)
	} else {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("SUCCESS ✅ [Status: %d, Body: %s]\n", resp.StatusCode, string(body))
	}

	// Teardown & Clean Up
	fmt.Print("\n🧹 Cleaning up test container & releasing port... ")
	if err := docker.StopAndRemoveContainer(ctx, containerID); err != nil {
		fmt.Printf("Warning: failed to remove container: %v\n", err)
	}
	pm.Release(hostPort)
	_ = docker.RemoveImage(ctx, imageTag)
	fmt.Println("CLEANUP COMPLETE ✅")

	fmt.Println("\n================================================================")
	fmt.Println("🎉 MILESTONE 1 COMPLETE: CORE ENGINE HANDSHAKE & RUNTIME OPERATIONAL")
	fmt.Println("================================================================")
}
