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
}

func NewDeploymentStore() *DeploymentStore {
	return &DeploymentStore{
		deployments: make(map[string]*Deployment),
	}
}

func (s *DeploymentStore) Create(appName string, srcPath string) *Deployment {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := fmt.Sprintf("dep-%d", time.Now().UnixNano())
	dep := &Deployment{
		ID:            id,
		AppName:       appName,
		RepoPath:      srcPath,
		ImageTag:      fmt.Sprintf("gantry/%s:%s", appName, id[4:12]),
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
