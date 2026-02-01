package server

import (
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ehrlich-b/cinch/internal/protocol"
)

const (
	// relayTimeout is how long to wait for a response from the self-hosted server.
	relayTimeout = 30 * time.Second
)

// RelayHTTPHandler handles incoming webhooks and forwards them to self-hosted servers.
type RelayHTTPHandler struct {
	hub *RelayHub
	log *slog.Logger
}

// NewRelayHTTPHandler creates a new relay HTTP handler.
func NewRelayHTTPHandler(hub *RelayHub, log *slog.Logger) *RelayHTTPHandler {
	if log == nil {
		log = slog.Default()
	}
	return &RelayHTTPHandler{
		hub: hub,
		log: log,
	}
}

// ServeHTTP handles webhook requests and forwards them to the appropriate relay.
// URL format: /relay/{relay_id}/webhooks/{forge}
func (h *RelayHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse URL path: /relay/{relay_id}/webhooks/{forge}
	path := strings.TrimPrefix(r.URL.Path, "/relay/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		http.Error(w, "invalid relay path", http.StatusBadRequest)
		return
	}

	relayID := parts[0]
	// parts[1] should be "webhooks"
	// parts[2] if present is the forge type (optional)

	// Find the relay connection
	relay := h.hub.Get(relayID)
	if relay == nil {
		h.log.Warn("relay not found", "relay_id", relayID)
		http.Error(w, "relay not connected", http.StatusServiceUnavailable)
		return
	}

	// Read the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.log.Error("failed to read webhook body", "error", err)
		http.Error(w, "failed to read body", http.StatusInternalServerError)
		return
	}

	// Build headers map (only include relevant headers)
	headers := make(map[string]string)
	for _, key := range []string{
		"Content-Type",
		"X-GitHub-Event",
		"X-GitHub-Delivery",
		"X-Hub-Signature",
		"X-Hub-Signature-256",
		"X-GitLab-Event",
		"X-GitLab-Token",
		"X-Gitea-Event",
		"X-Gitea-Delivery",
		"X-Gitea-Signature",
		"X-Forgejo-Event",
		"X-Forgejo-Delivery",
		"X-Forgejo-Signature",
	} {
		if v := r.Header.Get(key); v != "" {
			headers[key] = v
		}
	}

	// Generate request ID
	requestID := fmt.Sprintf("req_%d", time.Now().UnixNano())

	// Build relay path (preserve the path after relay ID)
	relayPath := "/" + strings.Join(parts[1:], "/")

	// Create the relay request
	relayReq := protocol.RelayRequest{
		RequestID: requestID,
		Method:    r.Method,
		Path:      relayPath,
		Headers:   headers,
		Body:      base64.StdEncoding.EncodeToString(body),
	}

	// Encode the request
	msg, err := protocol.Encode(protocol.TypeRelayRequest, relayReq)
	if err != nil {
		h.log.Error("failed to encode relay request", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Register pending request
	respCh := relay.AddPending(requestID)

	// Send request to relay
	select {
	case relay.Send <- msg:
		// Sent successfully
	default:
		relay.RemovePending(requestID)
		h.log.Warn("relay send buffer full", "relay_id", relayID)
		http.Error(w, "relay busy", http.StatusServiceUnavailable)
		return
	}

	// Wait for response with timeout
	select {
	case resp := <-respCh:
		if resp == nil {
			// Channel closed (relay disconnected)
			http.Error(w, "relay disconnected", http.StatusServiceUnavailable)
			return
		}

		// Decode body
		respBody, err := base64.StdEncoding.DecodeString(resp.Body)
		if err != nil {
			h.log.Error("failed to decode relay response body", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Copy response headers
		for k, v := range resp.Headers {
			w.Header().Set(k, v)
		}

		// Write response
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)

	case <-time.After(relayTimeout):
		relay.RemovePending(requestID)
		h.log.Warn("relay request timeout", "relay_id", relayID, "request_id", requestID)
		http.Error(w, "relay timeout", http.StatusGatewayTimeout)
	}
}
