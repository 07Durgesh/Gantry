package models

import "time"

// DeploymentStatus represents the lifecycle state of a container deployment
type DeploymentStatus string

const (
	StatusQueued   DeploymentStatus = "QUEUED"
	StatusBuilding DeploymentStatus = "BUILDING"
	StatusStarting DeploymentStatus = "STARTING"
	StatusHealthy  DeploymentStatus = "HEALTHY"
	StatusFailed   DeploymentStatus = "FAILED"
	StatusStopped  DeploymentStatus = "STOPPED"
)

// Deployment tracks the lifecycle, metadata, and runtime state of an application deployment
type Deployment struct {
	ID            string           `json:"id"`
	AppName       string           `json:"app_name"`
	RepoPath      string           `json:"repo_path"`
	ImageTag      string           `json:"image_tag"`
	ContainerID   string           `json:"container_id,omitempty"`
	HostPort      int              `json:"host_port,omitempty"`
	ContainerPort int              `json:"container_port"`
	Subdomain     string           `json:"subdomain"`
	Status        DeploymentStatus `json:"status"`
	ErrorMessage  string           `json:"error_message,omitempty"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

// ContainerLimits defines cgroup resource constraints for safety and multi-tenancy
type ContainerLimits struct {
	MemoryBytes int64   // e.g. 256MB = 256 * 1024 * 1024
	CPUShares   int64   // relative weight
	NanoCPUs    int64   // CPU quota in 1e-9 CPUs (e.g. 0.5 vCPU = 500_000_000)
}
