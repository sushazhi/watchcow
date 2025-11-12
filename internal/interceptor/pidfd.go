package interceptor

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
)

// PidfdManager manages pidfd operations for socket duplication
type PidfdManager struct {
	mu        sync.Mutex
	cachedFDs map[string]int // cache: "pid:fd" -> duplicated_fd
}

// NewPidfdManager creates a new PidfdManager
func NewPidfdManager() *PidfdManager {
	return &PidfdManager{
		cachedFDs: make(map[string]int),
	}
}

// DuplicateSocketFD duplicates a socket file descriptor from another process
func (pm *PidfdManager) DuplicateSocketFD(pid int, fd int) (int, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Check cache
	key := fmt.Sprintf("%d:%d", pid, fd)
	if cachedFD, exists := pm.cachedFDs[key]; exists {
		// Verify the FD is still valid
		if isValidFD(cachedFD) {
			return cachedFD, nil
		}
		// Remove invalid FD from cache
		delete(pm.cachedFDs, key)
	}

	// Use direct syscall (requires Linux 5.6+ kernel)
	newFD, err := duplicateViaSystemCall(pid, fd)
	if err != nil {
		return -1, fmt.Errorf("failed to duplicate FD %d from PID %d: %w", fd, pid, err)
	}

	pm.cachedFDs[key] = newFD
	return newFD, nil
}

// Close closes all cached file descriptors
func (pm *PidfdManager) Close() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, fd := range pm.cachedFDs {
		syscall.Close(fd)
	}
	pm.cachedFDs = make(map[string]int)
}

// DuplicateSocketFDNocache duplicates a socket FD without caching (for one-time use)
// Caller is responsible for closing the returned FD
func (pm *PidfdManager) DuplicateSocketFDNocache(pid int, fd int) (int, error) {
	return duplicateViaSystemCall(pid, fd)
}

// duplicateViaSystemCall uses direct syscalls to duplicate the FD
func duplicateViaSystemCall(pid int, fd int) (int, error) {
	const (
		SYS_PIDFD_OPEN  = 434
		SYS_PIDFD_GETFD = 438
	)

	// Open pidfd for the target process
	pidfd, _, errno := syscall.Syscall(SYS_PIDFD_OPEN, uintptr(pid), 0, 0)
	if errno != 0 {
		return -1, fmt.Errorf("pidfd_open failed: %v", errno)
	}
	defer syscall.Close(int(pidfd))

	// Duplicate the file descriptor
	newFD, _, errno := syscall.Syscall(SYS_PIDFD_GETFD, pidfd, uintptr(fd), 0)
	if errno != 0 {
		return -1, fmt.Errorf("pidfd_getfd failed: %v", errno)
	}

	return int(newFD), nil
}

// isValidFD checks if a file descriptor is still valid
func isValidFD(fd int) bool {
	_, _, err := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETFD, 0)
	return err == 0
}

// GetSocketPath tries to determine the socket path for a given FD
func GetSocketPath(pid int, fd int) (string, error) {
	linkPath := fmt.Sprintf("/proc/%d/fd/%d", pid, fd)
	target, err := os.Readlink(linkPath)
	if err != nil {
		return "", err
	}

	// Unix sockets typically show as "socket:[inode]"
	// Extract inode and look it up in /proc/net/unix
	if strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
		// Extract inode number
		inode := target[8 : len(target)-1]

		// Look up the socket path in /proc/net/unix (try both container and host namespaces)
		if realPath, err := lookupUnixSocketPath(inode); err == nil {
			return fmt.Sprintf("%s (inode:%s)", realPath, inode), nil
		}

		// Also try host namespace if running in a container
		if realPath, err := lookupUnixSocketPathInNamespace(pid, inode); err == nil {
			return fmt.Sprintf("%s (inode:%s)", realPath, inode), nil
		}
	}

	return target, nil
}

// lookupUnixSocketPath looks up the real path of a Unix socket by its inode
func lookupUnixSocketPath(inode string) (string, error) {
	return lookupUnixSocketPathFromFile("/proc/net/unix", inode)
}

// lookupUnixSocketPathInNamespace looks up socket path in target process's namespace
func lookupUnixSocketPathInNamespace(pid int, inode string) (string, error) {
	netUnixPath := fmt.Sprintf("/proc/%d/net/unix", pid)
	return lookupUnixSocketPathFromFile(netUnixPath, inode)
}

// lookupUnixSocketPathFromFile looks up the real path from a specific /proc/net/unix file
func lookupUnixSocketPathFromFile(filePath string, inode string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	// Parse /proc/net/unix
	// Format: Num RefCount Protocol Flags Type St Inode Path
	lines := strings.Split(string(data), "\n")
	for _, line := range lines[1:] { // Skip header
		fields := strings.Fields(line)
		if len(fields) >= 7 {
			if fields[6] == inode {
				// Path is the 8th field (index 7) if it exists
				if len(fields) >= 8 {
					return fields[7], nil
				}
				return "", fmt.Errorf("socket has no path (anonymous)")
			}
		}
	}

	return "", fmt.Errorf("inode %s not found in %s", inode, filePath)
}

// SendData sends data through a duplicated socket FD
func SendData(fd int, data []byte) error {
	n, err := syscall.Write(fd, data)
	if err != nil {
		return fmt.Errorf("failed to write to socket: %w", err)
	}
	if n != len(data) {
		return fmt.Errorf("partial write: %d/%d bytes", n, len(data))
	}
	return nil
}
