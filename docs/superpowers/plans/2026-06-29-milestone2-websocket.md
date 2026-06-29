# Milestone 2: WebSocket Skeleton Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up a WebSocket server where a client can connect, complete the hello handshake, receive a session token, and exchange heartbeat ping/pong messages.

**Architecture:** Three new packages: `protocol` owns message envelope types and JSON marshal/unmarshal; `auth` owns session token generation and an in-memory session store; `wsconn` owns the HTTP upgrade handler, per-connection read/write loops, and message dispatch. `main.go` wires them into a `net/http` server on `:8080`. No lobby or match logic yet.

**Tech Stack:** Go 1.22+, `github.com/gorilla/websocket` v1.5.3, `net/http`, `net/http/httptest` for integration tests.

## Global Constraints

- Go module path: `github.com/pong-mobile/backend`
- All messages are JSON over WebSocket text frames
- Client→Server envelope: `{ "type": string, "requestId": string, "sentAt": int64, "payload": {} }`
- Server→Client envelope: `{ "type": string, "serverTime": int64, "serverTick": int64, "payload": {} }`
- Session tokens are crypto-random hex strings (32 hex chars = 16 bytes)
- Heartbeat interval: 10 seconds (`heartbeatIntervalMs: 10000` in `server.hello`)
- Package name for WebSocket layer is `wsconn` (not `websocket`) to avoid import-alias confusion with gorilla/websocket
- No rate limiting, no lobby, no match in this milestone
- Go binary path: `C:\Users\trent\sdk\go\bin\go.exe`; always set `$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"` before running Go commands

---

## File Map

| File | Purpose |
|---|---|
| `backend/go.mod` / `backend/go.sum` | Add gorilla/websocket dependency |
| `backend/internal/protocol/message.go` | Envelope types, message type constants, marshal/unmarshal helpers |
| `backend/internal/protocol/message_test.go` | Envelope parse/serialize unit tests |
| `backend/internal/auth/session.go` | Session type, token generation, concurrency-safe in-memory store |
| `backend/internal/auth/session_test.go` | Session store unit tests |
| `backend/internal/wsconn/conn.go` | Per-connection struct, read/write goroutines, write-loop ping ticker |
| `backend/internal/wsconn/handler.go` | HTTP upgrader, connection factory, message dispatcher |
| `backend/internal/wsconn/handler_test.go` | Integration tests: hello handshake, unknown type error, pong accepted |
| `backend/cmd/server/main.go` | HTTP server wired to wsconn.Handler, listens on :8080 |

---

### Task 1: gorilla/websocket dependency + protocol package

**Files:**
- Modify: `backend/go.mod` (via `go get`)
- Create (auto): `backend/go.sum`
- Create: `backend/internal/protocol/message.go`
- Create: `backend/internal/protocol/message_test.go`

**Interfaces:**
- Produces:
  - `protocol.TypeClientHello = "client.hello"`
  - `protocol.TypeServerHello = "server.hello"`
  - `protocol.TypeServerPing  = "server.ping"`
  - `protocol.TypeClientPong  = "client.pong"`
  - `protocol.TypeError       = "error"`
  - `protocol.ClientEnvelope{Type string; RequestID string; SentAt int64; Payload json.RawMessage}`
  - `protocol.ServerEnvelope{Type string; ServerTime int64; ServerTick int64; Payload any}`
  - `protocol.ErrorPayload{Code string; Message string; RequestID string; Recoverable bool}`
  - `protocol.ParseClient(data []byte) (ClientEnvelope, error)`
  - `protocol.MarshalServer(env ServerEnvelope) ([]byte, error)` — sets ServerTime to `time.Now().UnixMilli()` if zero
  - `protocol.MakeError(code, message, requestID string, recoverable bool, tick int64) ([]byte, error)`

- [ ] **Step 1: Add gorilla/websocket**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
Set-Location D:\Pong-Mobile\backend
go get github.com/gorilla/websocket@v1.5.3
```

Expected: `go.mod` updated with `require github.com/gorilla/websocket v1.5.3`, `go.sum` created, no errors.

- [ ] **Step 2: Write failing tests**

Create `D:\Pong-Mobile\backend\internal\protocol\message_test.go`:

```go
package protocol_test

