package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ehrlich-b/cinch/internal/protocol"
	"github.com/ehrlich-b/cinch/internal/storage"
	"github.com/gorilla/websocket"
)

func TestHashToken(t *testing.T) {
	// Same input should produce same hash
	hash1 := hashToken("test-token")
	hash2 := hashToken("test-token")

	if hash1 != hash2 {
		t.Error("same token should produce same hash")
	}

	// Different input should produce different hash
	hash3 := hashToken("other-token")
	if hash1 == hash3 {
		t.Error("different tokens should produce different hashes")
	}

	// Hash should be hex encoded
	if len(hash1) != 64 { // SHA3-256 = 32 bytes = 64 hex chars
		t.Errorf("hash length = %d, want 64", len(hash1))
	}
}

func TestTokensEqual(t *testing.T) {
	if !TokensEqual("abc", "abc") {
		t.Error("same tokens should be equal")
	}
	if TokensEqual("abc", "xyz") {
		t.Error("different tokens should not be equal")
	}
	if TokensEqual("abc", "abcd") {
		t.Error("different length tokens should not be equal")
	}
}

func TestWSHandlerMissingToken(t *testing.T) {
	hub := NewHub()
	store, _ := storage.NewSQLite(":memory:", "", "")
	defer store.Close()

	handler := NewWSHandler(hub, store, nil)

	req := httptest.NewRequest("GET", "/ws", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestWSHandlerInvalidToken(t *testing.T) {
	hub := NewHub()
	store, _ := storage.NewSQLite(":memory:", "", "")
	defer store.Close()

	handler := NewWSHandler(hub, store, nil)

	req := httptest.NewRequest("GET", "/ws?token=invalid", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestWSFullConnection(t *testing.T) {
	hub := NewHub()
	store, _ := storage.NewSQLite(":memory:", "", "")
	defer store.Close()

	// Create token
	token := "test-token-123"
	tokenHash := hashToken(token)
	err := store.CreateToken(context.Background(), &storage.Token{
		ID:        "tok_1",
		Name:      "test",
		Hash:      tokenHash,
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}

	handler := NewWSHandler(hub, store, nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	// Connect with WebSocket
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// Should receive AUTH_OK
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read AUTH_OK failed: %v", err)
	}

	msgType, payload, err := protocol.Decode(msg)
	if err != nil {
		t.Fatalf("decode AUTH_OK failed: %v", err)
	}

	if msgType != protocol.TypeAuthOK {
		t.Errorf("message type = %s, want %s", msgType, protocol.TypeAuthOK)
	}

	authOK, err := protocol.DecodePayload[protocol.AuthOK](payload)
	if err != nil {
		t.Fatalf("decode AUTH_OK payload failed: %v", err)
	}

	if authOK.WorkerID != "tok_1" {
		t.Errorf("worker_id = %s, want tok_1", authOK.WorkerID)
	}

	// Send REGISTER
	regMsg, _ := protocol.Encode(protocol.TypeRegister, protocol.Register{
		Labels:       []string{"linux", "docker"},
		Capabilities: protocol.Capabilities{Docker: true},
		Version:      "0.1.0",
		Hostname:     "test-host",
	})
	if err := conn.WriteMessage(websocket.TextMessage, regMsg); err != nil {
		t.Fatalf("send REGISTER failed: %v", err)
	}

	// Should receive REGISTERED
	_, msg, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("read REGISTERED failed: %v", err)
	}

	msgType, _, err = protocol.Decode(msg)
	if err != nil {
		t.Fatalf("decode REGISTERED failed: %v", err)
	}

	if msgType != protocol.TypeRegistered {
		t.Errorf("message type = %s, want %s", msgType, protocol.TypeRegistered)
	}

	// Worker should be registered in hub
	time.Sleep(10 * time.Millisecond) // Give time for registration
	worker := hub.Get("tok_1")
	if worker == nil {
		t.Fatal("worker not found in hub")
	}

	if len(worker.Labels) != 2 {
		t.Errorf("labels = %v, want [linux docker]", worker.Labels)
	}
}

func TestWSHandlePing(t *testing.T) {
	hub := NewHub()
	store, _ := storage.NewSQLite(":memory:", "", "")
	defer store.Close()

	// Create token
	token := "test-token-ping"
	tokenHash := hashToken(token)
	err := store.CreateToken(context.Background(), &storage.Token{
		ID:        "tok_ping",
		Name:      "test",
		Hash:      tokenHash,
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}

	handler := NewWSHandler(hub, store, nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	// Connect
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// Read AUTH_OK
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read AUTH_OK failed: %v", err)
	}

	// Send REGISTER
	regMsg, _ := protocol.Encode(protocol.TypeRegister, protocol.Register{
		Labels:  []string{"linux"},
		Version: "0.1.0",
	})
	if err := conn.WriteMessage(websocket.TextMessage, regMsg); err != nil {
		t.Fatalf("send REGISTER failed: %v", err)
	}

	// Read REGISTERED
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read REGISTERED failed: %v", err)
	}

	// Send PING
	pingMsg, _ := protocol.Encode(protocol.TypePing, protocol.Ping{
		Timestamp:  time.Now().Unix(),
		ActiveJobs: []string{"j_1", "j_2"},
	})
	if err := conn.WriteMessage(websocket.TextMessage, pingMsg); err != nil {
		t.Fatalf("send PING failed: %v", err)
	}

	// Should receive PONG
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read PONG failed: %v", err)
	}

	msgType, _, err := protocol.Decode(msg)
	if err != nil {
		t.Fatalf("decode PONG failed: %v", err)
	}

	if msgType != protocol.TypePong {
		t.Errorf("message type = %s, want %s", msgType, protocol.TypePong)
	}
}
