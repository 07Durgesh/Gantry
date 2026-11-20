package engine

import (
	"fmt"
	"net"
	"sync"
)

// PortManager acts like a hotel receptionist for network ports.
// It hands out free ports to running containers and ensures no two containers
// ever fight over the same host port.
type PortManager struct {
	mu        sync.Mutex
	minPort   int
	maxPort   int
	allocated map[int]bool
}

// NewPortManager creates a port allocator within a safe, unprivileged range (e.g. 40000 to 49999).
func NewPortManager(minPort, maxPort int) *PortManager {
	return &PortManager{
		minPort:   minPort,
		maxPort:   maxPort,
		allocated: make(map[int]bool),
	}
}

// Allocate finds the next available port that is both:
// 1. Not in Gantry's internal ledger.
// 2. Not currently occupied by another program on the host OS.
func (pm *PortManager) Allocate() (int, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for port := pm.minPort; port <= pm.maxPort; port++ {
		if pm.allocated[port] {
			continue // Already assigned to another container
		}

		// Double-check if the operating system has this port free
		if isPortAvailableOnHost(port) {
			pm.allocated[port] = true
			return port, nil
		}
	}

	return 0, fmt.Errorf("port exhaustion: no available ports in range %d-%d", pm.minPort, pm.maxPort)
}

// Release marks a port as free when a container is stopped or destroyed.
func (pm *PortManager) Release(port int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.allocated, port)
}

// isPortAvailableOnHost tests if the OS can actually bind to this port right now.
func isPortAvailableOnHost(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}
