package relay

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ehrlich-b/cinch/internal/protocol"
	"github.com/gorilla/websocket"
)

const (
	// writeWait is how long to wait for a write to complete.
	writeWait = 10 * time.Second

	// pongWait is how long to wait for a pong.
	pongWait = 90 * time.Second

	// pingPeriod is how often to send pings.
	pingPeriod = 30 * time.Second

	// reconnectDelay is how long to wait before reconnecting.
	reconnectDelay = 5 * time.Second
)

// Client connects a self-hosted server to cinch.sh for webhook relay.
type Client struct {
	relayURL   string // cinch.sh relay WebSocket URL (e.g., wss://cinch.sh/ws/relay)
	token      string // JWT token for authentication
	localAddr  string // Local server address (e.g., http://localhost:8080)
	log        *slog.Logger
	httpClient *http.Client

	mu              sync.Mutex
	conn            *websocket.Conn
	relayID         string
	relayWebhookURL string
	done            chan struct{}
}

// NewClient creates a new relay client.
func NewClient(relayURL, token, localAddr string, log *slog.Logger) *Client {
	if log == nil {
		log = slog.Default()
	}
	return &Client{
		relayURL:   relayURL,
		token:      token,
		localAddr:  localAddr,
		log:        log,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		done:       make(chan struct{}),
	}
}

// RelayURL returns the public webhook URL for this relay (once connected).
func (c *Client) RelayURL() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.relayWebhookURL
}

// Run starts the relay client and reconnects on failure.
func (c *Client) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := c.connect(ctx); err != nil {
			c.log.Warn("relay connection failed, reconnecting...", "error", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(reconnectDelay):
			}
			continue
		}

		c.readPump(ctx)

		// Clean up
		c.mu.Lock()
		if c.conn != nil {
			c.conn.Close()
			c.conn = nil
		}
		c.mu.Unlock()
	}
}

// Stop gracefully stops the client.
func (c *Client) Stop() {
	close(c.done)
	c.mu.Lock()
	if c.conn != nil {
		c.conn.Close()
	}
	c.mu.Unlock()
}

// connect establishes a WebSocket connection to the relay.
func (c *Client) connect(ctx context.Context) error {
	// Build WebSocket URL with token
	u, err := url.Parse(c.relayURL)
	if err != nil {
		return fmt.Errorf("parse relay URL: %w", err)
	}
	q := u.Query()
	q.Set("token", c.token)
	u.RawQuery = q.Encode()

	c.log.Info("connecting to relay", "url", c.relayURL)

	// Connect
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return fmt.Errorf("connect to relay: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	// Wait for RELAY_READY
	_, msg, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read RELAY_READY: %w", err)
	}

	msgType, payload, err := protocol.Decode(msg)
	if err != nil {
		return fmt.Errorf("decode message: %w", err)
	}

	if msgType != protocol.TypeRelayReady {
		return fmt.Errorf("expected RELAY_READY, got %s", msgType)
	}

	ready, err := protocol.DecodePayload[protocol.RelayReady](payload)
	if err != nil {
		return fmt.Errorf("decode RELAY_READY: %w", err)
	}

	c.mu.Lock()
	c.relayID = ready.RelayID
	c.relayWebhookURL = ready.RelayURL
	c.mu.Unlock()

	c.log.Info("relay connected",
		"relay_id", ready.RelayID,
		"webhook_url", ready.RelayURL,
	)

	return nil
}

// readPump reads messages from the relay and handles them.
func (c *Client) readPump(ctx context.Context) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return
	}

	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Start ping goroutine
	go c.pingPump(ctx, conn)

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		default:
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.log.Warn("relay read error", "error", err)
			}
			return
		}

		c.handleMessage(ctx, msg)
	}
}

// pingPump sends periodic pings.
func (c *Client) pingPump(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-ticker.C:
			if err := conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage processes incoming relay messages.
func (c *Client) handleMessage(ctx context.Context, data []byte) {
	msgType, payload, err := protocol.Decode(data)
	if err != nil {
		c.log.Warn("failed to decode relay message", "error", err)
		return
	}

	switch msgType {
	case protocol.TypeRelayRequest:
		c.handleRelayRequest(ctx, payload)
	default:
		c.log.Warn("unknown relay message type", "type", msgType)
	}
}

// handleRelayRequest processes an incoming webhook request.
func (c *Client) handleRelayRequest(ctx context.Context, payload []byte) {
	req, err := protocol.DecodePayload[protocol.RelayRequest](payload)
	if err != nil {
		c.log.Warn("failed to decode RELAY_REQUEST", "error", err)
		return
	}

	c.log.Debug("handling relay request",
		"request_id", req.RequestID,
		"method", req.Method,
		"path", req.Path,
	)

	// Decode body
	body, err := base64.StdEncoding.DecodeString(req.Body)
	if err != nil {
		c.sendError(req.RequestID, 400, "invalid body encoding")
		return
	}

	// Build local request
	localURL := strings.TrimSuffix(c.localAddr, "/") + req.Path
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, localURL, bytes.NewReader(body))
	if err != nil {
		c.sendError(req.RequestID, 500, "failed to create request")
		return
	}

	// Copy headers
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.log.Warn("local request failed", "error", err)
		c.sendError(req.RequestID, 502, "local server unavailable")
		return
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.sendError(req.RequestID, 500, "failed to read response")
		return
	}

	// Build response headers
	respHeaders := make(map[string]string)
	for k := range resp.Header {
		respHeaders[k] = resp.Header.Get(k)
	}

	// Send response
	relayResp := protocol.RelayResponse{
		RequestID:  req.RequestID,
		StatusCode: resp.StatusCode,
		Headers:    respHeaders,
		Body:       base64.StdEncoding.EncodeToString(respBody),
	}

	c.sendResponse(relayResp)
}

// sendResponse sends a relay response.
func (c *Client) sendResponse(resp protocol.RelayResponse) {
	msg, err := protocol.Encode(protocol.TypeRelayResponse, resp)
	if err != nil {
		c.log.Error("failed to encode response", "error", err)
		return
	}

	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return
	}

	if err := conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return
	}
	if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
		c.log.Warn("failed to send response", "error", err)
	}
}

// sendError sends an error response.
func (c *Client) sendError(requestID string, statusCode int, message string) {
	c.sendResponse(protocol.RelayResponse{
		RequestID:  requestID,
		StatusCode: statusCode,
		Headers:    map[string]string{"Content-Type": "text/plain"},
		Body:       base64.StdEncoding.EncodeToString([]byte(message)),
	})
}
