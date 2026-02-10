package caltraingateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Line represents a transit line from the 511 API
type Line struct {
	ID            string `json:"Id"`
	Name          string `json:"Name"`
	FromDate      string `json:"FromDate"`
	ToDate        string `json:"ToDate"`
	TransportMode string `json:"TransportMode"`
	PublicCode    string `json:"PublicCode"`
	SiriLineRef   string `json:"SiriLineRef"`
	Monitored     bool   `json:"Monitored"`
	OperatorRef   string `json:"OperatorRef"`
}

// LoadLinesFromFile reads and parses a lines JSON file from the given filename.
func LoadLinesFromFile(filename string) ([]Line, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read lines file: %w", err)
	}

	return parseLinesJSON(data)
}

// LoadLinesFromURL fetches and parses lines JSON from the given URL.
func LoadLinesFromURL(url string) ([]Line, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch lines from URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return parseLinesJSON(data)
}

// parseLinesJSON parses the JSON data into a slice of Line
func parseLinesJSON(data []byte) ([]Line, error) {
	// Strip UTF-8 BOM if present
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	var lines []Line
	if err := json.Unmarshal(data, &lines); err != nil {
		return nil, fmt.Errorf("failed to parse lines JSON: %w", err)
	}
	return lines, nil
}

// GetLineIDs returns a slice of all line IDs
func GetLineIDs(lines []Line) []string {
	ids := make([]string, len(lines))
	for i, line := range lines {
		ids[i] = line.ID
	}
	return ids
}

// GetMonitoredLines returns only lines that are monitored
func GetMonitoredLines(lines []Line) []Line {
	var monitored []Line
	for _, line := range lines {
		if line.Monitored {
			monitored = append(monitored, line)
		}
	}
	return monitored
}
