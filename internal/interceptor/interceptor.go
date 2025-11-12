package interceptor

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Event flags (must match bpf/common.h)
const (
	FLAG_APPSTORE = 0x01
	FLAG_NOTIFY   = 0x02
)

// Event represents a sendmsg event from eBPF
type Event struct {
	PID         uint32
	TID         uint32
	FD          uint32
	DataLen     uint32
	Timestamp   uint64
	DebugStep   uint32
	DebugIovLen uint32
	Flags       uint32 // Event flags (FLAG_APPSTORE, FLAG_NOTIFY, etc)
	SocketPath  string
	Data        []byte
}

// Interceptor is the main interception handler
type Interceptor struct {
	pidfdMgr  *PidfdManager
	processor *DataProcessor
	notifier  *Notifier
	mu        sync.RWMutex

	// Track processed requests to avoid duplicates
	processedReqs map[string]time.Time

	// Track trim_sac PID (for sending notifications)
	trimSacPID int

	// Debug mode
	debug bool

	// Stats
	eventsReceived    uint64
	eventsProcessed   uint64
	responsesInjected uint64
}

// NewInterceptor creates a new Interceptor
func NewInterceptor() *Interceptor {
	pidfdMgr := NewPidfdManager()
	return &Interceptor{
		pidfdMgr:      pidfdMgr,
		processor:     NewDataProcessor(),
		notifier:      NewNotifier(pidfdMgr),
		processedReqs: make(map[string]time.Time),
	}
}

// SetDebug enables or disables debug mode
func (i *Interceptor) SetDebug(debug bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.debug = debug
}

// ProcessEvent processes an event from eBPF
func (i *Interceptor) ProcessEvent(ctx context.Context, event *Event) error {
	i.mu.Lock()
	i.eventsReceived++
	// Track trim_sac PID for later use
	if i.trimSacPID == 0 || i.trimSacPID != int(event.PID) {
		i.trimSacPID = int(event.PID)
	}
	i.mu.Unlock()

	// Skip small packets (likely WebSocket ping/pong or keepalive)
	if event.DataLen <= 20 {
		return nil
	}

	// Debug mode: Print hex dump of all large packets
	if i.debug && event.DataLen > 100 {
		dataStr := string(event.Data)

		// Identify packet type
		if strings.Contains(dataStr, "notify") {
			log.Printf("🔔 [DEBUG] Found packet with 'notify' keyword (PID: %d, FD: %d, Len: %d)", event.PID, event.FD, event.DataLen)
		} else if strings.Contains(dataStr, "appStoreList") {
			log.Printf("📱 [DEBUG] Found appStoreList packet (PID: %d, FD: %d, Len: %d)", event.PID, event.FD, event.DataLen)
		} else {
			log.Printf("📦 [DEBUG] Found unknown packet (PID: %d, FD: %d, Len: %d)", event.PID, event.FD, event.DataLen)
		}

		log.Printf("📊 [DEBUG] Hex dump:")
		// Print hex dump in chunks of 16 bytes for readability
		for i := 0; i < len(event.Data); i += 16 {
			end := i + 16
			if end > len(event.Data) {
				end = len(event.Data)
			}
			chunk := event.Data[i:end]
			hexStr := hex.EncodeToString(chunk)

			// Format as: offset | hex bytes | ASCII
			ascii := make([]byte, len(chunk))
			for j, b := range chunk {
				if b >= 32 && b <= 126 {
					ascii[j] = b
				} else {
					ascii[j] = '.'
				}
			}
			log.Printf("  %04x | %-48s | %s", i, formatHexBytes(hexStr), string(ascii))
		}
		log.Printf("📝 [DEBUG] Full data: %s", dataStr)
		log.Printf("---")
	}

	// Check if this is an appStoreList response
	if !i.processor.IsAppStoreListResponse(event.Data) {
		return nil
	}

	log.Printf("📱 Detected appStoreList request")

	// Extract request ID (best effort - continue even if it fails)
	reqID, err := i.processor.ExtractReqID(event.Data)
	if err != nil {
		reqID = fmt.Sprintf("ts_%d", event.Timestamp)
	}

	// Check if we've already processed this request (deduplication)
	if i.shouldSkipRequest(reqID) {
		return nil
	}

	// Process the response
	if err := i.injectResponse(event); err != nil {
		return fmt.Errorf("injection failed: %w", err)
	}

	// Mark as processed
	i.markRequestProcessed(reqID)

	i.mu.Lock()
	i.eventsProcessed++
	i.responsesInjected++
	i.mu.Unlock()

	log.Printf("✅ Injected Docker apps into response")
	return nil
}

