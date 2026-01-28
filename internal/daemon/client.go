package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// DefaultSocketPath returns the default daemon socket path.
func DefaultSocketPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/cinch-daemon.sock"
	}
	return filepath.Join(home, ".cinch", "daemon.sock")
}

// Client connects to a daemon via Unix socket.
type Client struct {
	conn    net.Conn
	scanner *bufio.Scanner
}

// Connect connects to a daemon at the given socket path.
func Connect(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon: %w", err)
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	return &Client{
		conn:    conn,
		scanner: scanner,
	}, nil
}

// Close closes the connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Status requests the daemon's current status.
func (c *Client) Status() (*StatusResponse, error) {
	if err := c.send(TypeStatusRequest, nil); err != nil {
		return nil, err
	}

	msgType, payload, err := c.recv()
	if err != nil {
		return nil, err
	}

	if msgType == TypeError {
		errMsg, _ := DecodePayload[Error](payload)
		return nil, fmt.Errorf("daemon error: %s", errMsg.Message)
	}

	if msgType != TypeStatusResponse {
		return nil, fmt.Errorf("unexpected response type: %s", msgType)
	}

	resp, err := DecodePayload[StatusResponse](payload)
	if err != nil {
		return nil, err
	}

	return &resp, nil
}

// StartStream starts streaming events from the daemon.
func (c *Client) StartStream(jobID string, includeLogs bool) error {
	req := StreamRequest{
		JobID:       jobID,
		IncludeLogs: includeLogs,
	}
	return c.send(TypeStreamRequest, req)
}

// StopStream stops the event stream.
func (c *Client) StopStream() error {
	return c.send(TypeStreamStop, nil)
}

// ReadEvent reads the next event from the stream.
// Returns the message type and payload, or an error.
func (c *Client) ReadEvent() (string, json.RawMessage, error) {
	return c.recv()
}

// send sends a message to the daemon.
func (c *Client) send(msgType string, payload any) error {
	data, err := Encode(msgType, payload)
	if err != nil {
		return err
	}
	_, err = c.conn.Write(append(data, '\n'))
	return err
}

// recv receives a message from the daemon.
func (c *Client) recv() (string, json.RawMessage, error) {
	if !c.scanner.Scan() {
		if err := c.scanner.Err(); err != nil {
			return "", nil, err
		}
		return "", nil, fmt.Errorf("connection closed")
	}
	return Decode(c.scanner.Bytes())
}

// IsDaemonRunning checks if a daemon is running at the given socket path.
func IsDaemonRunning(socketPath string) bool {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
