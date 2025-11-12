package interceptor

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"syscall"
	"time"
)

// NotifyHeader represents the 37-byte binary header for notify messages
type NotifyHeader struct {
	TotalLen  uint32  // offset 0-3: total message length
	Unknown1  uint32  // offset 4-7: always 0x00020000
	Unknown2  uint32  // offset 8-11: always 0x00020000
	DateTime  uint32  // offset 12-15: unix timestamp
	ID        uint32  // offset 16-19: message ID (0 or 1)
	Cat       uint32  // offset 20-23: always 1000
	Level     uint32  // offset 24-27: always 0
	Title     [8]byte // offset 28-35: 8 spaces
	Separator byte    // offset 36: 0x00
}

// NotifyJSON represents the JSON content of a notify message
type NotifyJSON struct {
	From    string          `json:"from"`
	EventID string          `json:"eventId"`
	Data    NotifyEventData `json:"data"`
}

// NotifyEventData represents the data field in notify JSON
type NotifyEventData struct {
	Apps []AppStateChange `json:"apps"`
}

// AppStateChange represents an app state change
type AppStateChange struct {
	AppName string `json:"appName"`
	State   string `json:"state"` // "starting", "running", "stopped"
}

// Notifier sends notifications to fnOS clients
type Notifier struct {
	pidfdMgr *PidfdManager
}

// NewNotifier creates a new Notifier
func NewNotifier(pidfdMgr *PidfdManager) *Notifier {
	return &Notifier{
		pidfdMgr: pidfdMgr,
	}
}

// SendToActiveClient sends notification to a specific active client FD
func (n *Notifier) SendToActiveClient(pid int, fd int, appName string, state string) error {
	return n.sendToActiveClient(pid, fd, appName, state, false)
}

// SendToActiveClientWithDebug sends notification with debug hex dump
func (n *Notifier) SendToActiveClientWithDebug(pid int, fd int, appName string, state string) error {
	return n.sendToActiveClient(pid, fd, appName, state, true)
}

// sendToActiveClient is the internal implementation for sending notifications
func (n *Notifier) sendToActiveClient(pid int, fd int, appName string, state string, debug bool) error {
	// Construct the notify message (once per notification, not per FD)
	message, err := n.buildNotifyMessage(appName, state)
	if err != nil {
		return fmt.Errorf("failed to build notify message: %w", err)
	}

	// Print hex dump if debug mode is enabled
	if debug {
		n.printMessageHexDump(message, appName, state)
	}

	// Send to the active client FD
	if err := n.sendToFD(pid, fd, message); err != nil {
		return fmt.Errorf("failed to send to FD %d: %w", fd, err)
	}

	log.Printf("📢 Sent '%s' notification via FD %d (app: %s)", state, fd, appName)
	return nil
}

// buildNotifyMessage constructs a complete notify message with binary header + JSON
func (n *Notifier) buildNotifyMessage(appName string, state string) ([]byte, error) {
	// Build JSON content
	notifyJSON := NotifyJSON{
		From:    "trim.sac",
		EventID: "entryChange",
		Data: NotifyEventData{
			Apps: []AppStateChange{
				{
					AppName: appName,
					State:   state,
				},
			},
		},
	}

	jsonBytes, err := json.Marshal(notifyJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Calculate total length (header + JSON + \0 + trailing "trim.sac\x00")
	trailing := []byte("\x00trim.sac\x00") // Add \0 before "trim.sac"
	totalLen := 37 + len(jsonBytes) + len(trailing)

	// Build binary header
	header := NotifyHeader{
		TotalLen:  uint32(totalLen),
		Unknown1:  0x00000200, // Fixed: was 0x00020000
		Unknown2:  0x00000200, // Fixed: was 0x00020000
		DateTime:  uint32(time.Now().Unix()),
		ID:        1,
		Cat:       1000,
		Level:     0,
		Title:     [8]byte{' ', ' ', ' ', ' ', ' ', ' ', ' ', ' '},
		Separator: 0x00,
	}

	// Serialize header
	buf := make([]byte, totalLen)
	offset := 0

	binary.LittleEndian.PutUint32(buf[offset:], header.TotalLen)
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:], header.Unknown1)
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:], header.Unknown2)
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:], header.DateTime)
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:], header.ID)
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:], header.Cat)
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:], header.Level)
	offset += 4
	copy(buf[offset:], header.Title[:])
	offset += 8
	buf[offset] = header.Separator
	offset += 1

	// Append JSON
	copy(buf[offset:], jsonBytes)
	offset += len(jsonBytes)

	// Append trailing
	copy(buf[offset:], trailing)

	return buf, nil
}

// sendToFD sends message to a specific FD
func (n *Notifier) sendToFD(pid int, fd int, message []byte) error {
	// Duplicate the FD without caching (use once and discard)
	dupFD, err := n.pidfdMgr.DuplicateSocketFDNocache(pid, fd)
	if err != nil {
		return fmt.Errorf("failed to duplicate FD %d: %w", fd, err)
	}
	defer syscall.Close(dupFD)

	// Send the message
	written, err := syscall.Write(dupFD, message)
	if err != nil {
		return fmt.Errorf("failed to write: %w", err)
	}
	if written != len(message) {
		return fmt.Errorf("partial write: %d/%d bytes", written, len(message))
	}

	return nil
}

// printMessageHexDump prints hex dump of the constructed notification message
func (n *Notifier) printMessageHexDump(message []byte, appName string, state string) {
	log.Printf("📤 [SENDING] Notification for %s (%s), Length: %d bytes", appName, state, len(message))
	log.Printf("📊 [SENDING] Hex dump of our message:")

	// Print hex dump in chunks of 16 bytes for readability
	for i := 0; i < len(message); i += 16 {
		end := i + 16
		if end > len(message) {
			end = len(message)
		}
		chunk := message[i:end]
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

		// Format hex with spaces
		var formattedHex strings.Builder
		for k := 0; k < len(hexStr); k += 2 {
			if k > 0 {
				formattedHex.WriteByte(' ')
			}
			formattedHex.WriteString(hexStr[k : k+2])
		}

		log.Printf("  %04x | %-48s | %s", i, formattedHex.String(), string(ascii))
	}
	log.Printf("---")
}
