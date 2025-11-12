package interceptor

/*
#cgo CFLAGS: -I.
#include "sockdiag_cgo.h"
*/
import "C"

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// Netlink constants for SOCK_DIAG
const (
	NETLINK_SOCK_DIAG   = 4
	SOCK_DIAG_BY_FAMILY = 20

	AF_UNIX = 1

	// Show flags for unix_diag_req.udiag_show (request)
	UDIAG_SHOW_NAME    = 0x00000001
	UDIAG_SHOW_VFS     = 0x00000002
	UDIAG_SHOW_PEER    = 0x00000004
	UDIAG_SHOW_ICONS   = 0x00000008
	UDIAG_SHOW_RQLEN   = 0x00000010
	UDIAG_SHOW_MEMINFO = 0x00000020

	// Attribute types in response (rtattr type field)
	UNIX_DIAG_NAME    = 1
	UNIX_DIAG_VFS     = 2
	UNIX_DIAG_PEER    = 3
	UNIX_DIAG_ICONS   = 4
	UNIX_DIAG_RQLEN   = 5
	UNIX_DIAG_MEMINFO = 6

	NLMSG_ALIGNTO = 4

	// Socket states for unix_diag_req.States
	SS_ESTABLISHED    = 1
	SS_SYN_SENT       = 2
	SS_SYN_RECV       = 3
	SS_FIN_WAIT1      = 4
	SS_FIN_WAIT2      = 5
	SS_TIME_WAIT      = 6
	SS_CLOSE          = 7
	SS_CLOSE_WAIT     = 8
	SS_LAST_ACK       = 9
	SS_LISTEN         = 10
	SS_CLOSING        = 11
	SS_NEW_SYN_RECV   = 12
	SS_BOUND_INACTIVE = 13
	SS_MAX            = 14
)

// SS_CONN matches connected sockets (excludes LISTEN, CLOSE, TIME_WAIT, SYN_RECV)
// This is what 'ss' uses by default for Unix stream sockets
const SS_CONN = ((1 << SS_MAX) - 1) & ^((1 << SS_LISTEN) | (1 << SS_CLOSE) | (1 << SS_TIME_WAIT) | (1 << SS_SYN_RECV))

// nlmsghdr represents netlink message header
type nlmsghdr struct {
	Len   uint32
	Type  uint16
	Flags uint16
	Seq   uint32
	Pid   uint32
}

// unixDiagReq represents Unix socket diagnostic request
type unixDiagReq struct {
	Family    uint8
	Protocol  uint8
	Pad       uint16
	States    uint32
	Ino       uint32
	ShowFlags uint32
	Cookie    [8]byte
}

// unixDiagMsg represents Unix socket diagnostic message
type unixDiagMsg struct {
	Family uint8
	Type   uint8
	State  uint8
	Pad    uint8
	Ino    uint32
	Cookie [8]byte
}

// nlAttr represents netlink attribute
type nlAttr struct {
	Len  uint16
	Type uint16
}

// align rounds the length up to NLMSG_ALIGNTO
func nlmsgAlign(len int) int {
	return (len + NLMSG_ALIGNTO - 1) & ^(NLMSG_ALIGNTO - 1)
}

// containsNlmsgDone checks if the netlink data contains NLMSG_DONE message
func containsNlmsgDone(data []byte) bool {
	offset := 0
	for offset < len(data) {
		if offset+syscall.NLMSG_HDRLEN > len(data) {
			break
		}
		hdr := (*nlmsghdr)(unsafe.Pointer(&data[offset]))
		if hdr.Type == syscall.NLMSG_DONE {
			return true
		}
		msgLen := nlmsgAlign(int(hdr.Len))
		if msgLen <= 0 {
			break
		}
		offset += msgLen
	}
	return false
}

// getPeerInodeUsingCGO uses CGO to call C function for SOCK_DIAG query
func getPeerInodeUsingCGO(inode uint32, debug bool) (uint32, error) {
	if debug {
		slog.Debug("Querying peer inode using CGO",
			"component", "sockdiag",
			"method", "cgo",
			"inode", inode)
	}

	// Call C function
	peerInode := uint32(C.get_peer_inode_cgo(C.uint32_t(inode)))

	if peerInode == 0 {
		if debug {
			slog.Debug("Failed to find peer inode",
				"component", "sockdiag",
				"method", "cgo",
				"inode", inode,
				"status", "not_found")
		}
		return 0, fmt.Errorf("peer inode not found for inode %d", inode)
	}

	if debug {
		slog.Debug("Found peer inode",
			"component", "sockdiag",
			"method", "cgo",
			"inode", inode,
			"peer_inode", peerInode,
			"status", "success")
	}
	return peerInode, nil
}

// findProcessByInode finds the process that owns a specific socket inode
func findProcessByInode(inode uint32) (pid int, name string, err error) {
	targetSocket := fmt.Sprintf("socket:[%d]", inode)

	// Try /host/proc first (when running in container with -v /proc:/host/proc)
	// Fallback to /proc (when running natively)
	procPath := "/host/proc"
	if _, err := os.Stat(procPath); os.IsNotExist(err) {
		procPath = "/proc"
	}

	entries, err := os.ReadDir(procPath)
	if err != nil {
		return 0, "", err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pidStr := entry.Name()
		pidNum, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}

		// Scan FDs
		fdDir := fmt.Sprintf("%s/%d/fd", procPath, pidNum)
		fdEntries, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}

		for _, fdEntry := range fdEntries {
			linkPath := fmt.Sprintf("%s/%d/fd/%s", procPath, pidNum, fdEntry.Name())
			target, err := os.Readlink(linkPath)
			if err != nil {
				continue
			}

			if target == targetSocket {
				// Found it! Get process name
				cmdlinePath := fmt.Sprintf("%s/%d/cmdline", procPath, pidNum)
				cmdlineData, err := os.ReadFile(cmdlinePath)
				if err != nil {
					return pidNum, "", nil
				}

				cmdline := string(cmdlineData)
				var processName string
				if idx := strings.Index(cmdline, "\x00"); idx > 0 {
					processName = cmdline[:idx]
				} else {
					processName = cmdline
				}

				if lastSlash := strings.LastIndex(processName, "/"); lastSlash >= 0 {
					processName = processName[lastSlash+1:]
				}

				return pidNum, processName, nil
			}
		}
	}

	return 0, "", fmt.Errorf("process not found for inode %d", inode)
}

// hasTrimPeer checks if the socket with given inode has a peer that belongs to "trim" process
func hasTrimPeer(inode string, debug bool) (bool, string) {
	inodeNum, err := strconv.ParseUint(inode, 10, 32)
	if err != nil {
		return false, ""
	}

	// Use CGO to query peer inode via SOCK_DIAG
	peerInode, err := getPeerInodeUsingCGO(uint32(inodeNum), debug)
	if err != nil {
		return false, fmt.Sprintf("failed to get peer inode: %v", err)
	}

	// Find process by peer inode
	pid, name, err := findProcessByInode(peerInode)
	if err != nil {
		return false, fmt.Sprintf("peer inode %d found but process not found", peerInode)
	}

	// Check if it's "trim" process (not trim_sac)
	isTrim := strings.Contains(name, "trim") && !strings.Contains(name, "trim_sac")

	if isTrim {
		return true, fmt.Sprintf("trim (PID:%d, peer_inode:%d) [cgo]", pid, peerInode)
	}

	return false, fmt.Sprintf("%s (PID:%d, peer_inode:%d) [cgo]", name, pid, peerInode)
}
