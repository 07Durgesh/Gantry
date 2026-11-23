package models

import (
	"fmt"
	"sync"
	"time"
)

// DeploymentStore provides thread-safe in-memory state tracking for deployments.
type DeploymentStore struct {
	mu          sync.RWMutex
	deployments map[string]*Deployment
	activeApps  map[string]string // appName -> current active deploymentID (for Zero-Downtime Blue-Green)
}

func NewDeploymentStore() *DeploymentStore {
	return &DeploymentStore{
		deployments: make(map[string]*Deployment),
		activeApps:  make(map[string]string),
	}
}

func (s *DeploymentStore) Create(appName string, srcPath string) *Deployment {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := fmt.Sprintf("dep-%d", time.Now().UnixNano())
	suffix := id
	if len(id) > 8 {
		suffix = id[len(id)-8:]
	}
	dep := &Deployment{
		ID:            id,
		AppName:       appName,
		RepoPath:      srcPath,
		ImageTag:      fmt.Sprintf("gantry/%s:%s", appName, suffix),
		ContainerPort: 8080,
		Subdomain:     fmt.Sprintf("%s.localhost", appName),
		Status:        StatusQueued,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	s.deployments[id] = dep
	return dep
}

func (s *DeploymentStore) Get(id string) (*Deployment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dep, exists := s.deployments[id]
	return dep, exists
}

func (s *DeploymentStore) GetActiveApp(appName string) (*Deployment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	depID, exists := s.activeApps[appName]
	if !exists {
		return nil, false
	}
	dep, exists := s.deployments[depID]
	return dep, exists
}

func (s *DeploymentStore) SetActiveApp(appName string, depID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeApps[appName] = depID
}

func (s *DeploymentStore) UpdateStatus(id string, status DeploymentStatus, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if dep, exists := s.deployments[id]; exists {
		dep.Status = status
		dep.ErrorMessage = errMsg
		dep.UpdatedAt = time.Now()
	}
}

func (s *DeploymentStore) SetContainerDetails(id string, containerID string, hostPort int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if dep, exists := s.deployments[id]; exists {
		dep.ContainerID = containerID
		dep.HostPort = hostPort
		dep.UpdatedAt = time.Now()
	}
}
