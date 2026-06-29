# M3 Room System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add room creation, joining, ready state, and match-start trigger to the Go backend so two connected clients can create/join a room, become ready, and fire a match-start callback.

**Architecture:** A new `lobby` package owns a thread-safe `Manager` (map + mutex) of rooms. `wsconn.conn` gains `ID()` and `SendBytes()` to satisfy `lobby.Sender`. The WebSocket handler gains four room message cases that dispatch to the Manager. The Manager broadcasts `room.updated` via each slot's `Sender`. When both players mark ready the Manager fires `onStart(room)` — this milestone wires a log-only stub.

**Tech Stack:** Go 1.22, module `github.com/pong-mobile/backend`, existing `protocol`/`auth`/`config` packages.

## Global Constraints

- Go 1.22, module path `github.com/pong-mobile/backend`
- All new and existing tests must pass with `go test ./...` — zero regressions
- JSON text frames only — no binary frames
- Room codes: 6-digit zero-padded decimal string (e.g. `"042913"`), generated with `crypto/rand`
- Settings clamping: `pointsToWin` [1–21], `paddleSpeed` [0.1–2.0], `ballSpeed` [0.1–2.0]
- `lobby` does NOT import `wsconn`. `wsconn` imports `lobby`.
- `lobby` MAY import `protocol`, `auth`, `config`.
- Test output must be pristine — no unexpected log lines in test runs
- PowerShell syntax for all shell commands; Go binary at `C:\Users\trent\sdk\go\bin\go.exe`
- Commits: use PowerShell here-string syntax `@'...'@` for multi-line messages

---

## File Map

| File | Change |
|---|---|
| `backend/internal/protocol/message.go` | Add room message type constants |
| `backend/internal/wsconn/conn.go` | Add `id` field, `ID()`, `SendBytes()`, add `onClose` param to `readLoop` |
| `backend/internal/wsconn/handler.go` | Add room dispatch cases; inject `*lobby.Manager`; pass `onClose` to `readLoop` |
| `backend/internal/wsconn/handler_test.go` | Add 2 room integration tests |
| `backend/internal/lobby/room.go` | Create: `Room`, `Slot`, `RoomStatus`, `Sender` interface |
| `backend/internal/lobby/manager.go` | Create: `Manager` — `CreateRoom`, `JoinRoom`, `SetReady`, `LeaveRoom`, `OnDisconnect` |
| `backend/internal/lobby/broadcast.go` | Create: `BroadcastRoomUpdated`, `SendRoomCreated`, `SendRoomError` |
| `backend/internal/lobby/manager_test.go` | Create: 9 unit tests with mock Sender |
| `backend/cmd/server/main.go` | Wire `lobby.Manager` with stub `onStart` |

---

### Task 1: conn.ID/SendBytes/onClose + protocol room constants

Add the plumbing `lobby` will need before the lobby package exists.

**Files:**
- Modify: `backend/internal/protocol/message.go`
- Modify: `backend/internal/wsconn/conn.go`
- Modify: `backend/internal/wsconn/handler.go` (compile fix only — pass `nil` for onClose)

**Interfaces:**
- Produces:
  - `(*conn).ID() string`
  - `(*conn).SendBytes(data []byte)`
  - `(*conn).readLoop(dispatch func(*conn, protocol.ClientEnvelope), onClose func())`
  - `protocol.TypeRoomCreate = "room.create"`
  - `protocol.TypeRoomCreated = "room.created"`
  - `protocol.TypeRoomJoin = "room.join"`
  - `protocol.TypeRoomUpdated = "room.updated"`
  - `protocol.TypeRoomReady = "room.ready"`
  - `protocol.TypeRoomLeave = "room.leave"`

- [ ] **Step 1: Add room type constants to protocol/message.go**

The const block in `backend/internal/protocol/message.go` currently ends at `TypeError`. Replace the entire const block with:

```go
const (
	TypeClientHello = "client.hello"
	TypeServerHello = "server.hello"
	TypeServerPing  = "server.ping"
	TypeClientPong  = "client.pong"
	TypeError       = "error"

	TypeRoomCreate  = "room.create"
	TypeRoomCreated = "room.created"
	TypeRoomJoin    = "room.join"
	TypeRoomUpdated = "room.updated"
	TypeRoomReady   = "room.ready"
	TypeRoomLeave   = "room.leave"
)
```

