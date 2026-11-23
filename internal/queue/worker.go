package queue

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gantry-project/gantry/internal/builder"
	"github.com/gantry-project/gantry/internal/engine"
	"github.com/gantry-project/gantry/internal/ingress"
	"github.com/gantry-project/gantry/internal/models"
)

type Worker struct {
	docker  *engine.DockerEngine
	queue   *RedisQueue
	portMgr *engine.PortManager
	store   *models.DeploymentStore
	caddy   *ingress.CaddyClient
}

func NewWorker(docker *engine.DockerEngine, queue *RedisQueue, portMgr *engine.PortManager, store *models.DeploymentStore, caddy *ingress.CaddyClient) *Worker {
	return &Worker{
		docker:  docker,
		queue:   queue,
		portMgr: portMgr,
		store:   store,
		caddy:   caddy,
	}
}

// Start runs the worker event loop, continuously pulling jobs from Redis.
func (w *Worker) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Wait up to 2 seconds for a new job
			job, err := w.queue.Dequeue(ctx, 2*time.Second)
			if err != nil {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			if job == nil {
				continue
			}

			w.processJob(ctx, job)
		}
	}
}

func (w *Worker) processJob(ctx context.Context, job *BuildJob) {
	depID := job.DeploymentID
	w.store.UpdateStatus(depID, models.StatusBuilding, "")
	_ = w.queue.PublishLog(ctx, depID, fmt.Sprintf("[Gantry] 🚀 Picked up build job for app '%s'\n", job.AppName))

	dep, ok := w.store.Get(depID)
	if !ok {
		return
	}

	// Step 1: In-memory tarball streaming
	_ = w.queue.PublishLog(ctx, depID, "[Gantry] 📦 Compressing source files in-memory...\n")
	tarStream, err := builder.CreateTarStream(job.SrcPath)
	if err != nil {
		w.failJob(ctx, job, fmt.Sprintf("Failed to package source: %v", err))
		return
	}

	// Step 2: Trigger Docker image build & stream logs line by line
	_ = w.queue.PublishLog(ctx, depID, fmt.Sprintf("[Gantry] 🔨 Building Docker image: %s\n", dep.ImageTag))
	buildBody, err := w.docker.BuildImage(ctx, tarStream, dep.ImageTag)
	if err != nil {
		w.failJob(ctx, job, fmt.Sprintf("Docker build trigger failed: %v", err))
		return
	}
	defer buildBody.Close()

	scanner := bufio.NewScanner(buildBody)
	for scanner.Scan() {
		var line struct {
			Stream string `json:"stream"`
			Error  string `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err == nil {
			if line.Stream != "" {
				_ = w.queue.PublishLog(ctx, depID, line.Stream)
			}
			if line.Error != "" {
				w.failJob(ctx, job, fmt.Sprintf("Build failed: %s", line.Error))
				return
			}
		}
	}

	// Step 3: Dynamic port allocation
	_ = w.queue.PublishLog(ctx, depID, "[Gantry] 🏨 Allocating dynamic host port...\n")
	hostPort, err := w.portMgr.Allocate()
	if err != nil {
		w.failJob(ctx, job, fmt.Sprintf("Port allocation failed: %v", err))
		return
	}

	// Step 4: Run container with cgroups resource constraints (256MB RAM, 0.5 vCPU)
	w.store.UpdateStatus(depID, models.StatusStarting, "")
	suffix := depID
	if len(depID) > 8 {
		suffix = depID[len(depID)-8:]
	}
	containerName := fmt.Sprintf("gantry-%s-%s", job.AppName, suffix)
	_ = w.queue.PublishLog(ctx, depID, fmt.Sprintf("[Gantry] 🚢 Starting container (Limits: 256MB RAM, 0.5 vCPU) on host port %d...\n", hostPort))

	limits := &models.ContainerLimits{
		MemoryBytes: 256 * 1024 * 1024, // 256MB
		NanoCPUs:    500_000_000,       // 0.5 CPU
	}
	containerID, err := w.docker.RunContainerWithLimits(ctx, dep.ImageTag, containerName, hostPort, dep.ContainerPort, limits)
	if err != nil {
		w.portMgr.Release(hostPort)
		w.failJob(ctx, job, fmt.Sprintf("Container run failed: %v", err))
		return
	}

	w.store.SetContainerDetails(depID, containerID, hostPort)

	// Step 5: Health check verification (polling)
	_ = w.queue.PublishLog(ctx, depID, "[Gantry] 🩺 Performing container health check...\n")
	time.Sleep(1 * time.Second)

	probeURL := fmt.Sprintf("http://127.0.0.1:%d/healthz", hostPort)
	resp, err := http.Get(probeURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		// Inspect if container was killed by OOM
		oom, _ := w.docker.CheckContainerOOM(ctx, containerID)
		errMsg := fmt.Sprintf("Container failed healthcheck at %s", probeURL)
		if oom {
			errMsg = "Container exceeded memory cap of 256MB and was terminated by the OOM-killer (Exit Code 137)"
		}
		w.failJob(ctx, job, errMsg)
		return
	}
	_ = resp.Body.Close()

	// Step 6: Dynamic Ingress Registration in Caddy (Zero-Downtime Switchover!)
	if w.caddy != nil {
		_ = w.queue.PublishLog(ctx, depID, fmt.Sprintf("[Gantry] 🔀 Registering ingress route: %s.localhost -> port %d\n", job.AppName, hostPort))
		if err := w.caddy.RegisterRoute(ctx, job.AppName, hostPort); err != nil {
			_ = w.queue.PublishLog(ctx, depID, fmt.Sprintf("[Gantry] ⚠️ Ingress route registration warning: %v\n", err))
		}
	}

	// Step 7: Blue-Green Decommissioning of Old Version (Zero Downtime)
	oldDep, hasOld := w.store.GetActiveApp(job.AppName)
	if hasOld && oldDep.ID != depID {
		_ = w.queue.PublishLog(ctx, depID, fmt.Sprintf("[Gantry] 🔄 Blue-Green Switchover complete! Decommissioning old container %s (port %d)...\n", oldDep.ID, oldDep.HostPort))
		_ = w.docker.StopAndRemoveContainer(ctx, oldDep.ContainerID)
		w.portMgr.Release(oldDep.HostPort)
		w.store.UpdateStatus(oldDep.ID, models.StatusStopped, "Superseded by newer deployment")
	}

	// Mark new deployment as the currently active version for this app
	w.store.SetActiveApp(job.AppName, depID)

	// Step 8: Final success state & acknowledge queue job
	w.store.UpdateStatus(depID, models.StatusHealthy, "")
	_ = w.queue.PublishLog(ctx, depID, fmt.Sprintf("[Gantry] ✅ Application is healthy and live at http://%s.localhost!\n", job.AppName))
	_ = w.queue.PublishLog(ctx, depID, "[EOF]\n")
	_ = w.queue.Acknowledge(ctx, job)
}

func (w *Worker) failJob(ctx context.Context, job *BuildJob, errMsg string) {
	w.store.UpdateStatus(job.DeploymentID, models.StatusFailed, errMsg)
	_ = w.queue.PublishLog(ctx, job.DeploymentID, fmt.Sprintf("[Gantry] ❌ ERROR: %s\n", errMsg))
	_ = w.queue.PublishLog(ctx, job.DeploymentID, "[EOF]\n")
	_ = w.queue.Acknowledge(ctx, job)
}