import (
	"encoding/json"
	"testing"

	"github.com/pong-mobile/backend/internal/protocol"
)

func TestParseClient_ValidHello(t *testing.T) {
	raw := `{"type":"client.hello","requestId":"req_001","sentAt":1710000000000,"payload":{"clientVersion":"0.1.0","platform":"android","sessionToken":null,"displayName":"Guest1234"}}`
	env, err := protocol.ParseClient([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Type != protocol.TypeClientHello {
		t.Errorf("type: got %q, want %q", env.Type, protocol.TypeClientHello)
	}
	if env.RequestID != "req_001" {
		t.Errorf("requestId: got %q", env.RequestID)
	}
	if env.SentAt != 1710000000000 {
		t.Errorf("sentAt: got %d", env.SentAt)
	}
	if len(env.Payload) == 0 {
		t.Error("payload should not be empty")
	}
}

func TestParseClient_InvalidJSON(t *testing.T) {
	_, err := protocol.ParseClient([]byte("not json"))
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}

func TestParseClient_MissingType(t *testing.T) {
	raw := `{"requestId":"req_001","sentAt":1710000000000,"payload":{}}`
	env, err := protocol.ParseClient([]byte(raw))
	if err != nil {
		t.Fatalf("parse should succeed even with missing type: %v", err)
	}
	if env.Type != "" {
		t.Errorf("type should be empty string, got %q", env.Type)
	}
}

func TestMarshalServer_ProducesValidJSON(t *testing.T) {
	env := protocol.ServerEnvelope{
		Type:       protocol.TypeServerHello,
		ServerTime: 1710000000030,
		ServerTick: 0,
		Payload:    map[string]string{"sessionId": "sess_abc"},
	}
	data, err := protocol.MarshalServer(env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if out["type"] != protocol.TypeServerHello {
		t.Errorf("type field: got %v", out["type"])
	}
	if out["serverTime"] == nil {
		t.Error("serverTime field missing")
	}
}

func TestMarshalServer_SetsServerTimeWhenZero(t *testing.T) {
	env := protocol.ServerEnvelope{Type: protocol.TypeServerPing, Payload: map[string]string{}}
	data, _ := protocol.MarshalServer(env)
	var out map[string]any
	json.Unmarshal(data, &out)
	st, ok := out["serverTime"].(float64)
	if !ok || st == 0 {
		t.Errorf("serverTime should be auto-set when zero, got %v", out["serverTime"])
	}
}

func TestMakeError_ContainsCode(t *testing.T) {
	data, err := protocol.MakeError("room_not_found", "Room not found.", "req_001", true, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if out["type"] != protocol.TypeError {
		t.Errorf("type: got %v, want %q", out["type"], protocol.TypeError)
	}
	payload, ok := out["payload"].(map[string]any)
	if !ok {
		t.Fatal("payload not an object")
	}
	if payload["code"] != "room_not_found" {
		t.Errorf("code: got %v", payload["code"])
	}
	if payload["recoverable"] != true {
		t.Errorf("recoverable: got %v", payload["recoverable"])
	}
	if payload["requestId"] != "req_001" {
		t.Errorf("requestId: got %v", payload["requestId"])
	}
}
```

- [ ] **Step 3: Run tests — expect compile failure**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
Set-Location D:\Pong-Mobile\backend
go test ./internal/protocol/... 2>&1
```

Expected: `cannot find package` or `no Go files`. This is correct — no implementation yet.

- [ ] **Step 4: Write implementation**

Create `D:\Pong-Mobile\backend\internal\protocol\message.go`:

```go
package protocol

import (
	"encoding/json"
	"time"
)

const (
	TypeClientHello = "client.hello"
	TypeServerHello = "server.hello"
	TypeServerPing  = "server.ping"
	TypeClientPong  = "client.pong"
	TypeError       = "error"
)

// ClientEnvelope is the common wrapper for all client→server messages.
type ClientEnvelope struct {
	Type      string          `json:"type"`
	RequestID string          `json:"requestId"`
	SentAt    int64           `json:"sentAt"`
	Payload   json.RawMessage `json:"payload"`
}

// ServerEnvelope is the common wrapper for all server→client messages.
type ServerEnvelope struct {
	Type       string `json:"type"`
	ServerTime int64  `json:"serverTime"`
	ServerTick int64  `json:"serverTick,omitempty"`
	Payload    any    `json:"payload"`
}

// ErrorPayload is the payload body for "error" messages.
type ErrorPayload struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	RequestID   string `json:"requestId,omitempty"`
	Recoverable bool   `json:"recoverable"`
}

// ParseClient parses a raw WebSocket text frame into a ClientEnvelope.
func ParseClient(data []byte) (ClientEnvelope, error) {
	var env ClientEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return ClientEnvelope{}, err
	}
	return env, nil
}

// MarshalServer serializes a ServerEnvelope to JSON.
// If ServerTime is zero it is set to the current UTC time in milliseconds.
func MarshalServer(env ServerEnvelope) ([]byte, error) {
	if env.ServerTime == 0 {
		env.ServerTime = time.Now().UnixMilli()
	}
	return json.Marshal(env)
}

// MakeError builds a serialized "error" message ready to send to a client.
func MakeError(code, message, requestID string, recoverable bool, tick int64) ([]byte, error) {
	return MarshalServer(ServerEnvelope{
		Type:       TypeError,
		ServerTick: tick,
		Payload: ErrorPayload{
			Code:        code,
			Message:     message,
			RequestID:   requestID,
			Recoverable: recoverable,
		},
	})
}
```

- [ ] **Step 5: Run tests — expect all pass**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
Set-Location D:\Pong-Mobile\backend
go test ./internal/protocol/... -v 2>&1
```

Expected: 5 tests, all PASS.

- [ ] **Step 6: Commit**

```powershell
$msg = @'
feat: protocol package — message envelope, marshal, error helpers

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
'@
git -C D:\Pong-Mobile add backend/go.mod backend/go.sum backend/internal/protocol/
git -C D:\Pong-Mobile commit -m $msg
```

---

### Task 2: Auth — session token generation and store

**Files:**
- Create: `backend/internal/auth/session.go`
- Create: `backend/internal/auth/session_test.go`

**Interfaces:**
- Produces:
  - `auth.Session{ID string; PlayerID string; Token string; DisplayName string; CreatedAt time.Time}`
  - `auth.NewSession(displayName string) (Session, error)` — generates crypto-random IDs and token
  - `auth.Store` with:
    - `auth.NewStore() *Store`
    - `(*Store).Put(s Session)`
    - `(*Store).GetByToken(token string) (Session, bool)`
    - `(*Store).Delete(token string)`

- [ ] **Step 1: Write failing tests**

Create `D:\Pong-Mobile\backend\internal\auth\session_test.go`:

```go
package auth_test

import (
	"testing"

	"github.com/pong-mobile/backend/internal/auth"
)

func TestNewSession_GeneratesUniqueTokens(t *testing.T) {
	s1, err := auth.NewSession("Alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s2, err := auth.NewSession("Bob")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s1.Token == s2.Token {
		t.Error("tokens should be unique")
	}
	if s1.PlayerID == s2.PlayerID {
		t.Error("player IDs should be unique")
	}
}

func TestNewSession_Fields(t *testing.T) {
	s, err := auth.NewSession("Guest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Token == "" {
		t.Error("token must not be empty")
	}
	if s.PlayerID == "" {
		t.Error("playerID must not be empty")
	}
	if s.DisplayName != "Guest" {
		t.Errorf("displayName: got %q, want Guest", s.DisplayName)
	}
	if s.CreatedAt.IsZero() {
		t.Error("createdAt must be set")
	}
}

func TestStore_PutAndGetByToken(t *testing.T) {
	store := auth.NewStore()
	s, _ := auth.NewSession("Alice")
	store.Put(s)

	got, ok := store.GetByToken(s.Token)
	if !ok {
		t.Fatal("expected to find session by token")
	}
	if got.PlayerID != s.PlayerID {
		t.Errorf("playerID mismatch: got %q, want %q", got.PlayerID, s.PlayerID)
	}
}

func TestStore_GetByToken_Missing(t *testing.T) {
	store := auth.NewStore()
	_, ok := store.GetByToken("nonexistent-token")
	if ok {
		t.Error("should not find missing token")
	}
}

func TestStore_Delete(t *testing.T) {
	store := auth.NewStore()
	s, _ := auth.NewSession("Alice")
	store.Put(s)
	store.Delete(s.Token)
	_, ok := store.GetByToken(s.Token)
	if ok {
		t.Error("session should be gone after delete")
	}
}
```

- [ ] **Step 2: Run tests — expect compile failure**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
Set-Location D:\Pong-Mobile\backend
go test ./internal/auth/... 2>&1
```

Expected: package not found.

- [ ] **Step 3: Write implementation**

Create `D:\Pong-Mobile\backend\internal\auth\session.go`:

```go
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Session holds identity info for one connected client.
type Session struct {
	ID          string    // short hex ID (used as session identifier)
	PlayerID    string    // "player_<hex>" — sent to clients
	Token       string    // opaque 32-char hex token for reconnect
	DisplayName string
	CreatedAt   time.Time
}

// NewSession creates a Session with crypto-random token and player ID.
func NewSession(displayName string) (Session, error) {
	token, err := randomHex(16) // 32 hex chars
	if err != nil {
		return Session{}, err
	}
	idBytes, err := randomHex(8) // 16 hex chars
	if err != nil {
		return Session{}, err
	}
	return Session{
		ID:          idBytes,
		PlayerID:    "player_" + idBytes,
		Token:       token,
		DisplayName: displayName,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

// Store is a concurrency-safe in-memory session store keyed by token.
type Store struct {
	mu      sync.RWMutex
	byToken map[string]Session
}

// NewStore returns an initialized Store.
func NewStore() *Store {
	return &Store{byToken: make(map[string]Session)}
}

// Put adds or replaces a session.
func (s *Store) Put(session Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byToken[session.Token] = session
}

// GetByToken looks up a session by opaque token.
func (s *Store) GetByToken(token string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.byToken[token]
	return sess, ok
}

// Delete removes a session by token.
func (s *Store) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byToken, token)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
```

- [ ] **Step 4: Run tests — expect all pass**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
Set-Location D:\Pong-Mobile\backend
go test ./internal/auth/... -v 2>&1
```

Expected: 5 tests, all PASS.

- [ ] **Step 5: Commit**

```powershell
$msg = @'
feat: auth package — session token generation and in-memory store

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
'@
git -C D:\Pong-Mobile add backend/internal/auth/
git -C D:\Pong-Mobile commit -m $msg
```

---

### Task 3: WebSocket handler, hello handshake, heartbeat, main.go

**Files:**
- Create: `backend/internal/wsconn/conn.go`
- Create: `backend/internal/wsconn/handler.go`
- Create: `backend/internal/wsconn/handler_test.go`
- Modify: `backend/cmd/server/main.go`

**Interfaces:**
- Consumes:
  - `protocol.ParseClient`, `protocol.MarshalServer`, `protocol.MakeError`
  - `protocol.TypeClientHello`, `protocol.TypeClientPong`, `protocol.TypeServerHello`, `protocol.TypeServerPing`, `protocol.TypeError`
  - `protocol.ClientEnvelope`, `protocol.ServerEnvelope`
  - `auth.NewSession(displayName string) (Session, error)`
  - `auth.NewStore() *Store`, `(*Store).Put`, `(*Store).GetByToken`
- Produces:
  - `wsconn.Handler(sessions *auth.Store) http.Handler`

- [ ] **Step 1: Write failing integration tests**

Create `D:\Pong-Mobile\backend\internal\wsconn\handler_test.go`:

```go
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
```

- [ ] **Step 2: Run tests — expect compile failure**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
Set-Location D:\Pong-Mobile\backend
go test ./internal/wsconn/... 2>&1
```

Expected: `cannot find package`.

- [ ] **Step 3: Create conn.go**

Create `D:\Pong-Mobile\backend\internal\wsconn\conn.go`:

```go
package wsconn

import (
	"fmt"
	"log"
	"time"

	gorillaws "github.com/gorilla/websocket"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/protocol"
)

const (
	writeWait    = 10 * time.Second
	pingInterval = 10 * time.Second
	pongWait     = 15 * time.Second
	maxMsgSize   = 4096
)

// conn manages a single WebSocket client connection.
type conn struct {
	ws      *gorillaws.Conn
	session *auth.Session
	send    chan []byte // buffered outbound message queue
}

func newConn(ws *gorillaws.Conn) *conn {
	return &conn{
		ws:   ws,
		send: make(chan []byte, 64),
	}
}

// sendBytes queues a raw message for the write loop. Drops silently if full.
func (c *conn) sendBytes(data []byte) {
	select {
	case c.send <- data:
	default:
		log.Println("wsconn: send buffer full, dropping message")
	}
}

// writeLoop drains the send channel and sends JSON ping frames on a ticker.
// Closes the WebSocket when the send channel is closed or a write fails.
func (c *conn) writeLoop() {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		c.ws.Close()
	}()

	pingSeq := 0
	for {
		select {
		case msg, ok := <-c.send:
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.ws.WriteMessage(gorillaws.CloseMessage, []byte{})
				return
			}
			if err := c.ws.WriteMessage(gorillaws.TextMessage, msg); err != nil {
				log.Printf("wsconn: write error: %v", err)
				return
			}

		case <-ticker.C:
			pingSeq++
			data, err := protocol.MarshalServer(protocol.ServerEnvelope{
				Type:    protocol.TypeServerPing,
				Payload: map[string]string{"pingId": fmt.Sprintf("ping_%d", pingSeq)},
			})
			if err != nil {
				continue
			}
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(gorillaws.TextMessage, data); err != nil {
				log.Printf("wsconn: ping write error: %v", err)
				return
			}
		}
	}
}

// readLoop reads messages and dispatches them until the connection closes.
// Closing the send channel signals the writeLoop to shut down.
func (c *conn) readLoop(dispatch func(*conn, protocol.ClientEnvelope)) {
	defer func() {
		close(c.send)
		c.ws.Close()
	}()

	c.ws.SetReadLimit(maxMsgSize)
	c.ws.SetReadDeadline(time.Now().Add(pongWait))

	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			if gorillaws.IsUnexpectedCloseError(err,
				gorillaws.CloseGoingAway,
				gorillaws.CloseNormalClosure,
			) {
				log.Printf("wsconn: unexpected close: %v", err)
			}
			return
		}
		// Extend deadline on any received frame (heartbeat or input).
		c.ws.SetReadDeadline(time.Now().Add(pongWait))

		env, err := protocol.ParseClient(data)
		if err != nil {
			errMsg, _ := protocol.MakeError("bad_message", "Message could not be parsed.", "", false, 0)
			c.sendBytes(errMsg)
			continue
		}
		dispatch(c, env)
	}
}
```

- [ ] **Step 4: Create handler.go**

Create `D:\Pong-Mobile\backend\internal\wsconn\handler.go`:

```go
package wsconn

import (
	"encoding/json"
	"log"
	"net/http"

	gorillaws "github.com/gorilla/websocket"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/protocol"
)

var upgrader = gorillaws.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow all origins during development.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handler returns an http.Handler that upgrades HTTP connections to WebSocket
// and runs the hello handshake and message dispatch loop.
func Handler(sessions *auth.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("wsconn: upgrade error: %v", err)
			return
		}
		c := newConn(ws)
		go c.writeLoop()
		c.readLoop(makeDispatcher(sessions))
	})
}

// helloPayload matches client.hello payload from NETWORK_PROTOCOL.md §7.
type helloPayload struct {
	ClientVersion string  `json:"clientVersion"`
	Platform      string  `json:"platform"`
	SessionToken  *string `json:"sessionToken"`
	DisplayName   string  `json:"displayName"`
}

// serverHelloPayload matches server.hello payload from NETWORK_PROTOCOL.md §7.
type serverHelloPayload struct {
	SessionID           string `json:"sessionId"`
	PlayerID            string `json:"playerId"`
	SessionToken        string `json:"sessionToken"`
	HeartbeatIntervalMs int    `json:"heartbeatIntervalMs"`
}

func makeDispatcher(sessions *auth.Store) func(*conn, protocol.ClientEnvelope) {
	return func(c *conn, env protocol.ClientEnvelope) {
		switch env.Type {
		case protocol.TypeClientHello:
			handleHello(c, env, sessions)

		case protocol.TypeClientPong:
			// JSON-level pong is a no-op; read deadline already extended in readLoop.

		default:
			errMsg, _ := protocol.MakeError("unknown_type", "Message type is not supported.", env.RequestID, false, 0)
			c.sendBytes(errMsg)
			log.Printf("wsconn: unknown message type %q", env.Type)
		}
	}
}

func handleHello(c *conn, env protocol.ClientEnvelope, sessions *auth.Store) {
	var p helloPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		errMsg, _ := protocol.MakeError("bad_message", "Invalid hello payload.", env.RequestID, false, 0)
		c.sendBytes(errMsg)
		return
	}

	displayName := p.DisplayName
	if displayName == "" {
		displayName = "Guest"
	}

	sess, err := auth.NewSession(displayName)
	if err != nil {
		errMsg, _ := protocol.MakeError("server_error", "Could not create session.", env.RequestID, false, 0)
		c.sendBytes(errMsg)
		return
	}
	sessions.Put(sess)
	c.session = &sess

	data, err := protocol.MarshalServer(protocol.ServerEnvelope{
		Type: protocol.TypeServerHello,
		Payload: serverHelloPayload{
			SessionID:           sess.ID,
			PlayerID:            sess.PlayerID,
			SessionToken:        sess.Token,
			HeartbeatIntervalMs: int(pingInterval.Milliseconds()),
		},
	})
	if err != nil {
		log.Printf("wsconn: marshal server.hello: %v", err)
		return
	}
	c.sendBytes(data)
	log.Printf("wsconn: session created playerID=%s displayName=%q", sess.PlayerID, sess.DisplayName)
}
```

- [ ] **Step 5: Run tests — expect all pass**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
Set-Location D:\Pong-Mobile\backend
go test ./internal/wsconn/... -v 2>&1
```

Expected: 5 tests, all PASS.

- [ ] **Step 6: Update main.go**

Replace `D:\Pong-Mobile\backend\cmd\server\main.go`:

```go
package main

import (
	"log"
	"net/http"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/wsconn"
)

func main() {
	sessions := auth.NewStore()

	mux := http.NewServeMux()
	mux.Handle("/ws", wsconn.Handler(sessions))

	addr := ":8080"
	log.Printf("volley server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
```

- [ ] **Step 7: Build and verify**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
Set-Location D:\Pong-Mobile\backend
go build ./... 2>&1
go test ./... 2>&1
```

Expected: build clean, all tests pass across all packages.

- [ ] **Step 8: Commit**

```powershell
$msg = @'
feat(milestone-2): WebSocket skeleton — hello handshake, heartbeat, error dispatch

- wsconn: HTTP upgrader, read/write goroutines, JSON ping ticker
- wsconn: client.hello → server.hello with session token
- wsconn: unknown type → error response; client.pong is no-op
- cmd/server: HTTP server on :8080 /ws endpoint

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
'@
git -C D:\Pong-Mobile add backend/internal/wsconn/ backend/cmd/server/main.go
git -C D:\Pong-Mobile commit -m $msg
```

---

## Self-Review

**Spec coverage (NETWORK_PROTOCOL.md §21 first implementation target):**

| Requirement | Covered by |
|---|---|
| `client.hello` parse | Task 3, handler.go `handleHello` |
| `server.hello` response | Task 3, handler.go `handleHello` |
| `server.ping` heartbeat | Task 3, conn.go `writeLoop` ticker |
| `client.pong` accepted | Task 3, handler.go dispatcher |
| `error` envelope | Task 1, `protocol.MakeError` |
| Session token in server.hello | Task 2 + Task 3 |
| `heartbeatIntervalMs` in server.hello | Task 3, `serverHelloPayload` |

**Placeholder scan:** No TBDs or TODOs found.

**Type consistency:**
- `protocol.ClientEnvelope.RequestID` (string) — used consistently in `MakeError` calls
- `auth.Session.PlayerID` — used as `playerId` in `serverHelloPayload` ✓
- `wsconn.Handler` signature matches test usage `wsconn.Handler(store)` ✓
- `pingInterval` defined in conn.go, referenced in handler.go — both in same package ✓