// injectResponse injects a modified response through duplicated FD
func (i *Interceptor) injectResponse(event *Event) error {
	// Duplicate the socket FD from the target process
	newFD, err := i.pidfdMgr.DuplicateSocketFD(int(event.PID), int(event.FD))
	if err != nil {
		// Try to find the correct FD by scanning
		newFD, err = i.findAndDuplicateSocketFD(int(event.PID))
		if err != nil {
			return fmt.Errorf("failed to duplicate socket FD: %w", err)
		}
	}

	// Ensure we close the duplicated FD when done
	defer syscall.Close(newFD)

	// Create modified response with injected Docker apps
	modifiedData, err := i.processor.InjectDockerApps(event.Data)
	if err != nil {
		return fmt.Errorf("failed to create modified response: %w", err)
	}

	// Send the modified response immediately (original has invalid reqID)
	if err := SendData(newFD, modifiedData); err != nil {
		return fmt.Errorf("failed to send modified response: %w", err)
	}

	return nil
}

// findAndDuplicateSocketFD scans for the correct socket FD
func (i *Interceptor) findAndDuplicateSocketFD(pid int) (int, error) {
	// Try common FD numbers for Unix sockets
	commonFDs := []int{3, 4, 5, 6, 7, 8, 9, 10, 11, 12}

	for _, fd := range commonFDs {
		socketPath, err := GetSocketPath(pid, fd)
		if err != nil {
			continue
		}

		// Check if this is our target socket
		if strings.Contains(socketPath, "trim_sac.socket") ||
			strings.Contains(socketPath, "socket:[") {
			// Try to duplicate it
			newFD, err := i.pidfdMgr.DuplicateSocketFD(pid, fd)
			if err == nil {
				return newFD, nil
			}
		}
	}

	return -1, fmt.Errorf("could not find valid socket FD for PID %d", pid)
}

// formatHexBytes formats hex string with spaces between bytes
func formatHexBytes(hexStr string) string {
	var result strings.Builder
	for i := 0; i < len(hexStr); i += 2 {
		if i > 0 {
			result.WriteByte(' ')
		}
		result.WriteString(hexStr[i : i+2])
	}
	return result.String()
}

// shouldSkipRequest checks if we should skip processing this request
func (i *Interceptor) shouldSkipRequest(reqID string) bool {
	i.mu.RLock()
	defer i.mu.RUnlock()

	if lastProcessed, exists := i.processedReqs[reqID]; exists {
		// Skip if processed within last 5 seconds
		if time.Since(lastProcessed) < 5*time.Second {
			return true
		}
	}
	return false
}

// markRequestProcessed marks a request as processed
func (i *Interceptor) markRequestProcessed(reqID string) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.processedReqs[reqID] = time.Now()

	// Clean up old entries (older than 1 minute)
	cutoff := time.Now().Add(-1 * time.Minute)
	for id, timestamp := range i.processedReqs {
		if timestamp.Before(cutoff) {
			delete(i.processedReqs, id)
		}
	}
}

// findTrimSacPID searches for the trim_sac process PID
func findTrimSacPID() (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, fmt.Errorf("failed to read /proc: %w", err)
	}

	for _, entry := range entries {
		// Only check numeric directories (PIDs)
		if !entry.IsDir() {
			continue
		}

		pidStr := entry.Name()
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}

		// Read process cmdline
		cmdlinePath := fmt.Sprintf("/proc/%d/cmdline", pid)
		cmdlineData, err := os.ReadFile(cmdlinePath)
		if err != nil {
			continue
		}

		// cmdline is null-separated, convert to string
		cmdline := string(cmdlineData)

		// Check if this is trim_sac process
		// Match: /usr/trim/bin/trim_sac
		// Exclude: grep, postgres (trim_sac_admin)
		if strings.Contains(cmdline, "/trim_sac") &&
			strings.Contains(cmdline, "/usr/trim/bin/") {
			return pid, nil
		}
	}

	return 0, fmt.Errorf("trim_sac process not found")
}

