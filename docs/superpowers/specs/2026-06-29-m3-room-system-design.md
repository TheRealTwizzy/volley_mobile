# M3 Room System Design

## Goal

Add room creation, joining, ready state, and match-start trigger to the Go backend. After this milestone, two connected clients can create/join a room, mark themselves ready, and trigger a match to start (match loop is a stub — implemented in M4).

## Architecture

New package `backend/internal/lobby` owns all room state. `wsconn/handler.go` routes room messages to a `lobby.Manager` injected at startup. The Manager broadcasts `room.updated` to both connected players whenever room state changes.

## Components

### `backend/internal/lobby/room.go`

```
Room struct:
  ID       string          // UUID
  Code     string          // 6-digit numeric string, e.g. "842913"
  HostSlot int             // 0
  Players  [2]*lobby.Slot
  Settings config.Settings
  Status   RoomStatus      // "waiting" | "starting"

Slot struct:
  Session  *auth.Session
  Conn     Sender          // interface: SendBytes([]byte)
  Ready    bool
  Connected bool
```

Room codes are random 6-digit strings generated with `crypto/rand`. Uniqueness is checked against the Manager's index before returning.

### `backend/internal/lobby/manager.go`

```
Manager struct:
  rooms   sync.Map  // code → *Room
  byConn  sync.Map  // connID → *Room (for disconnect lookup)
  onStart func(room *Room)  // called when both players ready
```

Public API:
- `NewManager(onStart func(*Room)) *Manager`
- `(m *Manager) CreateRoom(sess *auth.Session, conn Sender, settings config.Settings) (*Room, error)`
- `(m *Manager) JoinRoom(code string, sess *auth.Session, conn Sender) (*Room, error)` — error if full, not found
- `(m *Manager) SetReady(connID string, ready bool) error`
- `(m *Manager) LeaveRoom(connID string)`
- `(m *Manager) OnDisconnect(connID string)` — same as LeaveRoom but marks Connected=false; if match already started, handled by matchmgr

`Sender` interface:
```go
type Sender interface {
    SendBytes(data []byte)
    ID() string
}
```

`wsconn.conn` implements `Sender`. `ID()` returns a UUID assigned at connection time.

### `backend/internal/lobby/broadcast.go`

```
broadcastRoomUpdated(room *Room)  // marshals room.updated, sends to all connected slots
```

### `wsconn/handler.go` additions

New cases in `makeDispatcher`:
- `room.create` → `lobby.Manager.CreateRoom`
- `room.join` → `lobby.Manager.JoinRoom`
- `room.ready` → `lobby.Manager.SetReady`
- `room.leave` → `lobby.Manager.LeaveRoom`

`readLoop` calls `manager.OnDisconnect(connID)` in its deferred cleanup.

### `backend/cmd/server/main.go` wiring

```go
matchMgr := matchmgr.NewManager()  // stub in M3, real in M4
lobbyMgr := lobby.NewManager(func(room *lobby.Room) {
    matchMgr.StartMatch(room)
})
mux.Handle("/ws", wsconn.Handler(sessions, lobbyMgr))
```

## Protocol Messages

See `NETWORK_PROTOCOL.md §9`.

### Inbound (client → server)

| Type | Payload fields | Action |
|---|---|---|
| `room.create` | `settings.{pointsToWin,paddleSpeed,ballSpeed}` (all optional, defaults used if omitted) | Create room, respond `room.created`, broadcast `room.updated` |
| `room.join` | `roomCode` | Join room, respond `room.updated` to joiner, broadcast `room.updated` to host |
| `room.ready` | `ready: bool` | Toggle ready flag, broadcast `room.updated`; if both ready → call onStart |
| `room.leave` | _(none)_ | Remove player from room, broadcast `room.updated` to remaining player |

### Outbound (server → client)

| Type | When |
|---|---|
| `room.created` | Response to `room.create` — includes `roomId`, `roomCode`, `hostPlayerId`, `settings` |
| `room.updated` | Broadcast to all players on any room state change |
| `error` | `room_not_found`, `room_full`, `already_in_room`, `invalid_state` |

When both players are ready the server does **not** wait for any additional client message. It immediately:
1. Sets `room.Status = "starting"`
2. Broadcasts final `room.updated`
3. Calls `onStart(room)`

## Disconnect Handling

- Player disconnects while in room → `OnDisconnect` called
- If host disconnects: room closes, guest receives `error{code:"room_closed", recoverable:false}`
- If guest disconnects: guest slot cleared, room returns to one-player state, host receives `room.updated`
- If either player disconnects after `onStart` fires: handled by matchmgr (not lobby)

## Settings Validation

| Field | Default | Allowed range |
|---|---|---|
| `pointsToWin` | 5 | 1–21 |
| `paddleSpeed` | 0.9 | 0.1–2.0 |
| `ballSpeed` | 0.55 | 0.1–2.0 |

Out-of-range values are clamped silently.

## Testing

Integration tests in `backend/internal/lobby/manager_test.go` (table-driven, no network):
- Create room → correct code format, room in Manager
- Join room → both slots filled
- Join full room → `room_full` error
- Join nonexistent room → `room_not_found` error
- Both ready → onStart called
- One ready, one not → onStart not called
- Host disconnect → room closed, guest notified
- Guest disconnect → room has one player

`wsconn/handler_test.go` gets two new integration tests using `httptest`:
- Two clients: create + join + both ready → `onStart` callback fires
- Client sends room message without prior hello → `error{unauthorized}` (guard: check `c.session != nil`)

## File Summary

| File | Action |
|---|---|
| `backend/internal/lobby/room.go` | Create |
| `backend/internal/lobby/manager.go` | Create |
| `backend/internal/lobby/broadcast.go` | Create |
| `backend/internal/lobby/manager_test.go` | Create |
| `backend/internal/wsconn/conn.go` | Add `ID() string` method |
| `backend/internal/wsconn/handler.go` | Add room message cases, inject Manager |
| `backend/internal/wsconn/handler_test.go` | Add 2 room integration tests |
| `backend/cmd/server/main.go` | Wire lobby.Manager |
