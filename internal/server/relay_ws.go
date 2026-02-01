package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/ehrlich-b/cinch/internal/protocol"
	"github.com/ehrlich-b/cinch/internal/storage"
	"github.com/gorilla/websocket"
)

// RelayWSHandler handles WebSocket connections from self-hosted servers.
type RelayWSHandler struct {
	hub          *RelayHub
	storage      storage.Storage
	log          *slog.Logger
	baseURL      string
	jwtValidator JWTValidator
}

// NewRelayWSHandler creates a new relay WebSocket handler.
func NewRelayWSHandler(hub *RelayHub, store storage.Storage, baseURL string, log *slog.Logger) *RelayWSHandler {
	if log == nil {
		log = slog.Default()
	}
	return &RelayWSHandler{
		hub:     hub,
		storage: store,
		baseURL: baseURL,
		log:     log,
	}
}

// SetJWTValidator sets the JWT validator for authentication.
func (h *RelayWSHandler) SetJWTValidator(v JWTValidator) {
	h.jwtValidator = v
}

// ServeHTTP handles WebSocket upgrade requests for relay connections.
func (h *RelayWSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	// Validate JWT token to get user
	if h.jwtValidator == nil {
		http.Error(w, "relay not configured", http.StatusServiceUnavailable)
		return
	}

	email := h.jwtValidator.ValidateUserToken(token)
	if email == "" {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// Look up user to get their relay ID
	ctx := r.Context()
	user, err := h.storage.GetUserByEmail(ctx, email)
	if err != nil {
		h.log.Error("failed to get user for relay", "email", email, "error", err)
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	// Get or create relay ID for this user
	relayID, err := h.storage.GetOrCreateRelayID(ctx, user.ID)
	if err != nil {
		h.log.Error("failed to get relay ID", "user_id", user.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Upgrade to WebSocket
	conn, err := workerUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Error("websocket upgrade failed", "error", err)
		return
	}

	// Create relay connection
	relay := NewRelayConn(relayID, user.ID)

	h.log.Info("relay connected", "relay_id", relayID, "user", email)

	// Send RELAY_READY with the relay URL
	relayURL := h.baseURL + "/relay/" + relayID + "/webhooks"
	readyMsg, err := protocol.Encode(protocol.TypeRelayReady, protocol.RelayReady{
		RelayID:  relayID,
		RelayURL: relayURL,
	})
	if err != nil {
		h.log.Error("failed to encode RELAY_READY", "error", err)
		conn.Close()
		return
	}
	if err := conn.WriteMessage(websocket.TextMessage, readyMsg); err != nil {
		h.log.Error("failed to send RELAY_READY", "error", err)
		conn.Close()
		return
	}

	// Register with hub
	h.hub.Register(relay)

	// Start read/write pumps
	go h.writePump(conn, relay)
	go h.readPump(conn, relay)
}

// readPump reads messages from the relay connection.
func (h *RelayWSHandler) readPump(conn *websocket.Conn, relay *RelayConn) {
	defer func() {
		h.hub.Unregister(relay.ID)
		conn.Close()
		h.log.Info("relay disconnected", "relay_id", relay.ID)
	}()

	conn.SetReadLimit(maxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				h.log.Warn("relay read error", "relay_id", relay.ID, "error", err)
			}
			return
		}

		h.handleMessage(relay, message)
	}
}

// writePump sends messages to the relay connection.
func (h *RelayWSHandler) writePump(conn *websocket.Conn, relay *RelayConn) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		conn.Close()
	}()

	for {
		select {
		case message, ok := <-relay.Send:
			if err := conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if !ok {
				// Channel closed
				_ = conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
				h.log.Warn("relay write error", "relay_id", relay.ID, "error", err)
				return
			}

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

// handleMessage processes messages from the relay.
func (h *RelayWSHandler) handleMessage(relay *RelayConn, data []byte) {
	msgType, payload, err := protocol.Decode(data)
	if err != nil {
		h.log.Warn("failed to decode relay message", "relay_id", relay.ID, "error", err)
		return
	}

	switch msgType {
	case protocol.TypeRelayResponse:
		h.handleRelayResponse(relay, payload)
	default:
		h.log.Warn("unknown relay message type", "relay_id", relay.ID, "type", msgType)
	}
}

// handleRelayResponse processes a response from the self-hosted server.
func (h *RelayWSHandler) handleRelayResponse(relay *RelayConn, payload []byte) {
	resp, err := protocol.DecodePayload[protocol.RelayResponse](payload)
	if err != nil {
		h.log.Warn("failed to decode RELAY_RESPONSE", "relay_id", relay.ID, "error", err)
		return
	}

	if !relay.CompletePending(resp.RequestID, &resp) {
		h.log.Warn("no pending request for response", "relay_id", relay.ID, "request_id", resp.RequestID)
	}
}