// findActiveSocketFDs dynamically scans for active Unix socket FDs
// This is called on-demand when sending notifications (low frequency operation)
func (i *Interceptor) findActiveSocketFDs(pid int) ([]int, error) {
	fds := []int{}

	// Scan /proc/{pid}/fd/ directory
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read fd directory: %w", err)
	}

	if i.debug {
		log.Printf("🔍 [DEBUG] Scanning %d FDs in PID %d", len(entries), pid)
	}

	for _, entry := range entries {
		// Parse FD number from filename
		fdNum, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// Skip stdin/stdout/stderr
		if fdNum < 3 {
			continue
		}

		// Check if it's a Unix socket
		socketPath, err := GetSocketPath(pid, fdNum)
		if err != nil {
			continue
		}

		// Filter for Unix sockets - socketPath can be:
		// 1. "socket:[inode]" - anonymous socket (what we want!)
		// 2. "/var/run/trim_sac.socket (inode:12345)" - named socket (skip)

		if i.debug {
			log.Printf("🔍 [DEBUG] FD %d: %s", fdNum, socketPath)
		}

		// Strategy: ONLY accept anonymous sockets (socket:[inode])
		// Exclude sockets with filesystem paths
		isAnonymous := strings.HasPrefix(socketPath, "socket:[")
		if !isAnonymous {
			if i.debug {
				log.Printf("⏭️  [DEBUG] FD %d: Skipped (has filesystem path)", fdNum)
			}
			continue
		}

		// Check if it's connected and get detailed info
		isConnected, socketInfo := getSocketDetailedInfo(pid, socketPath)
		if i.debug {
			log.Printf("🔍 [DEBUG] FD %d: isConnected=%v", fdNum, isConnected)
			log.Printf("    Type=%s Flags=%s State=%s RefCount=%s",
				socketInfo.Type, socketInfo.Flags, socketInfo.State, socketInfo.RefCount)
		}

		// Check if peer is "trim" process using netlink SOCK_DIAG (with debug output)
		hasPeer, peerInfo := hasTrimPeer(socketInfo.Inode, i.debug)
		if i.debug {
			if hasPeer {
				log.Printf("    👥 Peer: %s ✅", peerInfo)
			} else {
				log.Printf("    👥 Peer: %s ❌", peerInfo)
			}
		}

		// Only accept connected sockets where peer is "trim" process
		if isConnected && hasPeer {
			fds = append(fds, fdNum)
			if i.debug {
				log.Printf("✅ [DEBUG] FD %d: ACCEPTED (inode:%s, peer:%s)", fdNum, socketInfo.Inode, peerInfo)
			}
		}
	}

	return fds, nil
}

// SocketInfo contains detailed information about a Unix socket
type SocketInfo struct {
	Num      string
	RefCount string
	Protocol string
	Flags    string
	Type     string
	State    string
	Inode    string
	Path     string
}

// ProcessInfo contains information about a process using a socket
type ProcessInfo struct {
	PID  int
	Name string
	FDs  []int // File descriptors using this socket
}

// findProcessesUsingInode finds all processes that have FDs pointing to this socket inode
func findProcessesUsingInode(inode string) []ProcessInfo {
	if inode == "" {
		return nil
	}

	var processes []ProcessInfo
	targetSocket := fmt.Sprintf("socket:[%s]", inode)

	// Scan /proc for all processes
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if it's a PID directory
		pidStr := entry.Name()
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}

		// Read process name
		cmdlinePath := fmt.Sprintf("/proc/%d/cmdline", pid)
		cmdlineData, err := os.ReadFile(cmdlinePath)
		if err != nil {
			continue
		}

		// Parse cmdline (null-separated)
		cmdline := string(cmdlineData)
		var processName string
		if idx := strings.Index(cmdline, "\x00"); idx > 0 {
			processName = cmdline[:idx]
		} else {
			processName = cmdline
		}

		// Extract just the binary name
		if lastSlash := strings.LastIndex(processName, "/"); lastSlash >= 0 {
			processName = processName[lastSlash+1:]
		}

		// Scan this process's FDs
		fdDir := fmt.Sprintf("/proc/%d/fd", pid)
		fdEntries, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}

		var matchingFDs []int
		for _, fdEntry := range fdEntries {
			fdNum, err := strconv.Atoi(fdEntry.Name())
			if err != nil {
				continue
			}

			linkPath := fmt.Sprintf("/proc/%d/fd/%d", pid, fdNum)
			target, err := os.Readlink(linkPath)
			if err != nil {
				continue
			}

			if target == targetSocket {
				matchingFDs = append(matchingFDs, fdNum)
			}
		}

		if len(matchingFDs) > 0 {
			processes = append(processes, ProcessInfo{
				PID:  pid,
				Name: processName,
				FDs:  matchingFDs,
			})
		}
	}

	return processes
}

