package engine

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/gantry-project/gantry/internal/models"
)

// DockerEngine wraps the official Docker client SDK to manage images and containers.
type DockerEngine struct {
	cli *client.Client
}

// NewDockerEngine creates a new connection to the Docker daemon.
// It automatically detects whether to use Windows Named Pipes or Unix sockets.
func NewDockerEngine() (*DockerEngine, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize docker client: %w", err)
	}

	return &DockerEngine{cli: cli}, nil
}

// Ping checks if the Docker daemon is alive and responding.
func (de *DockerEngine) Ping(ctx context.Context) (types.Ping, error) {
	return de.cli.Ping(ctx)
}

// ServerVersion returns the version info of the running Docker engine.
func (de *DockerEngine) ServerVersion(ctx context.Context) (types.Version, error) {
	return de.cli.ServerVersion(ctx)
}

// BuildImage sends an in-memory tar stream to the Docker daemon and builds an image.
// It returns an io.ReadCloser containing the build output stream (JSON-formatted build steps).
func (de *DockerEngine) BuildImage(ctx context.Context, tarStream io.Reader, tag string) (io.ReadCloser, error) {
	opts := types.ImageBuildOptions{
		Tags:       []string{tag},
		Dockerfile: "Dockerfile",
		Remove:     true, // Remove intermediate build containers
		NoCache:    false,
	}

	resp, err := de.cli.ImageBuild(ctx, tarStream, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to build image: %w", err)
	}

	return resp.Body, nil
}

// RunContainer creates and starts a container with port binding and default resource limits.
func (de *DockerEngine) RunContainer(ctx context.Context, imageName string, containerName string, hostPort int, containerPort int) (string, error) {
	// Default limits: 256MB RAM, 0.5 vCPU
	defaultLimits := &models.ContainerLimits{
		MemoryBytes: 256 * 1024 * 1024,
		NanoCPUs:    500_000_000,
	}
	return de.RunContainerWithLimits(ctx, imageName, containerName, hostPort, containerPort, defaultLimits)
}

// RunContainerWithLimits creates and starts a container enforcing hard cgroup memory and CPU constraints.
func (de *DockerEngine) RunContainerWithLimits(ctx context.Context, imageName string, containerName string, hostPort int, containerPort int, limits *models.ContainerLimits) (string, error) {
	targetPort := nat.Port(fmt.Sprintf("%d/tcp", containerPort))

	config := &container.Config{
		Image: imageName,
		ExposedPorts: nat.PortSet{
			targetPort: struct{}{},
		},
		Env: []string{
			fmt.Sprintf("PORT=%d", containerPort),
		},
	}

	var res container.Resources
	if limits != nil {
		res = container.Resources{
			Memory:   limits.MemoryBytes,
			NanoCPUs: limits.NanoCPUs,
		}
	}

	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			targetPort: []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: strconv.Itoa(hostPort),
				},
			},
		},
		Resources: res,
		RestartPolicy: container.RestartPolicy{
			Name: "no",
		},
	}

	createResp, err := de.cli.ContainerCreate(ctx, config, hostConfig, nil, nil, containerName)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	if err := de.cli.ContainerStart(ctx, createResp.ID, container.StartOptions{}); err != nil {
		// Clean up container if it fails to start
		_ = de.cli.ContainerRemove(ctx, createResp.ID, container.RemoveOptions{Force: true})
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	return createResp.ID, nil
}

// CheckContainerOOM inspects if a container crashed due to Linux cgroup memory exhaustion (Exit Code 137).
func (de *DockerEngine) CheckContainerOOM(ctx context.Context, containerID string) (bool, error) {
	inspect, err := de.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return false, err
	}
	if inspect.State != nil {
		if inspect.State.OOMKilled || inspect.State.ExitCode == 137 {
			return true, nil
		}
	}
	return false, nil
}

// StopAndRemoveContainer gracefully stops a running container and deletes it.
func (de *DockerEngine) StopAndRemoveContainer(ctx context.Context, containerID string) error {
	timeoutSeconds := 5
	stopOpts := container.StopOptions{
		Timeout: &timeoutSeconds,
	}

	// Attempt graceful stop
	if err := de.cli.ContainerStop(ctx, containerID, stopOpts); err != nil {
		// If already stopped, ignore error
	}

	// Remove container
	removeOpts := container.RemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	}
	return de.cli.ContainerRemove(ctx, containerID, removeOpts)
}

// InspectContainer retrieves current status and details of a container.
func (de *DockerEngine) InspectContainer(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	return de.cli.ContainerInspect(ctx, containerID)
}

// RemoveImage deletes an image by tag.
func (de *DockerEngine) RemoveImage(ctx context.Context, imageTag string) error {
	_, err := de.cli.ImageRemove(ctx, imageTag, image.RemoveOptions{Force: true, PruneChildren: true})
	return err
}

// Close closes the underlying client transport.
func (de *DockerEngine) Close() error {
	return de.cli.Close()
}
