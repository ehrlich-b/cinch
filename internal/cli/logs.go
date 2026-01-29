package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// LogsOptions configures the logs command.
type LogsOptions struct {
	ServerURL string
	Token     string
	JobID     string
	Follow    bool
}

// LogEntry represents a log line from the API.
type LogEntry struct {
	Type   string `json:"type"`   // "log" or "status"
	Stream string `json:"stream"` // "stdout" or "stderr"
	Data   string `json:"data"`
	Time   string `json:"time"`
	Status string `json:"status"` // for status messages
}

// Logs streams logs from a job.
func Logs(ctx context.Context, opts LogsOptions, out io.Writer) error {
	// If not following, just fetch existing logs
	if !opts.Follow {
		return fetchLogs(opts, out)
	}

	// For follow mode, use WebSocket
	return streamLogs(ctx, opts, out)
}

// fetchLogs gets existing logs via HTTP.
func fetchLogs(opts LogsOptions, out io.Writer) error {
	apiURL := fmt.Sprintf("%s/api/jobs/%s/logs", opts.ServerURL, opts.JobID)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+opts.Token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("job not found: %s", opts.JobID)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var logs []struct {
		Stream string `json:"stream"`
		Data   string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&logs); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	for _, log := range logs {
		fmt.Fprint(out, log.Data)
	}

	return nil
}

// streamLogs streams logs via WebSocket.
func streamLogs(ctx context.Context, opts LogsOptions, out io.Writer) error {
	// Convert HTTP URL to WebSocket URL
	wsURL := strings.Replace(opts.ServerURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL = fmt.Sprintf("%s/ws/logs/%s", wsURL, opts.JobID)

	// Connect with auth header
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+opts.Token)

	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return fmt.Errorf("connect to log stream: %w", err)
	}
	defer conn.Close()

	// Read messages
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				return nil
			}
			// Connection closed, job probably finished
			if strings.Contains(err.Error(), "close") {
				return nil
			}
			return fmt.Errorf("read message: %w", err)
		}

		var entry LogEntry
		if err := json.Unmarshal(message, &entry); err != nil {
			continue
		}

		switch entry.Type {
		case "log":
			fmt.Fprint(out, entry.Data)
		case "status":
			if entry.Status == "success" || entry.Status == "failed" || entry.Status == "error" || entry.Status == "cancelled" {
				// Job finished
				return nil
			}
		}
	}
}
