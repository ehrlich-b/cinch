package server

import (
	"sync"
	"time"

	"github.com/ehrlich-b/cinch/internal/protocol"
)

// RelayConn represents a connected relay (self-hosted server).
type RelayConn struct {
	ID       string                                  // Unique relay ID (e.g., "x7k9m")
	UserID   string                                  // User who owns this relay
	Send     chan []byte                             // Outbound messages to the relay
	Pending  map[string]chan *protocol.RelayResponse // Pending requests: request_id -> response channel
	mu       sync.Mutex                              // Protects Pending map
	LastSeen time.Time
}

// NewRelayConn creates a new relay connection.
func NewRelayConn(id, userID string) *RelayConn {
	return &RelayConn{
		ID:       id,
		UserID:   userID,
		Send:     make(chan []byte, 256),
		Pending:  make(map[string]chan *protocol.RelayResponse),
		LastSeen: time.Now(),
	}
}

// AddPending registers a pending request and returns the response channel.
func (c *RelayConn) AddPending(requestID string) chan *protocol.RelayResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan *protocol.RelayResponse, 1)
	c.Pending[requestID] = ch
	return ch
}

// CompletePending delivers a response to a pending request.
func (c *RelayConn) CompletePending(requestID string, resp *protocol.RelayResponse) bool {
	c.mu.Lock()
	ch, ok := c.Pending[requestID]
	if ok {
		delete(c.Pending, requestID)
	}
	c.mu.Unlock()

	if ok {
		ch <- resp
		close(ch)
		return true
	}
	return false
}

// RemovePending removes a pending request (e.g., on timeout).
func (c *RelayConn) RemovePending(requestID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ch, ok := c.Pending[requestID]; ok {
		close(ch)
		delete(c.Pending, requestID)
	}
}

// RelayHub manages active relay connections.
type RelayHub struct {
	mu     sync.RWMutex
	relays map[string]*RelayConn // keyed by relay ID
}

// NewRelayHub creates a new relay hub.
func NewRelayHub() *RelayHub {
	return &RelayHub{
		relays: make(map[string]*RelayConn),
	}
}

// Register adds a relay to the hub.
func (h *RelayHub) Register(relay *RelayConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.relays[relay.ID] = relay
}

// Unregister removes a relay from the hub.
func (h *RelayHub) Unregister(relayID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if relay, ok := h.relays[relayID]; ok {
		close(relay.Send)
		delete(h.relays, relayID)
	}
}

// Get returns a relay by ID, or nil if not found.
func (h *RelayHub) Get(relayID string) *RelayConn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.relays[relayID]
}

// GetByUserID returns the relay for a user, or nil if not connected.
func (h *RelayHub) GetByUserID(userID string) *RelayConn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, relay := range h.relays {
		if relay.UserID == userID {
			return relay
		}
	}
	return nil
}

// Count returns the number of active relays.
func (h *RelayHub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.relays)
}