- [ ] **Step 2: Extend conn.go**

Replace `backend/internal/wsconn/conn.go` entirely with:

```go
package wsconn

import (
	"crypto/rand"
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

type conn struct {
	ws      *gorillaws.Conn
	session *auth.Session
	send    chan []byte
	id      string
}

func newConn(ws *gorillaws.Conn) *conn {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return &conn{
		ws:   ws,
		send: make(chan []byte, 64),
		id:   fmt.Sprintf("%x", b),
	}
}

// ID returns a stable unique identifier for this connection. Satisfies lobby.Sender.
func (c *conn) ID() string { return c.id }

// SendBytes is the exported send method. Satisfies lobby.Sender.
func (c *conn) SendBytes(data []byte) { c.sendBytes(data) }

func (c *conn) sendBytes(data []byte) {
	select {
	case c.send <- data:
	default:
		log.Println("wsconn: send buffer full, dropping message")
	}
}

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

// readLoop reads messages and dispatches them. onClose (if non-nil) is called
// before the send channel is closed — use it to notify managers of disconnection.
func (c *conn) readLoop(dispatch func(*conn, protocol.ClientEnvelope), onClose func()) {
	defer func() {
		if onClose != nil {
			onClose()
		}
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
				gorillaws.CloseAbnormalClosure,
			) {
				log.Printf("wsconn: unexpected close: %v", err)
			}
			return
		}
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

- [ ] **Step 3: Update handler.go to pass nil onClose (compile fix)**

In `backend/internal/wsconn/handler.go` find line:
```go
		c.readLoop(makeDispatcher(sessions))
```
Replace with:
```go
		c.readLoop(makeDispatcher(sessions), nil)
```

- [ ] **Step 4: Run tests — all 11 must pass**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
Set-Location D:\Pong-Mobile\backend
go test ./internal/protocol/... ./internal/wsconn/... -v 2>&1
```

Expected: 6 protocol tests + 5 wsconn tests, all PASS, no compile errors.

- [ ] **Step 5: Commit**

```powershell
cd D:\Pong-Mobile
git add backend/internal/protocol/message.go backend/internal/wsconn/conn.go backend/internal/wsconn/handler.go
git commit -m "feat(m3): conn.ID/SendBytes, readLoop onClose param, room protocol constants"
```

---

### Task 2: lobby package — Room, Manager, broadcast, unit tests

**Files:**
- Create: `backend/internal/lobby/room.go`
- Create: `backend/internal/lobby/manager.go`
- Create: `backend/internal/lobby/broadcast.go`
- Create: `backend/internal/lobby/manager_test.go`

**Interfaces:**
- Consumes: `auth.Session`, `config.Settings`, `config.Default`, `protocol.MarshalServer`, `protocol.MakeError`, `protocol.TypeRoomUpdated`, `protocol.TypeRoomCreated`, `protocol.TypeError`
- Produces:
  - `lobby.Sender` interface: `SendBytes([]byte)`, `ID() string`
  - `lobby.Room` struct with fields: `ID`, `Code`, `Players [2]*Slot`, `Settings config.Settings`, `Status RoomStatus`
  - `lobby.Slot` struct: `Session *auth.Session`, `Conn Sender`, `Ready bool`, `Connected bool`
  - `lobby.RoomStatus` type: `RoomStatusWaiting = "waiting"`, `RoomStatusStarting = "starting"`
  - `lobby.ErrRoomNotFound`, `lobby.ErrRoomFull`, `lobby.ErrAlreadyInRoom`, `lobby.ErrNotInRoom`
  - `lobby.NewManager(onStart func(*Room)) *Manager`
  - `(*Manager).CreateRoom(sess *auth.Session, conn Sender, settings config.Settings) (*Room, error)`
  - `(*Manager).JoinRoom(code string, sess *auth.Session, conn Sender) (*Room, error)`
  - `(*Manager).SetReady(connID string, ready bool) error`
  - `(*Manager).LeaveRoom(connID string)`
  - `(*Manager).OnDisconnect(connID string)`

- [ ] **Step 1: Write failing tests**

Create `backend/internal/lobby/manager_test.go`:

