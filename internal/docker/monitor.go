package docker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"

	"watchcow/internal/fpkgen"
)

// AppOperation represents an appcenter-cli operation
type AppOperation struct {
	Type     string // "install", "stop", "uninstall"
	AppName  string
	AppDir   string
	ResultCh chan error
}

// Monitor watches Docker containers and manages fnOS app installation
type Monitor struct {
	cli       *client.Client
	generator *fpkgen.Generator
	installer *fpkgen.Installer
	stopCh    chan struct{}

	// Track container states
	containers map[string]*ContainerState // map[containerID]state
	mu         sync.RWMutex

	// Operation queue for serializing appcenter-cli calls
	opQueue chan *AppOperation
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
func NewMonitor() (*Monitor, error) {
	// Connect to Docker daemon
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	// Create generator
	generator, err := fpkgen.NewGenerator()
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
		stopCh:     make(chan struct{}),
		containers: make(map[string]*ContainerState),
		opQueue:    make(chan *AppOperation, 100),
	}, nil
}

// runOperationWorker processes appcenter-cli operations sequentially
func (m *Monitor) runOperationWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case op := <-m.opQueue:
			var err error
			switch op.Type {
			case "install":
				slog.Info("Installing fnOS app", "app", op.AppName)
				err = m.installer.InstallLocal(op.AppDir)
				// Clean up temp directory after install (success or fail)
				os.RemoveAll(op.AppDir)
			case "start":
				slog.Info("Starting fnOS app", "app", op.AppName)
				err = m.installer.StartApp(op.AppName)
			case "stop":
				slog.Info("Stopping fnOS app", "app", op.AppName)
				err = m.installer.StopApp(op.AppName)
			case "uninstall":
				slog.Info("Uninstalling fnOS app", "app", op.AppName)
				err = m.installer.Uninstall(op.AppName)
			}
			if op.ResultCh != nil {
				op.ResultCh <- err
			}
		}
	}
}

// queueOperation sends an operation to the worker and waits for result
func (m *Monitor) queueOperation(opType, appName, appDir string) error {
	if m.installer == nil {
		return nil
	}
	resultCh := make(chan error, 1)
	m.opQueue <- &AppOperation{
		Type:     opType,
		AppName:  appName,
		AppDir:   appDir,
		ResultCh: resultCh,
	}
	return <-resultCh
}

// Start starts monitoring Docker containers
func (m *Monitor) Start(ctx context.Context) {
	slog.Info("Starting Docker monitor...")

	// Start operation worker for serializing appcenter-cli calls
	if m.installer != nil {
		go m.runOperationWorker(ctx)
	}

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

		// Inspect container to get full labels (event.Actor.Attributes is incomplete)
		info, err := m.cli.ContainerInspect(ctx, containerID)
		if err != nil {
			slog.Debug("Failed to inspect container", "container", containerName, "error", err)
			return
		}

		labels := info.Config.Labels
		if shouldInstall(labels) {
			go m.handleContainerStart(ctx, containerID, containerName, labels)
		}

	case "stop", "die":
		slog.Info("Container stopped", "container", containerName, "id", containerID)
		m.handleContainerStop(ctx, containerID, containerName)

	case "destroy":
		slog.Info("Container destroyed", "container", containerName, "id", containerID)
		m.handleContainerDestroy(ctx, containerID, containerName)
	}
}

// getAppNameFromLabels extracts appName from labels
func getAppNameFromLabels(labels map[string]string, containerName string) string {
	appName := labels["watchcow.appname"]
	if appName == "" {
		appName = "watchcow." + containerName
	}
	return appName
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
	appName := getAppNameFromLabels(labels, containerName)

	// Check if already installed in fnOS
	if m.installer != nil && m.installer.IsAppInstalled(appName) {
		// Already installed, just start it
		slog.Info("App already installed, starting", "app", appName)
		if err := m.queueOperation("start", appName, ""); err != nil {
			slog.Warn("Failed to start fnOS app", "app", appName, "error", err)
		}

		// Track in memory
		m.mu.Lock()
		m.containers[containerID] = &ContainerState{
			ContainerID:   containerID,
			ContainerName: containerName,
			AppName:       appName,
			Installed:     true,
			Labels:        labels,
		}
		m.mu.Unlock()
		return
	}

	// Not installed yet, generate and install
	time.Sleep(2 * time.Second)

	config, appDir, err := m.generator.GenerateFromContainer(ctx, containerID)
	if err != nil {
		slog.Error("Failed to generate fnOS app", "container", containerName, "error", err)
		return
	}

	// Record state
	m.mu.Lock()
	m.containers[containerID] = &ContainerState{
		ContainerID:   containerID,
		ContainerName: containerName,
		AppName:       config.AppName,
		Installed:     false,
		Labels:        labels,
	}
	m.mu.Unlock()

	// Install via queue (serialized)
	if err := m.queueOperation("install", config.AppName, appDir); err != nil {
		slog.Error("Failed to install fnOS app", "app", config.AppName, "error", err)
		return
	}

	m.mu.Lock()
	if state, exists := m.containers[containerID]; exists {
		state.Installed = true
	}
	m.mu.Unlock()
	slog.Info("Successfully installed fnOS app", "app", config.AppName, "container", containerName)
	m.generator.MarkInstalled(containerID, config)
}

// handleContainerStop handles container stop event (stop app, keep installed)
func (m *Monitor) handleContainerStop(ctx context.Context, containerID, containerName string) {
	m.mu.RLock()
	state, exists := m.containers[containerID]
	m.mu.RUnlock()

	if !exists || !state.Installed {
		return
	}

	// Stop via queue (serialized)
	if err := m.queueOperation("stop", state.AppName, ""); err != nil {
		slog.Warn("Failed to stop fnOS app", "app", state.AppName, "error", err)
	}
}

// handleContainerDestroy handles container destroy event (uninstall app)
func (m *Monitor) handleContainerDestroy(ctx context.Context, containerID, containerName string) {
	m.mu.RLock()
	state, exists := m.containers[containerID]
	m.mu.RUnlock()

	if !exists {
		return
	}

	// Uninstall via queue (serialized)
	if state.Installed {
		if err := m.queueOperation("uninstall", state.AppName, ""); err != nil {
			slog.Warn("Failed to uninstall fnOS app", "app", state.AppName, "error", err)
		}
	}

	// Remove from tracking
	m.mu.Lock()
	delete(m.containers, containerID)
	m.mu.Unlock()
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
