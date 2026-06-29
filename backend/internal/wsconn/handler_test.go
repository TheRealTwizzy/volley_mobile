package wsconn_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorillaws "github.com/gorilla/websocket"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/protocol"
	"github.com/pong-mobile/backend/internal/wsconn"
)

func dialTest(t *testing.T, srv *httptest.Server) *gorillaws.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := gorillaws.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func readJSON(t *testing.T, conn *gorillaws.Conn) map[string]any {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("invalid JSON from server: %v", err)
	}
	return out
}

func sendHello(t *testing.T, conn *gorillaws.Conn, displayName string) {
	t.Helper()
	msg := map[string]any{
		"type":      "client.hello",
		"requestId": "req_001",
		"sentAt":    time.Now().UnixMilli(),
		"payload": map[string]any{
			"clientVersion": "0.1.0",
			"platform":      "android",
			"sessionToken":  nil,
			"displayName":   displayName,
		},
	}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("write hello: %v", err)
	}
}

func TestHandler_HelloHandshake(t *testing.T) {
	store := auth.NewStore()
	srv := httptest.NewServer(wsconn.Handler(store))
	defer srv.Close()

	conn := dialTest(t, srv)
	sendHello(t, conn, "TestPlayer")

	msg := readJSON(t, conn)
	if msg["type"] != protocol.TypeServerHello {
		t.Fatalf("expected server.hello, got %v", msg["type"])
	}
	payload, ok := msg["payload"].(map[string]any)
	if !ok {
		t.Fatal("payload not an object")
	}
	if payload["sessionToken"] == nil || payload["sessionToken"] == "" {
		t.Error("server.hello must include sessionToken")
	}
	if payload["playerId"] == nil || payload["playerId"] == "" {
		t.Error("server.hello must include playerId")
	}
	hi, ok := payload["heartbeatIntervalMs"].(float64)
	if !ok || hi <= 0 {
		t.Errorf("heartbeatIntervalMs should be positive, got %v", payload["heartbeatIntervalMs"])
	}
}

func TestHandler_UnknownType_ReturnsError(t *testing.T) {
	store := auth.NewStore()
	srv := httptest.NewServer(wsconn.Handler(store))
	defer srv.Close()

	conn := dialTest(t, srv)
	sendHello(t, conn, "T")
	readJSON(t, conn) // consume server.hello

	conn.WriteJSON(map[string]any{
		"type": "unknown.thing", "requestId": "r2",
		"sentAt": time.Now().UnixMilli(), "payload": map[string]any{},
	})

	msg := readJSON(t, conn)
	if msg["type"] != protocol.TypeError {
		t.Fatalf("expected error message, got %v", msg["type"])
	}
	payload := msg["payload"].(map[string]any)
	if payload["code"] != "unknown_type" {
		t.Errorf("expected unknown_type, got %v", payload["code"])
	}
}

func TestHandler_BadJSON_ReturnsError(t *testing.T) {
	store := auth.NewStore()
	srv := httptest.NewServer(wsconn.Handler(store))
	defer srv.Close()

	conn := dialTest(t, srv)

	conn.WriteMessage(gorillaws.TextMessage, []byte("this is not json"))

	msg := readJSON(t, conn)
	if msg["type"] != protocol.TypeError {
		t.Fatalf("expected error message, got %v", msg["type"])
	}
	payload := msg["payload"].(map[string]any)
	if payload["code"] != "bad_message" {
		t.Errorf("expected bad_message, got %v", payload["code"])
	}
}

func TestHandler_ClientPong_KeepsConnectionAlive(t *testing.T) {
	store := auth.NewStore()
	srv := httptest.NewServer(wsconn.Handler(store))
	defer srv.Close()

	conn := dialTest(t, srv)
	sendHello(t, conn, "T")
	readJSON(t, conn) // consume server.hello

	// Send a client.pong — should not cause an error response
	conn.WriteJSON(map[string]any{
		"type": "client.pong", "sentAt": time.Now().UnixMilli(),
		"payload": map[string]any{"pingId": "ping_1"},
	})

	// Follow with unknown type to confirm connection is still healthy
	conn.WriteJSON(map[string]any{
		"type": "unknown.thing", "requestId": "r3",
		"sentAt": time.Now().UnixMilli(), "payload": map[string]any{},
	})
	msg := readJSON(t, conn)
	if msg["type"] != protocol.TypeError {
		t.Errorf("expected error response after pong, got %v", msg["type"])
	}
}

func TestHandler_Upgrade_NonWebSocket_Returns400(t *testing.T) {
	store := auth.NewStore()
	srv := httptest.NewServer(wsconn.Handler(store))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for non-WS request, got %d", resp.StatusCode)
	}
}
