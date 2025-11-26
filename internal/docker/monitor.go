package docker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"

	"watchcow/internal/fpkgen"
)

// Monitor watches Docker containers and manages fnOS app installation
type Monitor struct {
	cli       *client.Client
	generator *fpkgen.Generator
	installer *fpkgen.Installer
	outputDir string
	stopCh    chan struct{}

	// Track container states
	containers map[string]*ContainerState // map[containerID]state
	mu         sync.RWMutex
}

// ContainerState tracks the state of a monitored container
type ContainerState struct {
	ContainerID   string
	ContainerName string
	AppName       string
	Installed     bool
	Labels        map[string]string
}

// NewMonitor creates a new Docker monitor
func NewMonitor(outputDir string) (*Monitor, error) {
	// Connect to Docker daemon
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	// Create generator
	generator, err := fpkgen.NewGenerator(outputDir)
	if err != nil {
		cli.Close()
		return nil, fmt.Errorf("failed to create generator: %w", err)
	}

	// Try to create installer (may fail if appcenter-cli not available)
	installer, err := fpkgen.NewInstaller()
	if err != nil {
		slog.Warn("appcenter-cli not available, will only generate app packages", "error", err)
		// Continue without installer - useful for development/testing
	} else {
		slog.Info("Installer ready, apps will be auto-installed via appcenter-cli")
	}

	return &Monitor{
		cli:        cli,
		generator:  generator,
		installer:  installer,
		outputDir:  outputDir,
		stopCh:     make(chan struct{}),
		containers: make(map[string]*ContainerState),
	}, nil
}

// Start starts monitoring Docker containers
func (m *Monitor) Start(ctx context.Context) {
	slog.Info("Starting Docker monitor...", "outputDir", m.outputDir)

	// Initial scan to process existing containers
	m.scanContainers(ctx)

	// Start listening to Docker events for real-time updates
	go m.listenToDockerEvents(ctx)
}

// listenToDockerEvents listens to Docker daemon events
func (m *Monitor) listenToDockerEvents(ctx context.Context) {
	// Set up event filters
	eventFilters := filters.NewArgs()
	eventFilters.Add("type", "container")
	eventFilters.Add("event", "start")
	eventFilters.Add("event", "stop")
	eventFilters.Add("event", "die")
	eventFilters.Add("event", "destroy")

	eventChan, errChan := m.cli.Events(ctx, events.ListOptions{
		Filters: eventFilters,
	})

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case err := <-errChan:
			if err != nil {
				slog.Warn("Docker event stream error, reconnecting...", "error", err)
				time.Sleep(5 * time.Second)
				go m.listenToDockerEvents(ctx)
				return
			}
		case event := <-eventChan:
			m.handleDockerEvent(ctx, event)
		}
	}
}

// handleDockerEvent processes a Docker event
func (m *Monitor) handleDockerEvent(ctx context.Context, event events.Message) {
	containerName := event.Actor.Attributes["name"]
	containerID := event.Actor.ID
	if len(containerID) > 12 {
		containerID = containerID[:12]
	}

	switch event.Action {
	case "start":
		slog.Info("Container started", "container", containerName, "id", containerID)

		// Check if this container should be managed
		labels := event.Actor.Attributes
		if shouldInstall(labels) {
			go m.handleContainerStart(ctx, containerID, containerName, labels)
		}

	case "stop", "die", "destroy":
		slog.Info("Container stopped", "container", containerName, "id", containerID)
		m.handleContainerStop(ctx, containerID, containerName)
	}
}

// shouldInstall checks if a container should be installed as fnOS app
func shouldInstall(labels map[string]string) bool {
	// Check watchcow.enable label
	if labels["watchcow.enable"] != "true" {
		return false
	}

	// Check watchcow.install label (default to "fnos" if enable is true)
	installMode := labels["watchcow.install"]
	return installMode == "fnos" || installMode == "true" || installMode == ""
}