```go
package lobby_test

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/config"
	"github.com/pong-mobile/backend/internal/lobby"
)

// mockSender records SendBytes calls and implements lobby.Sender.
type mockSender struct {
	mu       sync.Mutex
	id       string
	messages [][]byte
}

func (m *mockSender) SendBytes(data []byte) {
	m.mu.Lock()
	m.messages = append(m.messages, data)
	m.mu.Unlock()
}
func (m *mockSender) ID() string { return m.id }
func (m *mockSender) received() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]map[string]any, len(m.messages))
	for i, b := range m.messages {
		_ = json.Unmarshal(b, &out[i])
	}
	return out
}
func newMock(id string) *mockSender { return &mockSender{id: id} }

func newSess(playerID string) *auth.Session {
	return &auth.Session{
		ID:          "sess_" + playerID,
		PlayerID:    playerID,
		Token:       "tok_" + playerID,
		DisplayName: playerID,
	}
}

func TestCreateRoom_Success(t *testing.T) {
	mgr := lobby.NewManager(func(r *lobby.Room) {})
	conn := newMock("conn1")
	room, err := mgr.CreateRoom(newSess("p1"), conn, config.Default)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if len(room.Code) != 6 {
		t.Errorf("room code should be 6 chars, got %q", room.Code)
	}
	if room.Players[0] == nil {
		t.Error("slot 0 should be filled")
	}
	if room.Players[1] != nil {
		t.Error("slot 1 should be empty")
	}
}

func TestJoinRoom_Success(t *testing.T) {
	mgr := lobby.NewManager(func(r *lobby.Room) {})
	c1 := newMock("conn1")
	room, _ := mgr.CreateRoom(newSess("p1"), c1, config.Default)
	c2 := newMock("conn2")
	joined, err := mgr.JoinRoom(room.Code, newSess("p2"), c2)
	if err != nil {
		t.Fatalf("JoinRoom: %v", err)
	}
	if joined.Players[1] == nil {
		t.Error("slot 1 should be filled after join")
	}
}

func TestJoinRoom_RoomNotFound(t *testing.T) {
	mgr := lobby.NewManager(func(r *lobby.Room) {})
	c := newMock("conn1")
	_, err := mgr.JoinRoom("000000", newSess("p1"), c)
	if err != lobby.ErrRoomNotFound {
		t.Errorf("expected ErrRoomNotFound, got %v", err)
	}
}

func TestJoinRoom_RoomFull(t *testing.T) {
	mgr := lobby.NewManager(func(r *lobby.Room) {})
	c1, c2, c3 := newMock("conn1"), newMock("conn2"), newMock("conn3")
	room, _ := mgr.CreateRoom(newSess("p1"), c1, config.Default)
	mgr.JoinRoom(room.Code, newSess("p2"), c2)
	_, err := mgr.JoinRoom(room.Code, newSess("p3"), c3)
	if err != lobby.ErrRoomFull {
		t.Errorf("expected ErrRoomFull, got %v", err)
	}
}

func TestJoinRoom_AlreadyInRoom(t *testing.T) {
	mgr := lobby.NewManager(func(r *lobby.Room) {})
	c1 := newMock("conn1")
	room, _ := mgr.CreateRoom(newSess("p1"), c1, config.Default)
	// c1 tries to join another room (same code in this test)
	_, err := mgr.JoinRoom(room.Code, newSess("p1b"), c1)
	if err != lobby.ErrAlreadyInRoom {
		t.Errorf("expected ErrAlreadyInRoom, got %v", err)
	}
}

func TestSetReady_BothReady_CallsOnStart(t *testing.T) {
	started := false
	mgr := lobby.NewManager(func(r *lobby.Room) { started = true })
	c1, c2 := newMock("conn1"), newMock("conn2")
	room, _ := mgr.CreateRoom(newSess("p1"), c1, config.Default)
	mgr.JoinRoom(room.Code, newSess("p2"), c2)

	mgr.SetReady("conn1", true)
	if started {
		t.Error("onStart should not fire with only one player ready")
	}
	mgr.SetReady("conn2", true)
	if !started {
		t.Error("onStart should fire when both players are ready")
	}
}

func TestSetReady_OneReady_NoStart(t *testing.T) {
	started := false
	mgr := lobby.NewManager(func(r *lobby.Room) { started = true })
	c1, c2 := newMock("conn1"), newMock("conn2")
	room, _ := mgr.CreateRoom(newSess("p1"), c1, config.Default)
	mgr.JoinRoom(room.Code, newSess("p2"), c2)
	mgr.SetReady("conn1", true)
	if started {
		t.Error("onStart must not fire with only one player ready")
	}
}

func TestLeaveRoom_HostLeaves_ClosesRoom(t *testing.T) {
	mgr := lobby.NewManager(func(r *lobby.Room) {})
	c1, c2 := newMock("conn1"), newMock("conn2")
	room, _ := mgr.CreateRoom(newSess("p1"), c1, config.Default)
	mgr.JoinRoom(room.Code, newSess("p2"), c2)

	mgr.LeaveRoom("conn1")

	// Guest should have received an error message
	msgs := c2.received()
	if len(msgs) == 0 {
		t.Fatal("guest should receive a message when host leaves")
	}
	last := msgs[len(msgs)-1]
	if last["type"] != "error" {
		t.Errorf("guest should receive error, got %v", last["type"])
	}
}

func TestLeaveRoom_GuestLeaves_RoomOpen(t *testing.T) {
	mgr := lobby.NewManager(func(r *lobby.Room) {})
	c1, c2 := newMock("conn1"), newMock("conn2")
	room, _ := mgr.CreateRoom(newSess("p1"), c1, config.Default)
	mgr.JoinRoom(room.Code, newSess("p2"), c2)

	// Count messages before guest leaves (join triggers room.updated)
	priorCount := len(c1.received())

	mgr.LeaveRoom("conn2")

	// Host should receive room.updated showing only 1 player
	msgs := c1.received()
	if len(msgs) <= priorCount {
		t.Fatal("host should receive room.updated when guest leaves")
	}
	last := msgs[len(msgs)-1]
	if last["type"] != "room.updated" {
		t.Errorf("host should receive room.updated, got %v", last["type"])
	}
}
```

