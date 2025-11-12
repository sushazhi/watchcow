package interceptor

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
)

// AppInfo represents an application in the response
type AppInfo struct {
	AppName   string      `json:"appName"`
	AppID     string      `json:"appID"`
	EntryName string      `json:"entryName"`
	Title     string      `json:"title"`
	Desc      string      `json:"desc"`
	Icon      string      `json:"icon"`
	Type      string      `json:"type"`
	URI       interface{} `json:"uri"`
	MicroApp  bool        `json:"microApp"`
	NativeApp bool        `json:"nativeApp"`
	FullURL   string      `json:"fullUrl"`
	Status    string      `json:"status"`
	FileTypes []string    `json:"fileTypes"`
	IsDisplay bool        `json:"isDisplay"`
	Category  string      `json:"category,omitempty"` // Keep this for future use
}

// Response represents the WebSocket response structure
// Note: The actual structure is nested with an outer "data" wrapper
type Response struct {
	Data struct {
		Result string `json:"result"`
		ReqID  string `json:"reqid"`
		Data   struct {
			List []AppInfo `json:"list"`
		} `json:"data"`
	} `json:"data"`
}

// DataProcessor handles the processing of intercepted data
type DataProcessor struct {
	mu         sync.RWMutex // Protects dockerApps
	dockerApps []AppInfo    // Will be populated from Docker monitoring
	updateCh   chan []AppInfo
}

// NewDataProcessor creates a new DataProcessor
func NewDataProcessor() *DataProcessor {
	return &DataProcessor{
		dockerApps: []AppInfo{}, // Start empty, will be updated by Docker monitor
		updateCh:   make(chan []AppInfo, 1),
	}
}

// GetUpdateChannel returns the channel for receiving Docker app updates
func (dp *DataProcessor) GetUpdateChannel() chan<- []AppInfo {
	return dp.updateCh
}

// StartUpdateListener starts listening for Docker app updates
func (dp *DataProcessor) StartUpdateListener() {
	go func() {
		for apps := range dp.updateCh {
			dp.UpdateDockerApps(apps)
			if len(apps) > 0 {
				log.Printf("🐳 Updated Docker app list: %d containers", len(apps))
			}
		}
	}()
}

// IsAppStoreListResponse checks if the data contains an appStoreList response
func (dp *DataProcessor) IsAppStoreListResponse(data []byte) bool {
	// Check for the presence of key indicators
	// The response should contain "appcgi.sac.entry.v1.appStoreList" or its response pattern
	dataStr := string(data)

	// Check if it's a response (has result field and list)
	if strings.Contains(dataStr, `"result":"succ"`) &&
		strings.Contains(dataStr, `"reqid"`) &&
		strings.Contains(dataStr, `"list":[`) {
		return true
	}

	return false
}

// ExtractReqID extracts the request ID from the response
func (dp *DataProcessor) ExtractReqID(data []byte) (string, error) {
	var response Response

	// Find JSON start (might have WebSocket framing or binary prefix)
	jsonStart := findJSONStart(data)
	if jsonStart == -1 {
		return "", fmt.Errorf("no JSON found in data")
	}

	jsonData := data[jsonStart:]
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return response.Data.ReqID, nil
}

// InjectDockerApps creates a completely new response with Docker apps injected
func (dp *DataProcessor) InjectDockerApps(originalData []byte) ([]byte, error) {
	// Parse original to get structure and reqid
	jsonStart := findJSONStart(originalData)
	if jsonStart == -1 {
		return nil, fmt.Errorf("no JSON found in data")
	}

	prefix := originalData[:jsonStart]
	jsonData := originalData[jsonStart:]

	var response Response
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Create new response with both original and Docker apps
	newResponse := Response{}
	newResponse.Data.Result = response.Data.Result
	newResponse.Data.ReqID = response.Data.ReqID

	// Copy original apps
	newResponse.Data.Data.List = make([]AppInfo, len(response.Data.Data.List))
	copy(newResponse.Data.Data.List, response.Data.Data.List)

	// Add Docker apps (with read lock)
	dp.mu.RLock()
	newResponse.Data.Data.List = append(newResponse.Data.Data.List, dp.dockerApps...)
	dp.mu.RUnlock()

	// Marshal to JSON
	newJSON, err := json.Marshal(newResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal new response: %w", err)
	}

	// Update length field in binary header (offset 10-11, little-endian uint16)
	if len(prefix) >= 12 {
		newLen := uint16(len(newJSON))
		prefix[10] = byte(newLen & 0xFF)        // Low byte
		prefix[11] = byte((newLen >> 8) & 0xFF) // High byte
	}

	// Combine with updated prefix
	result := append(prefix, newJSON...)

	return result, nil
}

// findJSONStart finds the start of JSON in the data (skipping WebSocket framing or binary prefix)
func findJSONStart(data []byte) int {
	for i := 0; i < len(data); i++ {
		if data[i] == '{' {
			return i
		}
	}
	return -1
}

// UpdateDockerApps updates the list of Docker apps (called by Docker monitor)
func (dp *DataProcessor) UpdateDockerApps(apps []AppInfo) {
	dp.mu.Lock()
	dp.dockerApps = apps
	dp.mu.Unlock()
}
