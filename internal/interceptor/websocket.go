package interceptor

import (
	"encoding/binary"
	"fmt"
)

// WebSocketFrame represents a parsed WebSocket frame
type WebSocketFrame struct {
	FIN     bool
	Opcode  byte
	Masked  bool
	Payload []byte
}

// WebSocket opcodes
const (
	OpcodeContinuation = 0x0
	OpcodeText         = 0x1
	OpcodeBinary       = 0x2
	OpcodeClose        = 0x8
	OpcodePing         = 0x9
	OpcodePong         = 0xA
)

// ParseWebSocketFrames parses multiple WebSocket frames from data
func ParseWebSocketFrames(data []byte) ([]WebSocketFrame, error) {
	frames := []WebSocketFrame{}
	offset := 0

	for offset < len(data) {
		if offset+2 > len(data) {
			break // Not enough data for header
		}

		// Byte 0: FIN(1bit) + RSV(3bit) + opcode(4bit)
		byte0 := data[offset]
		fin := (byte0 & 0x80) != 0
		opcode := byte0 & 0x0F

		// Byte 1: MASK(1bit) + payload_len(7bit)
		byte1 := data[offset+1]
		masked := (byte1 & 0x80) != 0
		payloadLen := int(byte1 & 0x7F)

		headerSize := 2
		var actualPayloadLen int

		// Determine actual payload length
		if payloadLen <= 125 {
			actualPayloadLen = payloadLen
		} else if payloadLen == 126 {
			// 2-byte extended payload length
			if offset+4 > len(data) {
				break
			}
			actualPayloadLen = int(binary.BigEndian.Uint16(data[offset+2 : offset+4]))
			headerSize = 4
		} else { // payloadLen == 127
			// 8-byte extended payload length
			if offset+10 > len(data) {
				break
			}
			actualPayloadLen = int(binary.BigEndian.Uint64(data[offset+2 : offset+10]))
			headerSize = 10
		}

		// Check if we have enough data
		if offset+headerSize+actualPayloadLen > len(data) {
			break
		}

		// Extract payload
		payloadStart := offset + headerSize
		payloadEnd := payloadStart + actualPayloadLen
		payload := make([]byte, actualPayloadLen)
		copy(payload, data[payloadStart:payloadEnd])

		// Note: We don't handle masking as server->client frames are unmasked

		frames = append(frames, WebSocketFrame{
			FIN:     fin,
			Opcode:  opcode,
			Masked:  masked,
			Payload: payload,
		})

		offset = payloadEnd
	}

	return frames, nil
}

// EncodeWebSocketTextFrame encodes data as a WebSocket text frame
func EncodeWebSocketTextFrame(data []byte) []byte {
	dataLen := len(data)
	var frame []byte

	// Byte 0: FIN=1, RSV=0, opcode=1 (text)
	frame = append(frame, 0x81)

	// Byte 1+: payload length
	if dataLen <= 125 {
		frame = append(frame, byte(dataLen))
	} else if dataLen <= 65535 {
		frame = append(frame, 126)
		lenBytes := make([]byte, 2)
		binary.BigEndian.PutUint16(lenBytes, uint16(dataLen))
		frame = append(frame, lenBytes...)
	} else {
		frame = append(frame, 127)
		lenBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(lenBytes, uint64(dataLen))
		frame = append(frame, lenBytes...)
	}

	// Payload
	frame = append(frame, data...)

	return frame
}

// IsAppStoreListFrame checks if frame contains appStoreList data
func IsAppStoreListFrame(frame WebSocketFrame) bool {
	if frame.Opcode != OpcodeText {
		return false
	}
	payload := string(frame.Payload)
	// Check for appStoreList pattern
	return len(payload) > 100 &&
		(payload[0] == '{' || payload[1] == '{') && // JSON start
		(len(payload) > 20 && (payload[:20] == `{"result":"succ","` ||
			len(payload) > 30 && payload[20:50] != ""))
}

// FormatWebSocketFrameInfo returns a debug string for a frame
func FormatWebSocketFrameInfo(frame WebSocketFrame) string {
	opcodeStr := "UNKNOWN"
	switch frame.Opcode {
	case OpcodeText:
		opcodeStr = "TEXT"
	case OpcodeBinary:
		opcodeStr = "BINARY"
	case OpcodeClose:
		opcodeStr = "CLOSE"
	case OpcodePing:
		opcodeStr = "PING"
	case OpcodePong:
		opcodeStr = "PONG"
	}
	return fmt.Sprintf("Opcode=%s, FIN=%v, Len=%d", opcodeStr, frame.FIN, len(frame.Payload))
}
