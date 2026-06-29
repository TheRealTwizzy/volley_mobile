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
	"github.com/pong-mobile/backend/internal/lobby"
	"github.com/pong-mobile/backend/internal/matchmgr"
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

func newTestHandler(t *testing.T) (store *auth.Store, mgr *lobby.Manager, srv *httptest.Server) {
	t.Helper()
	store = auth.NewStore()
	mmgr := matchmgr.NewManager(nil)
	mgr = lobby.NewManager(func(r *lobby.Room) {})
	srv = httptest.NewServer(wsconn.Handler(store, mgr, mmgr))
	t.Cleanup(srv.Close)
	return
}

func TestHandler_HelloHandshake(t *testing.T) {
	store, mgr, srv := newTestHandler(t)
	_ = mgr

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
	_ = store
}

func TestHandler_UnknownType_ReturnsError(t *testing.T) {
	_, _, srv := newTestHandler(t)

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
	_, _, srv := newTestHandler(t)

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
	_, _, srv := newTestHandler(t)

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
	_, _, srv := newTestHandler(t)

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for non-WS request, got %d", resp.StatusCode)
	}
}

func TestHandler_RoomCreate_Success(t *testing.T) {
	store := auth.NewStore()
	mgr := lobby.NewManager(func(r *lobby.Room) {})
	mmgr := matchmgr.NewManager(nil)
	srv := httptest.NewServer(wsconn.Handler(store, mgr, mmgr))
	defer srv.Close()

	conn := dialTest(t, srv)
	sendHello(t, conn, "Host")
	readJSON(t, conn) // consume server.hello

	conn.WriteJSON(map[string]any{
		"type":      "room.create",
		"requestId": "req_r1",
		"sentAt":    time.Now().UnixMilli(),
		"payload":   map[string]any{"settings": map[string]any{}},
	})

	msg := readJSON(t, conn)
	if msg["type"] != "room.created" {
		t.Fatalf("expected room.created, got %v", msg["type"])
	}
	payload := msg["payload"].(map[string]any)
	code, _ := payload["roomCode"].(string)
	if len(code) != 6 {
		t.Errorf("roomCode should be 6 chars, got %q", code)
	}
	if payload["hostPlayerId"] == nil || payload["hostPlayerId"] == "" {
		t.Error("room.created must include hostPlayerId")
	}
}

func TestHandler_RoomMessage_WithoutHello_Unauthorized(t *testing.T) {
	store := auth.NewStore()
	mgr := lobby.NewManager(func(r *lobby.Room) {})
	mmgr := matchmgr.NewManager(nil)
	srv := httptest.NewServer(wsconn.Handler(store, mgr, mmgr))
	defer srv.Close()

	conn := dialTest(t, srv)
	// Skip hello — send room.create directly
	conn.WriteJSON(map[string]any{
		"type":      "room.create",
		"requestId": "req_r2",
		"sentAt":    time.Now().UnixMilli(),
		"payload":   map[string]any{"settings": map[string]any{}},
	})

	msg := readJSON(t, conn)
	if msg["type"] != "error" {
		t.Fatalf("expected error, got %v", msg["type"])
	}
	payload := msg["payload"].(map[string]any)
	if payload["code"] != "unauthorized" {
		t.Errorf("expected unauthorized, got %v", payload["code"])
	}
}

// readUntil reads messages from conn until it finds one with the given type or
// times out. Returns the matching message or calls t.Fatal.
func readUntil(t *testing.T, conn *gorillaws.Conn, wantType string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(deadline)
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("readUntil %q: read error: %v", wantType, err)
		}
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("readUntil %q: invalid JSON: %v", wantType, err)
		}
		if msg["type"] == wantType {
			return msg
		}
	}
	t.Fatalf("readUntil: timed out waiting for %q", wantType)
	return nil
}

// TestHandler_MatchStartFlow is an end-to-end integration test:
// two clients connect, create/join a room, both go ready, and the match
// starts (countdown → started → at least one snapshot arrives).
func TestHandler_MatchStartFlow(t *testing.T) {
	store := auth.NewStore()
	mmgr := matchmgr.NewManager(nil)
	mgr := lobby.NewManager(func(r *lobby.Room) {
		mmgr.StartMatch(r)
	})
	srv := httptest.NewServer(wsconn.Handler(store, mgr, mmgr))
	defer srv.Close()

	// Connect two clients.
	c1 := dialTest(t, srv)
	c2 := dialTest(t, srv)

	// ---- Hello handshake ----
	sendHello(t, c1, "Player1")
	msg := readUntil(t, c1, protocol.TypeServerHello, 2*time.Second)
	if msg["type"] != protocol.TypeServerHello {
		t.Fatalf("c1: expected server.hello")
	}

	sendHello(t, c2, "Player2")
	msg = readUntil(t, c2, protocol.TypeServerHello, 2*time.Second)
	if msg["type"] != protocol.TypeServerHello {
		t.Fatalf("c2: expected server.hello")
	}

	// ---- c1 creates room ----
	c1.WriteJSON(map[string]any{
		"type":      "room.create",
		"requestId": "req_create",
		"sentAt":    time.Now().UnixMilli(),
		"payload":   map[string]any{"settings": map[string]any{}},
	})

	created := readUntil(t, c1, "room.created", 2*time.Second)
	roomCode, _ := created["payload"].(map[string]any)["roomCode"].(string)
	if roomCode == "" {
		t.Fatal("room.created: missing roomCode")
	}

	// ---- c2 joins room ----
	c2.WriteJSON(map[string]any{
		"type":      "room.join",
		"requestId": "req_join",
		"sentAt":    time.Now().UnixMilli(),
		"payload":   map[string]any{"roomCode": roomCode},
	})

	// Both should get room.updated (c1 gets the join notification, c2 gets its own join ack).
	readUntil(t, c1, "room.updated", 2*time.Second)
	readUntil(t, c2, "room.updated", 2*time.Second)

	// ---- c1 ready ----
	c1.WriteJSON(map[string]any{
		"type":      "room.ready",
		"requestId": "req_ready1",
		"sentAt":    time.Now().UnixMilli(),
		"payload":   map[string]any{"ready": true},
	})
	readUntil(t, c1, "room.updated", 2*time.Second)
	readUntil(t, c2, "room.updated", 2*time.Second)

	// ---- c2 ready → triggers match start ----
	c2.WriteJSON(map[string]any{
		"type":      "room.ready",
		"requestId": "req_ready2",
		"sentAt":    time.Now().UnixMilli(),
		"payload":   map[string]any{"ready": true},
	})
	readUntil(t, c1, "room.updated", 2*time.Second)
	readUntil(t, c2, "room.updated", 2*time.Second)

	// ---- Expect match.countdown ----
	readUntil(t, c1, protocol.TypeMatchCountdown, 2*time.Second)
	readUntil(t, c2, protocol.TypeMatchCountdown, 2*time.Second)

	// ---- Expect match.started (after ~3s countdown) ----
	readUntil(t, c1, protocol.TypeMatchStarted, 5*time.Second)
	readUntil(t, c2, protocol.TypeMatchStarted, 5*time.Second)

	// ---- Expect at least one match.snapshot ----
	readUntil(t, c1, protocol.TypeMatchSnapshot, 3*time.Second)
}