// getSocketDetailedInfo gets detailed socket information from /proc/net/unix
// Returns (isConnected, socketInfo)
func getSocketDetailedInfo(pid int, socketPath string) (bool, SocketInfo) {
	info := SocketInfo{}

	// Extract inode from "socket:[12345]"
	if !strings.HasPrefix(socketPath, "socket:[") {
		return false, info
	}
	inode := socketPath[8 : len(socketPath)-1]
	info.Inode = inode

	// Read socket state from /proc/net/unix
	socketInfoPath := fmt.Sprintf("/proc/%d/net/unix", pid)
	data, err := os.ReadFile(socketInfoPath)
	if err != nil {
		info.State = "read_error"
		return false, info
	}

	// Parse /proc/net/unix to get detailed info
	// Format: Num RefCount Protocol Flags Type St Inode Path
	lines := strings.Split(string(data), "\n")
	for _, line := range lines[1:] { // Skip header
		fields := strings.Fields(line)
		if len(fields) >= 7 && fields[6] == inode {
			info.Num = fields[0]
			info.RefCount = fields[1]
			info.Protocol = fields[2]
			info.Flags = fields[3]
			info.Type = fields[4]
			info.State = fields[5]
			if len(fields) >= 8 {
				info.Path = fields[7]
			}

			// Check if connected: 01=SS_CONNECTED, 03=SS_CONNECTING
			isConnected := (info.State == "01" || info.State == "03")
			return isConnected, info
		}
	}

	info.State = "not_found"
	return false, info
}

// isConnectedSocketWithState checks if a socket FD is connected and returns its state
// Returns (isConnected, stateString)
func isConnectedSocketWithState(pid int, socketPath string) (bool, string) {
	// Extract inode from "socket:[12345]"
	if !strings.HasPrefix(socketPath, "socket:[") {
		return false, "invalid_format"
	}
	inode := socketPath[8 : len(socketPath)-1]

	// Read socket state from /proc/net/unix
	socketInfo := fmt.Sprintf("/proc/%d/net/unix", pid)
	data, err := os.ReadFile(socketInfo)
	if err != nil {
		return false, "read_error"
	}

	// Parse /proc/net/unix to check socket state
	// Format: Num RefCount Protocol Flags Type St Inode Path
	lines := strings.Split(string(data), "\n")
	for _, line := range lines[1:] { // Skip header
		fields := strings.Fields(line)
		if len(fields) >= 7 && fields[6] == inode {
			// Check St (State) field - index 5
			// 01 = SS_CONNECTED, 03 = SS_CONNECTING, 0A = SS_LISTENING
			state := fields[5]
			isConnected := (state == "01" || state == "03")
			return isConnected, state
		}
	}

	return false, "not_found"
}

// GetStats returns current statistics
func (i *Interceptor) GetStats() (received, processed, injected uint64) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.eventsReceived, i.eventsProcessed, i.responsesInjected
}

// StartDockerAppListener starts the Docker app update listener
func (i *Interceptor) StartDockerAppListener() {
	i.processor.StartUpdateListener()
}

// GetDockerAppUpdateChannel returns the channel for Docker app updates
func (i *Interceptor) GetDockerAppUpdateChannel() chan<- []AppInfo {
	return i.processor.GetUpdateChannel()
}

// SendContainerNotification sends a notification about container state change
// Dynamically scans for active socket connections (low frequency operation)
func (i *Interceptor) SendContainerNotification(containerName string, state string) error {
	i.mu.RLock()
	pid := i.trimSacPID
	i.mu.RUnlock()

	// If PID not yet detected via eBPF, try to find it
	if pid == 0 {
		foundPID, err := findTrimSacPID()
		if err != nil {
			return fmt.Errorf("trim_sac process not found: %w", err)
		}
		pid = foundPID

		// Cache it for future use
		i.mu.Lock()
		i.trimSacPID = pid
		i.mu.Unlock()

		log.Printf("📍 Detected trim_sac process: PID %d", pid)
	}

	// Dynamically scan for active Unix socket FDs
	fds, err := i.findActiveSocketFDs(pid)
	if err != nil {
		return fmt.Errorf("failed to scan socket FDs: %w", err)
	}

	if len(fds) == 0 {
		return fmt.Errorf("no active socket connections found")
	}

	if i.debug {
		log.Printf("🔍 [DEBUG] Found %d candidate FDs for notification: %v", len(fds), fds)
	}

	// Broadcast to all active socket FDs
	successCount := 0
	for idx, fd := range fds {
		// Print hex dump only for first FD in debug mode
		var err error
		if idx == 0 && i.debug {
			err = i.notifier.SendToActiveClientWithDebug(pid, fd, containerName, state)
		} else {
			err = i.notifier.SendToActiveClient(pid, fd, containerName, state)
		}

		if err != nil {
			log.Printf("⚠️  Failed to send to FD %d: %v", fd, err)
			continue
		}
		successCount++
	}

	if successCount == 0 {
		return fmt.Errorf("failed to send to any client")
	}

	log.Printf("✅ Notification broadcast successful (%d/%d sockets)", successCount, len(fds))
	return nil
}

// Close cleans up resources
func (i *Interceptor) Close() {
	i.pidfdMgr.Close()
}
