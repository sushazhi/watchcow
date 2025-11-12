package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"watchcow/internal/docker"
	"watchcow/internal/ebpf"
)

func main() {
	// Parse command line flags
	debug := flag.Bool("debug", false, "Enable debug mode (print hex dump of all packets)")
	flag.Parse()

	// Configure slog
	var logLevel slog.Level
	if *debug {
		logLevel = slog.LevelDebug
	} else {
		logLevel = slog.LevelInfo
	}

	// Use text handler with time and level
	opts := &slog.HandlerOptions{
		Level: logLevel,
	}
	handler := slog.NewTextHandler(os.Stdout, opts)
	logger := slog.New(handler)
	slog.SetDefault(logger)

	slog.Info("🚀 WatchCow - Docker Injector for fnOS")
	slog.Info("========================================")
	if *debug {
		slog.Info("🔍 Debug mode enabled")
	}

	// Create context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Create and initialize eBPF manager
	manager, err := ebpf.NewManager()
	if err != nil {
		slog.Error("Failed to create eBPF manager", "error", err)
		os.Exit(1)
	}
	defer manager.Stop()

	// Set debug mode
	manager.SetDebug(*debug)

	// Start Docker app update listener
	manager.StartDockerAppListener()

	// Create and start Docker monitor
	dockerMonitor, err := docker.NewMonitor(
		manager.GetDockerAppUpdateChannel(),
		manager.GetInterceptor(),
	)
	if err != nil {
		slog.Warn("Failed to create Docker monitor", "error", err)
		slog.Warn("Continuing without Docker monitoring...")
	} else {
		go dockerMonitor.Start(ctx)
		defer dockerMonitor.Stop()
	}

	// Load and attach eBPF programs
	if err := manager.LoadAndAttach(); err != nil {
		slog.Error("Failed to load eBPF programs", "error", err)
		os.Exit(1)
	}

	slog.Info("✅ eBPF programs loaded successfully")

	// Start event processing
	if err := manager.Start(ctx); err != nil {
		slog.Error("Failed to start event processing", "error", err)
		os.Exit(1)
	}

	slog.Info("✅ Monitoring started (Press Ctrl+C to stop)")
	slog.Info("")

	// Wait for shutdown signal
	<-sigChan
	slog.Info("\n🛑 Shutting down...")
}