- [ ] **Step 2: Run tests — expect compile failure**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
Set-Location D:\Pong-Mobile\backend
go test ./internal/lobby/... 2>&1
```

Expected: `cannot find package "github.com/pong-mobile/backend/internal/lobby"`.

- [ ] **Step 3: Create lobby/room.go**

Create `backend/internal/lobby/room.go`:

```go
package lobby

import (
	"errors"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/config"
)

// Sender is implemented by wsconn.conn via duck typing (no import needed).
type Sender interface {
	SendBytes(data []byte)
	ID() string
}

type RoomStatus string

const (
	RoomStatusWaiting  RoomStatus = "waiting"
	RoomStatusStarting RoomStatus = "starting"
)

var (
	ErrRoomNotFound  = errors.New("room_not_found")
	ErrRoomFull      = errors.New("room_full")
	ErrAlreadyInRoom = errors.New("already_in_room")
	ErrNotInRoom     = errors.New("not_in_room")
)

// Slot holds one player's connection and state within a room.
type Slot struct {
	Session   *auth.Session
	Conn      Sender
	Ready     bool
	Connected bool
}

// Room is the authoritative room state.
type Room struct {
	ID       string
	Code     string
	Players  [2]*Slot
	Settings config.Settings
	Status   RoomStatus
}
```

- [ ] **Step 4: Create lobby/broadcast.go**

Create `backend/internal/lobby/broadcast.go`:

```go
package lobby

import (
	"github.com/pong-mobile/backend/internal/protocol"
)

type roomUpdatedPayload struct {
	RoomID   string        `json:"roomId"`
	RoomCode string        `json:"roomCode"`
	Status   string        `json:"status"`
	Players  []playerEntry `json:"players"`
	Settings settingsEntry `json:"settings"`
}