// handleContainerStart handles container start event
func (m *Monitor) handleContainerStart(ctx context.Context, containerID, containerName string, labels map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already tracked in memory
	if state, exists := m.containers[containerID]; exists && state.Installed {
		slog.Debug("Container already tracked as installed", "container", containerName)
		return
	}

	// Wait a moment for container to fully start
	time.Sleep(2 * time.Second)

	// Generate fnOS app package (to get appName)
	config, appDir, err := m.generator.GenerateFromContainer(ctx, containerID)
	if err != nil {
		slog.Error("Failed to generate fnOS app", "container", containerName, "error", err)
		return
	}

	// Check if already installed in fnOS (handles WatchCow restart case)
	if m.installer != nil && m.installer.IsAppInstalled(config.AppName) {
		slog.Info("App already installed in fnOS, skipping", "app", config.AppName, "container", containerName)
		// Track it in memory
		m.containers[containerID] = &ContainerState{
			ContainerID:   containerID,
			ContainerName: containerName,
			AppName:       config.AppName,
			Installed:     true,
			Labels:        labels,
		}
		m.generator.MarkInstalled(containerID, config)
		return
	}

	// Record state
	m.containers[containerID] = &ContainerState{
		ContainerID:   containerID,
		ContainerName: containerName,
		AppName:       config.AppName,
		Installed:     false,
		Labels:        labels,
	}

	// Install if installer is available
	if m.installer != nil {
		if err := m.installer.InstallLocal(appDir); err != nil {
			slog.Error("Failed to install fnOS app", "app", config.AppName, "error", err)
			return
		}
		m.containers[containerID].Installed = true
		slog.Info("Successfully installed fnOS app", "app", config.AppName, "container", containerName)
	} else {
		slog.Info("Generated fnOS app package (appcenter-cli not available)",
			"app", config.AppName,
			"appDir", appDir)
	}

	// Mark as installed in generator
	m.generator.MarkInstalled(containerID, config)
}

// handleContainerStop handles container stop event
func (m *Monitor) handleContainerStop(ctx context.Context, containerID, containerName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, exists := m.containers[containerID]
	if !exists {
		return
	}

	// Check if keep-on-stop is enabled (default: uninstall on container removal)
	// Use watchcow.keepOnStop=true to keep the fnOS app when container stops
	if state.Labels["watchcow.keepOnStop"] == "true" {
		slog.Info("Keeping fnOS app (watchcow.keepOnStop=true)", "app", state.AppName)
		return
	}

	// Uninstall fnOS app
	if m.installer != nil && state.Installed {
		slog.Info("Uninstalling fnOS app", "app", state.AppName, "container", containerName)
		if err := m.installer.Uninstall(state.AppName); err != nil {
			slog.Warn("Failed to uninstall fnOS app", "app", state.AppName, "error", err)
		}
	}

	// Clean up generated directory
	appDir := fpkgen.GetAppDir(m.outputDir, state.AppName)
	if err := fpkgen.CleanupAppDir(appDir); err != nil {
		slog.Warn("Failed to cleanup app directory", "appDir", appDir, "error", err)
	}

	// Remove from tracking
	delete(m.containers, containerID)
	m.generator.MarkUninstalled(containerID)
}

// scanContainers scans all running containers
func (m *Monitor) scanContainers(ctx context.Context) {
	containers, err := m.cli.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		slog.Error("Failed to list containers", "error", err)
		return
	}

	slog.Info("Scanning existing containers...", "count", len(containers))

	for _, ctr := range containers {
		containerID := ctr.ID[:12]
		containerName := strings.TrimPrefix(ctr.Names[0], "/")

		// Check if should be installed
		if shouldInstall(ctr.Labels) {
			slog.Info("Found container to install", "container", containerName)
			go m.handleContainerStart(ctx, containerID, containerName, ctr.Labels)
		}
	}
}

// GetContainerStates returns all monitored container states
func (m *Monitor) GetContainerStates() map[string]*ContainerState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*ContainerState)
	for k, v := range m.containers {
		result[k] = v
	}
	return result
}

// Stop stops the monitor
func (m *Monitor) Stop() {
	close(m.stopCh)

	if m.generator != nil {
		m.generator.Close()
	}

	if m.cli != nil {
		if err := m.cli.Close(); err != nil {
			slog.Warn("Error closing Docker client", "error", err)
		}
	}
}
