package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/gantry-project/gantry/internal/models"
	"github.com/gantry-project/gantry/internal/queue"
)

// BuildQueue abstracts the build job queue and log pub/sub for the HTTP API.
type BuildQueue interface {
	Enqueue(ctx context.Context, job queue.BuildJob) error
	SubscribeLogs(ctx context.Context, deploymentID string) *redis.PubSub
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for dev/CLI
	},
}

type Server struct {
	queue BuildQueue
	store *models.DeploymentStore
	mux   *http.ServeMux
}

func NewServer(q BuildQueue, store *models.DeploymentStore) *Server {
	s := &Server{
		queue: q,
		store: store,
		mux:   http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/api/deploy", s.handleDeploy)
	s.mux.HandleFunc("/api/deployments", s.handleListDeployments)
	s.mux.HandleFunc("/api/deployments/", s.handleGetDeployment)
	s.mux.HandleFunc("/api/logs/", s.handleLogStreamWS)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

type DeployRequest struct {
	AppName string `json:"app_name"`
	SrcPath string `json:"src_path"`
}

type DeployResponse struct {
	DeploymentID string                  `json:"deployment_id"`
	AppName      string                  `json:"app_name"`
	Status       models.DeploymentStatus `json:"status"`
	Message      string                  `json:"message"`
}

// handleDeploy accepts a build request and returns HTTP 202 Accepted immediately.
//
// Analogy: The cashier taking your Starbucks order and handing you receipt #42 in 0.1 seconds.
func (s *Server) handleDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req DeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	if req.AppName == "" || req.SrcPath == "" {
		http.Error(w, "app_name and src_path are required", http.StatusBadRequest)
		return
	}

	// 1. Create deployment record with QUEUED status
	dep := s.store.Create(req.AppName, req.SrcPath)

	// 2. Put build job onto the Redis queue
	job := queue.BuildJob{
		DeploymentID: dep.ID,
		AppName:      req.AppName,
		SrcPath:      req.SrcPath,
		CreatedAt:    time.Now(),
	}

	if err := s.queue.Enqueue(r.Context(), job); err != nil {
		s.store.UpdateStatus(dep.ID, models.StatusFailed, "Failed to enqueue job")
		http.Error(w, fmt.Sprintf("Failed to enqueue build: %v", err), http.StatusInternalServerError)
		return
	}

	// 3. Return 202 Accepted immediately!
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(DeployResponse{
		DeploymentID: dep.ID,
		AppName:      dep.AppName,
		Status:       dep.Status,
		Message:      "Build job accepted and queued for worker processing",
	})
}

// handleHealth reports daemon readiness.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleListDeployments returns all deployments ordered from newest to oldest.
func (s *Server) handleListDeployments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.store.List())
}

// handleGetDeployment queries current status of a deployment.
func (s *Server) handleGetDeployment(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/deployments/")
	dep, ok := s.store.Get(id)
	if !ok {
		http.Error(w, "Deployment not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(dep)
}

// handleLogStreamWS upgrades HTTP to WebSocket and streams live build logs.
//
// Analogy: The overhead TV display at Starbucks showing your drink order status live.
func (s *Server) handleLogStreamWS(w http.ResponseWriter, r *http.Request) {
	// Path: /api/logs/{deployment_id}/ws
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/logs/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 || parts[1] != "ws" {
		http.Error(w, "Invalid path format. Use /api/logs/{id}/ws", http.StatusBadRequest)
		return
	}
	depID := parts[0]

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade websocket: %v", err)
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	pubsub := s.queue.SubscribeLogs(ctx, depID)
	defer pubsub.Close()

	ch := pubsub.Channel()

	// Stream logs to client with write deadline to prevent slow-consumer buffer leaks
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}

			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
				return // Client disconnected
			}

			// End of build stream
			if strings.TrimSpace(msg.Payload) == "[EOF]" {
				_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Build complete"))
				return
			}
		}
	}
}