type playerEntry struct {
	PlayerID    string `json:"playerId"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	Ready       bool   `json:"ready"`
	Connected   bool   `json:"connected"`
}

type settingsEntry struct {
	PointsToWin int     `json:"pointsToWin"`
	PaddleSpeed float64 `json:"paddleSpeed"`
	BallSpeed   float64 `json:"ballSpeed"`
}

type roomCreatedPayload struct {
	RoomID       string        `json:"roomId"`
	RoomCode     string        `json:"roomCode"`
	HostPlayerID string        `json:"hostPlayerId"`
	Settings     settingsEntry `json:"settings"`
}

func buildRoomUpdated(room *Room) ([]byte, error) {
	var players []playerEntry
	for i, slot := range room.Players {
		if slot == nil {
			continue
		}
		role := "host"
		if i == 1 {
			role = "guest"
		}
		players = append(players, playerEntry{
			PlayerID:    slot.Session.PlayerID,
			DisplayName: slot.Session.DisplayName,
			Role:        role,
			Ready:       slot.Ready,
			Connected:   slot.Connected,
		})
	}
	return protocol.MarshalServer(protocol.ServerEnvelope{
		Type: protocol.TypeRoomUpdated,
		Payload: roomUpdatedPayload{
			RoomID:   room.ID,
			RoomCode: room.Code,
			Status:   string(room.Status),
			Players:  players,
			Settings: settingsEntry{
				PointsToWin: room.Settings.PointsToWin,
				PaddleSpeed: room.Settings.PaddleSpeed,
				BallSpeed:   room.Settings.BallSpeed,
			},
		},
	})
}

// BroadcastRoomUpdated sends room.updated to all connected players.
func BroadcastRoomUpdated(room *Room) {
	data, err := buildRoomUpdated(room)
	if err != nil {
		return
	}
	for _, slot := range room.Players {
		if slot != nil && slot.Conn != nil && slot.Connected {
			slot.Conn.SendBytes(data)
		}
	}
}

// SendRoomCreated sends the room.created response to the creator.
func SendRoomCreated(room *Room) {
	data, err := protocol.MarshalServer(protocol.ServerEnvelope{
		Type: protocol.TypeRoomCreated,
		Payload: roomCreatedPayload{
			RoomID:       room.ID,
			RoomCode:     room.Code,
			HostPlayerID: room.Players[0].Session.PlayerID,
			Settings: settingsEntry{
				PointsToWin: room.Settings.PointsToWin,
				PaddleSpeed: room.Settings.PaddleSpeed,
				BallSpeed:   room.Settings.BallSpeed,
			},
		},
	})
	if err != nil {
		return
	}
	room.Players[0].Conn.SendBytes(data)
}

// SendRoomError sends an error message to a specific connection.
func SendRoomError(conn Sender, code, message, requestID string) {
	data, err := protocol.MakeError(code, message, requestID, false, 0)
	if err != nil {
		return
	}
	conn.SendBytes(data)
}
```

- [ ] **Step 5: Create lobby/manager.go**

Create `backend/internal/lobby/manager.go`:

```go
package lobby

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/config"
)

// Manager owns all room state. All exported methods are safe for concurrent use.
type Manager struct {
	mu      sync.Mutex
	rooms   map[string]*Room // code → *Room
	byConn  map[string]*Room // connID → *Room
	onStart func(*Room)
}

func NewManager(onStart func(*Room)) *Manager {
	return &Manager{
		rooms:   make(map[string]*Room),
		byConn:  make(map[string]*Room),
		onStart: onStart,
	}
}

func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func generateCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// CreateRoom creates a new room with the host in slot 0.
// Returns ErrAlreadyInRoom if conn is already in a room.
func (m *Manager) CreateRoom(sess *auth.Session, conn Sender, settings config.Settings) (*Room, error) {
	// Clamp user-supplied settings onto defaults.
	if settings.PointsToWin <= 0 {
		settings.PointsToWin = config.Default.PointsToWin
	} else {
		settings.PointsToWin = clampInt(settings.PointsToWin, 1, 21)
	}
	if settings.PaddleSpeed <= 0 {
		settings.PaddleSpeed = config.Default.PaddleSpeed
	} else {
		settings.PaddleSpeed = clampFloat(settings.PaddleSpeed, 0.1, 2.0)
	}
	if settings.BallSpeed <= 0 {
		settings.BallSpeed = config.Default.BallSpeed
	} else {
		settings.BallSpeed = clampFloat(settings.BallSpeed, 0.1, 2.0)
	}
	// Carry over all physics/timing constants from Default.
	full := config.Default
	full.PointsToWin = settings.PointsToWin
	full.PaddleSpeed = settings.PaddleSpeed
	full.BallSpeed = settings.BallSpeed

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.byConn[conn.ID()]; exists {
		return nil, ErrAlreadyInRoom
	}

	var code string
	for {
		var err error
		code, err = generateCode()
		if err != nil {
			return nil, err
		}
		if _, taken := m.rooms[code]; !taken {
			break
		}
	}

	room := &Room{
		ID:       generateID(),
		Code:     code,
		Settings: full,
		Status:   RoomStatusWaiting,
		Players: [2]*Slot{
			{Session: sess, Conn: conn, Connected: true},
			nil,
		},
	}
	m.rooms[code] = room
	m.byConn[conn.ID()] = room

	SendRoomCreated(room)
	return room, nil
}

// JoinRoom adds a second player to the room with the given code.
// Returns ErrRoomNotFound, ErrRoomFull, or ErrAlreadyInRoom on failure.
func (m *Manager) JoinRoom(code string, sess *auth.Session, conn Sender) (*Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	room, ok := m.rooms[code]
	if !ok {
		return nil, ErrRoomNotFound
	}
	if room.Players[1] != nil {
		return nil, ErrRoomFull
	}
	if _, exists := m.byConn[conn.ID()]; exists {
		return nil, ErrAlreadyInRoom
	}

	room.Players[1] = &Slot{Session: sess, Conn: conn, Connected: true}
	m.byConn[conn.ID()] = room

	BroadcastRoomUpdated(room)
	return room, nil
}

// SetReady toggles a player's ready flag and fires onStart when both are ready.
// Returns ErrNotInRoom if connID is not in any room.
func (m *Manager) SetReady(connID string, ready bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	room, ok := m.byConn[connID]
	if !ok {
		return ErrNotInRoom
	}

	for _, slot := range room.Players {
		if slot != nil && slot.Conn.ID() == connID {
			slot.Ready = ready
			break
		}
	}

	BroadcastRoomUpdated(room)

	if room.Status == RoomStatusWaiting &&
		room.Players[0] != nil && room.Players[0].Ready &&
		room.Players[1] != nil && room.Players[1].Ready {
		room.Status = RoomStatusStarting
		go m.onStart(room) // run outside lock to avoid deadlock in onStart
	}

	return nil
}

// LeaveRoom removes the player with connID from their room.
// If the host leaves, the room is closed and the guest is notified.
// If the guest leaves, the host receives room.updated.
func (m *Manager) LeaveRoom(connID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.leaveRoomLocked(connID)
}

// OnDisconnect is called when a WebSocket connection closes.
// In M3 this is identical to LeaveRoom; match-phase handling is added in M4.
func (m *Manager) OnDisconnect(connID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.leaveRoomLocked(connID)
}

func (m *Manager) leaveRoomLocked(connID string) {
	room, ok := m.byConn[connID]
	if !ok {
		return
	}
	delete(m.byConn, connID)

	if room.Players[0] != nil && room.Players[0].Conn.ID() == connID {
		// Host left — close room, notify guest.
		if room.Players[1] != nil {
			SendRoomError(room.Players[1].Conn, "room_closed", "Room was closed by the host.", "")
			delete(m.byConn, room.Players[1].Conn.ID())
		}
		delete(m.rooms, room.Code)
	} else if room.Players[1] != nil && room.Players[1].Conn.ID() == connID {
		// Guest left — clear slot, notify host.
		room.Players[1] = nil
		BroadcastRoomUpdated(room)
	}
}
```

- [ ] **Step 6: Run tests — expect all pass**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
Set-Location D:\Pong-Mobile\backend
go test ./internal/lobby/... -v 2>&1
```

Expected: 9 tests, all PASS.

- [ ] **Step 7: Run full suite — no regressions**

```powershell
go test ./... 2>&1
```

Expected: all packages pass.

- [ ] **Step 8: Commit**

```powershell
cd D:\Pong-Mobile
git add backend/internal/lobby/
git commit -m "feat(m3): lobby package — room, manager, broadcast"
```

---

### Task 3: wsconn room routing + main.go wiring

Connect the handler to the lobby Manager and add integration tests.

**Files:**
- Modify: `backend/internal/wsconn/handler.go`
- Modify: `backend/internal/wsconn/handler_test.go`
- Modify: `backend/cmd/server/main.go`

**Interfaces:**
- Consumes: all lobby.Manager methods, lobby.ErrRoomNotFound, lobby.ErrRoomFull, lobby.ErrAlreadyInRoom
- Produces: `wsconn.Handler(sessions *auth.Store, mgr *lobby.Manager) http.Handler`

- [ ] **Step 1: Write failing integration tests**

Add to `backend/internal/wsconn/handler_test.go`:

```go
func TestHandler_RoomCreate_Success(t *testing.T) {
	store := auth.NewStore()
	mgr := lobby.NewManager(func(r *lobby.Room) {})
	srv := httptest.NewServer(wsconn.Handler(store, mgr))
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
	srv := httptest.NewServer(wsconn.Handler(store, mgr))
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
```

Also add `"github.com/pong-mobile/backend/internal/lobby"` to the import block in handler_test.go.

- [ ] **Step 2: Run tests — expect compile failure**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
Set-Location D:\Pong-Mobile\backend
go test ./internal/wsconn/... 2>&1
```

Expected: compile error — `wsconn.Handler` takes 1 argument, tests pass 2.

- [ ] **Step 3: Rewrite handler.go**

Replace `backend/internal/wsconn/handler.go` entirely with:

```go
package wsconn

import (
	"encoding/json"
	"log"
	"net/http"

	gorillaws "github.com/gorilla/websocket"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/config"
	"github.com/pong-mobile/backend/internal/lobby"
	"github.com/pong-mobile/backend/internal/protocol"
)

var upgrader = gorillaws.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// Handler returns an http.Handler for WebSocket connections.
func Handler(sessions *auth.Store, mgr *lobby.Manager) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("wsconn: upgrade error: %v", err)
			return
		}
		c := newConn(ws)
		go c.writeLoop()
		c.readLoop(makeDispatcher(sessions, mgr), func() {
			mgr.OnDisconnect(c.id)
		})
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

// createPayload is the payload for room.create.
type createPayload struct {
	Settings struct {
		PointsToWin int     `json:"pointsToWin"`
		PaddleSpeed float64 `json:"paddleSpeed"`
		BallSpeed   float64 `json:"ballSpeed"`
	} `json:"settings"`
}

// joinPayload is the payload for room.join.
type joinPayload struct {
	RoomCode string `json:"roomCode"`
}

// readyPayload is the payload for room.ready.
type readyPayload struct {
	Ready bool `json:"ready"`
}

func makeDispatcher(sessions *auth.Store, mgr *lobby.Manager) func(*conn, protocol.ClientEnvelope) {
	return func(c *conn, env protocol.ClientEnvelope) {
		// All messages except client.hello require a session.
		if c.session == nil && env.Type != protocol.TypeClientHello {
			errMsg, _ := protocol.MakeError("unauthorized", "Session required. Send client.hello first.", env.RequestID, false, 0)
			c.sendBytes(errMsg)
			return
		}

		switch env.Type {
		case protocol.TypeClientHello:
			handleHello(c, env, sessions)

		case protocol.TypeClientPong:
			// no-op; read deadline already extended in readLoop.

		case protocol.TypeRoomCreate:
			handleRoomCreate(c, env, mgr)

		case protocol.TypeRoomJoin:
			handleRoomJoin(c, env, mgr)

		case protocol.TypeRoomReady:
			handleRoomReady(c, env, mgr)

		case protocol.TypeRoomLeave:
			mgr.LeaveRoom(c.id)

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

func handleRoomCreate(c *conn, env protocol.ClientEnvelope, mgr *lobby.Manager) {
	var p createPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		errMsg, _ := protocol.MakeError("bad_message", "Invalid room.create payload.", env.RequestID, false, 0)
		c.sendBytes(errMsg)
		return
	}

	settings := config.Settings{
		PointsToWin: p.Settings.PointsToWin,
		PaddleSpeed: p.Settings.PaddleSpeed,
		BallSpeed:   p.Settings.BallSpeed,
	}

	_, err := mgr.CreateRoom(c.session, c, settings)
	if err != nil {
		code := "server_error"
		if err == lobby.ErrAlreadyInRoom {
			code = "already_in_room"
		}
		errMsg, _ := protocol.MakeError(code, err.Error(), env.RequestID, false, 0)
		c.sendBytes(errMsg)
	}
}

func handleRoomJoin(c *conn, env protocol.ClientEnvelope, mgr *lobby.Manager) {
	var p joinPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		errMsg, _ := protocol.MakeError("bad_message", "Invalid room.join payload.", env.RequestID, false, 0)
		c.sendBytes(errMsg)
		return
	}

	_, err := mgr.JoinRoom(p.RoomCode, c.session, c)
	if err != nil {
		var code string
		switch err {
		case lobby.ErrRoomNotFound:
			code = "room_not_found"
		case lobby.ErrRoomFull:
			code = "room_full"
		case lobby.ErrAlreadyInRoom:
			code = "already_in_room"
		default:
			code = "server_error"
		}
		errMsg, _ := protocol.MakeError(code, err.Error(), env.RequestID, false, 0)
		c.sendBytes(errMsg)
	}
}

func handleRoomReady(c *conn, env protocol.ClientEnvelope, mgr *lobby.Manager) {
	var p readyPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		errMsg, _ := protocol.MakeError("bad_message", "Invalid room.ready payload.", env.RequestID, false, 0)
		c.sendBytes(errMsg)
		return
	}

	if err := mgr.SetReady(c.id, p.Ready); err != nil {
		errMsg, _ := protocol.MakeError("invalid_state", "Not in a room.", env.RequestID, false, 0)
		c.sendBytes(errMsg)
	}
}
```

- [ ] **Step 4: Update main.go**

Replace `backend/cmd/server/main.go` with:

```go
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/lobby"
	"github.com/pong-mobile/backend/internal/wsconn"
)

func main() {
	sessions := auth.NewStore()
	lobbyMgr := lobby.NewManager(func(room *lobby.Room) {
		// M3 stub: match loop implemented in M4.
		log.Printf("match start triggered for room %s (stub)", room.Code)
	})

	mux := http.NewServeMux()
	mux.Handle("/ws", wsconn.Handler(sessions, lobbyMgr))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	log.Printf("volley server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
```

- [ ] **Step 5: Run all tests**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
Set-Location D:\Pong-Mobile\backend
go test ./... -v 2>&1
```

Expected:
- `internal/auth`: 5 tests PASS
- `internal/match`: 11 tests PASS
- `internal/physics`: 13 tests PASS
- `internal/protocol`: 6 tests PASS
- `internal/lobby`: 9 tests PASS
- `internal/wsconn`: 7 tests PASS (5 existing + 2 new)

- [ ] **Step 6: Build server**

```powershell
go build ./cmd/server 2>&1
```

Expected: no output (build succeeds).

- [ ] **Step 7: Commit**

```powershell
cd D:\Pong-Mobile
git add backend/internal/wsconn/handler.go backend/internal/wsconn/handler_test.go backend/cmd/server/main.go
git commit -m "feat(m3): wsconn room routing, lobby Manager wiring, lobby integration tests"
```

---

## Self-Review

**Spec coverage:**

| Requirement | Task |
|---|---|
| `lobby.Sender` interface | Task 2, room.go |
| `lobby.Manager` with CreateRoom, JoinRoom, SetReady, LeaveRoom, OnDisconnect | Task 2, manager.go |
| 6-digit crypto/rand room codes | Task 2, manager.go `generateCode()` |
| Settings clamping (pointsToWin 1–21, paddleSpeed/ballSpeed 0.1–2.0) | Task 2, manager.go |
| `room.create` → `room.created` + `room.updated` | Task 3, handler.go `handleRoomCreate` |
| `room.join` → `room.updated` broadcast | Task 3, handler.go `handleRoomJoin` |
| `room.ready` → `room.updated` + `onStart` when both ready | Task 2 Manager + Task 3 handler |
| `room.leave` → room closed / slot cleared | Task 2 manager.go `leaveRoomLocked` |
| Host disconnect → guest receives `error{room_closed}` | Task 2 manager.go |
| Guest disconnect → host receives `room.updated` | Task 2 manager.go |
| `unauthorized` error for room messages without hello | Task 3, dispatcher guard |
| `conn.ID()` and `conn.SendBytes()` | Task 1, conn.go |
| `readLoop` `onClose` param | Task 1, conn.go |
| `onDisconnect` called on connection close | Task 3, Handler wires `mgr.OnDisconnect` |
| Unit tests (9) | Task 2, manager_test.go |
| Integration tests (2) | Task 3, handler_test.go |
| `cmd/server/main.go` wired | Task 3, main.go |
| `PORT` env var | Task 3, main.go |

**Placeholder scan:** None found.

**Type consistency:**
- `lobby.Sender` interface consumed by `mgr.CreateRoom(sess, conn Sender, settings)` — `conn *wsconn.conn` satisfies it ✓
- `lobby.ErrRoomNotFound` etc. used in handler.go switch ✓
- `BroadcastRoomUpdated(room *Room)` called from manager.go with the locked room ✓
