//go:build linux

package ebpf

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"

	"watchcow/internal/interceptor"
)

// Generate eBPF bindings from C source
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -D__TARGET_ARCH_x86" -tags linux unixhook ../../bpf/unix_hook.c -- -I../../bpf

// Event matches the sendmsg_event structure in common.h
type Event struct {
	Pid         uint32
	Tid         uint32
	Fd          uint32
	DataLen     uint32
	Timestamp   uint64
	DebugStep   uint32
	DebugIovLen uint32
	Flags       uint32 // Event flags (FLAG_APPSTORE, etc)
	SocketPath  [108]byte
	Data        [4096]byte
}

// Manager manages eBPF programs and maps
type Manager struct {
	objs    *unixhookObjects
	links   []link.Link
	ringbuf *ringbuf.Reader

	interceptor *interceptor.Interceptor
	stopCh      chan struct{}
}

// NewManager creates a new eBPF manager
func NewManager() (*Manager, error) {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("failed to remove memlock: %w", err)
	}

	mgr := &Manager{
		interceptor: interceptor.NewInterceptor(),
		stopCh:      make(chan struct{}),
	}

	return mgr, nil
}

// LoadAndAttach loads and attaches eBPF programs
func (m *Manager) LoadAndAttach() error {
	// Load pre-compiled eBPF objects
	objs := unixhookObjects{}
	if err := loadUnixhookObjects(&objs, nil); err != nil {
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}
	m.objs = &objs

	// Attach to write syscall tracepoint
	tp, err := link.Tracepoint("syscalls", "sys_enter_write", objs.TraceWrite, nil)
	if err != nil {
		return fmt.Errorf("failed to attach write tracepoint: %w", err)
	}
	m.links = append(m.links, tp)

	// Open ring buffer reader
	m.ringbuf, err = ringbuf.NewReader(objs.Events)
	if err != nil {
		return fmt.Errorf("failed to create ringbuf reader: %w", err)
	}

	return nil
}

// Start starts processing events
func (m *Manager) Start(ctx context.Context) error {
	go m.processEvents(ctx)
	return nil
}

// processEvents processes events from the ring buffer
func (m *Manager) processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		default:
			record, err := m.ringbuf.Read()
			if err != nil {
				if errors.Is(err, ringbuf.ErrClosed) {
					return
				}
				log.Printf("⚠️  Ring buffer error: %v", err)
				continue
			}

			// Parse the event
			event, err := parseEvent(record.RawSample)
			if err != nil {
				continue
			}

			// Convert to interceptor event
			intEvent := &interceptor.Event{
				PID:         event.Pid,
				TID:         event.Tid,
				FD:          event.Fd,
				DataLen:     event.DataLen,
				Timestamp:   event.Timestamp,
				DebugStep:   event.DebugStep,
				DebugIovLen: event.DebugIovLen,
				Flags:       event.Flags,
				SocketPath:  string(bytes.TrimRight(event.SocketPath[:], "\x00")),
				Data:        event.Data[:event.DataLen],
			}

			// Process the event
			if err := m.interceptor.ProcessEvent(ctx, intEvent); err != nil {
				log.Printf("⚠️  Event processing error: %v", err)
			}
		}
	}
}

// parseEvent parses raw bytes into an Event structure
func parseEvent(raw []byte) (*Event, error) {
	if len(raw) < int(unsafe.Sizeof(Event{})) {
		return nil, fmt.Errorf("raw data too small: %d bytes", len(raw))
	}

	event := &Event{}
	reader := bytes.NewReader(raw)

	// Read fields in order (must match C struct layout)
	if err := binary.Read(reader, binary.LittleEndian, &event.Pid); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &event.Tid); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &event.Fd); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &event.DataLen); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &event.Timestamp); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &event.DebugStep); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &event.DebugIovLen); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &event.Flags); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &event.SocketPath); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &event.Data); err != nil {
		return nil, err
	}

	return event, nil
}

// GetStats returns statistics
func (m *Manager) GetStats() (received, processed, injected uint64) {
	if m.interceptor != nil {
		return m.interceptor.GetStats()
	}
	return 0, 0, 0
}

// StartDockerAppListener starts the Docker app update listener
func (m *Manager) StartDockerAppListener() {
	if m.interceptor != nil {
		m.interceptor.StartDockerAppListener()
	}
}

// GetDockerAppUpdateChannel returns the channel for Docker app updates
func (m *Manager) GetDockerAppUpdateChannel() chan<- []interceptor.AppInfo {
	if m.interceptor != nil {
		return m.interceptor.GetDockerAppUpdateChannel()
	}
	return nil
}

// GetInterceptor returns the interceptor instance
func (m *Manager) GetInterceptor() *interceptor.Interceptor {
	return m.interceptor
}

// SetDebug enables or disables debug mode
func (m *Manager) SetDebug(debug bool) {
	if m.interceptor != nil {
		m.interceptor.SetDebug(debug)
	}
}

// Stop stops the manager and cleans up
func (m *Manager) Stop() {

	// Signal stop
	close(m.stopCh)

	// Close ring buffer
	if m.ringbuf != nil {
		m.ringbuf.Close()
	}

	// Detach links
	for _, l := range m.links {
		if l != nil {
			l.Close()
		}
	}

	// Close interceptor
	if m.interceptor != nil {
		m.interceptor.Close()
	}

	// Close eBPF objects
	if m.objs != nil {
		m.objs.Close()
	}

	log.Printf("[eBPF] Stopped and cleaned up")
}
