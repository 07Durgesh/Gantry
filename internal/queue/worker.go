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
	"github.com/gantry-project/gantry/internal/models"
)

type Worker struct {
	docker  *engine.DockerEngine
	queue   *RedisQueue
	portMgr *engine.PortManager
	store   *models.DeploymentStore
}

func NewWorker(docker *engine.DockerEngine, queue *RedisQueue, portMgr *engine.PortManager, store *models.DeploymentStore) *Worker {
	return &Worker{
		docker:  docker,
		queue:   queue,
		portMgr: portMgr,
		store:   store,
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

	// Step 4: Run container
	w.store.UpdateStatus(depID, models.StatusStarting, "")
	containerName := fmt.Sprintf("gantry-%s-%s", job.AppName, depID[4:12])
	_ = w.queue.PublishLog(ctx, depID, fmt.Sprintf("[Gantry] 🚢 Starting container on host port %d...\n", hostPort))

	containerID, err := w.docker.RunContainer(ctx, dep.ImageTag, containerName, hostPort, dep.ContainerPort)
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
		w.failJob(ctx, job, fmt.Sprintf("Container failed healthcheck at %s", probeURL))
		return
	}
	_ = resp.Body.Close()

	// Step 6: Mark healthy and acknowledge job
	w.store.UpdateStatus(depID, models.StatusHealthy, "")
	_ = w.queue.PublishLog(ctx, depID, fmt.Sprintf("[Gantry] ✅ Application is healthy and live on port %d!\n", hostPort))
	_ = w.queue.PublishLog(ctx, depID, "[EOF]\n") // Signal stream termination to clients
	_ = w.queue.Acknowledge(ctx, job)
}

func (w *Worker) failJob(ctx context.Context, job *BuildJob, errMsg string) {
	w.store.UpdateStatus(job.DeploymentID, models.StatusFailed, errMsg)
	_ = w.queue.PublishLog(ctx, job.DeploymentID, fmt.Sprintf("[Gantry] ❌ ERROR: %s\n", errMsg))
	_ = w.queue.PublishLog(ctx, job.DeploymentID, "[EOF]\n")
	_ = w.queue.Acknowledge(ctx, job)
}
