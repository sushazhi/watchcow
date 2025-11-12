# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**WatchCow** is a Docker injector for fnOS that uses eBPF to intercept WebSocket communication from the `trim_sac` process and inject Docker container information into application lists. It monitors Docker containers and sends real-time notifications about container state changes.

The system works by:
1. Using eBPF to hook into the `trim_sac` process's write() syscalls
2. Detecting `appStoreList` WebSocket responses containing application lists
3. Injecting Docker container information into these responses
4. Monitoring Docker events and sending notifications to fnOS clients

## Build & Development Commands

### Building
```bash
# Build Docker image
docker-compose build

# Generate eBPF bindings (if modifying eBPF code)
cd internal/ebpf
go generate
```

### Running
```bash
# Start the service
docker-compose up -d

# View logs
docker logs -f watchcow

# Enable debug mode (shows hex dumps of intercepted packets)
docker-compose run watchcow --debug

# Stop the service
docker-compose down
```

### Testing
There are no automated tests in this codebase. Testing is done manually using the examples:

```bash
# Test with nginx example
cd examples/nginx
docker-compose up -d

# Verify watchcow detected the container
docker logs watchcow | grep nginx
```

## Architecture

### Core Components

**1. eBPF Hook (`bpf/unix_hook.c`, `internal/ebpf/loader.go`)**
- Attaches to `sys_enter_write` tracepoint
- Filters for `trim_sac` process only
- Captures write() data and sends to userspace via ring buffer
- Generated Go bindings via `bpf2go` tool

**2. Interceptor (`internal/interceptor/interceptor.go`)**
- Main event processing pipeline
- Detects `appStoreList` WebSocket responses
- Injects Docker container info via `DataProcessor`
- Manages socket FD duplication via `PidfdManager`
- Handles notifications to fnOS clients

**3. Docker Monitor (`internal/docker/monitor.go`)**
- Listens to Docker daemon events (start/stop/die/destroy)
- Converts containers to `AppInfo` structures
- Supports both auto-discovery and explicit labeling via `watchcow.*` labels
- Sends real-time notifications when containers change state

**4. Data Processor (`internal/interceptor/processor.go`)**
- Manages the list of Docker apps to inject
- Parses and modifies WebSocket JSON responses
- Extracts request IDs for deduplication

**5. Notifier (`internal/interceptor/notifier.go`)**
- Sends WebSocket notifications to fnOS clients
- Constructs properly formatted messages for container state changes

### Key Data Flow

```
Docker Event → Docker Monitor → Update DockerApps List
                                        ↓
                                  Send Notification to fnOS clients

eBPF Hook → Ring Buffer → Interceptor → Detect appStoreList response
                                      → Inject Docker apps
                                      → Send modified response via duplicated FD
```

### Critical Implementation Details

**Socket FD Duplication**
- Uses `pidfd_getfd()` syscall to duplicate socket FDs from `trim_sac` process
- Implemented in `internal/interceptor/pidfd.go`
- Required because eBPF cannot directly modify data in flight

**Process Discovery**
- Dynamically finds `trim_sac` process by scanning `/proc`
- Filters for anonymous Unix sockets using SOCK_DIAG netlink
- Validates socket connections to ensure they're active fnOS clients

**WebSocket Format**
- fnOS uses custom WebSocket message format with binary framing
- Messages start with 0x82 (WebSocket binary frame)
- Payload length encoded in first 2 bytes after frame type
- JSON data follows the framing

**WatchCow Labels**
Containers can be explicitly configured with labels:
- `watchcow.enable`: "true" to enable discovery
- `watchcow.appName`, `watchcow.title`, `watchcow.port`, etc.
- See `examples/README.md` for complete label reference

## Platform Requirements

- **Linux only** (uses eBPF and Linux-specific syscalls)
- **Debian 12 (bookworm)** - matches fnOS target platform
- **Kernel 5.8+** required for eBPF features
- Must run in **privileged** mode with host PID/network namespace
- Requires access to `/sys/kernel/debug` for tracepoints
- Requires access to `/var/run/docker.sock` for Docker monitoring

## Development Guidelines

### When Modifying eBPF Code
1. Edit `bpf/unix_hook.c`
2. Run `go generate ./internal/ebpf` to regenerate bindings
3. Rebuild Docker image (eBPF bytecode is embedded in binary)

### When Adding New Features
- Docker app conversion logic is in `docker/monitor.go:containerToAppInfo()`
- Injection logic is in `interceptor/processor.go:InjectDockerApps()`
- WebSocket message construction is in `interceptor/notifier.go`

### Debugging
- Use `--debug` flag to see hex dumps of all intercepted packets
- Check for "trim_sac" process: `ps aux | grep trim_sac`
- Verify eBPF programs loaded: `bpftool prog list | grep watchcow`
- Check ring buffer stats: `bpftool map list`

### Common Gotchas
- eBPF verifier is strict about bounds checking - always validate array access
- Socket FD scanning must filter out named sockets (only use anonymous sockets)
- Request deduplication is critical (same request may be captured multiple times)
- WebSocket frame parsing requires exact byte offsets - off-by-one errors are common
- Container notifications require `trim_sac` to be running first (may panic and restart if not ready)

## Important Files

- `cmd/watchcow/main.go` - Entry point, flag parsing, initialization
- `internal/ebpf/loader.go` - eBPF program loading and ring buffer handling
- `internal/interceptor/interceptor.go` - Main processing logic
- `internal/docker/monitor.go` - Docker event monitoring
- `bpf/unix_hook.c` - eBPF C code for write() interception
- `docker-compose.yml` - Required capabilities and volume mounts
