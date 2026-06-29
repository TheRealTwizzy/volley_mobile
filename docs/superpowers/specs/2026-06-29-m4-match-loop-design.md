# M4 Match Loop Design

## Goal

Wire the existing physics/match engine into a live server-authoritative match loop. Two clients in a room can play a full match end-to-end: paddle input sent, snapshots received, scoring detected, match ended. Disconnect/reconnect within a match is handled.

## Architecture

New package `backend/internal/matchmgr` owns running matches. Each active match runs in its own goroutine (`matchLoop`). The loop ticks at 30/s, broadcasts snapshots at 20/s, and processes inbound input from a buffered channel. The `wsconn` handler routes `input.paddle_target` and `match.reconnect` into the appropriate match via the Manager.

`matchmgr.Manager` replaces the M3 stub.

## Components

### `backend/internal/matchmgr/match_run.go`

```
MatchRun struct:
  State     match.MatchState
  Players   [2]PlayerConn   // connID → Sender
  InputCh   chan InputMsg    // buffered 64
  mu        sync.Mutex      // guards State during reconnect write
  cancelFn  context.CancelFunc

PlayerConn struct:
  Sender    lobby.Sender
  ConnID    string
  ReconnectDeadline time.Time  // zero = connected
```

`matchLoop(ctx context.Context, run *MatchRun)`:
1. Send `match.countdown` to both players (3 s).
2. Sleep until countdown ends.
3. Send `match.started` to each player (with their `playerSlot` p1/p2).
4. Enter tick loop:
   - Drain `InputCh` (apply all queued inputs via `match.SetPlayerTarget`).
   - Call `match.Tick(state)`.
   - Every 3rd tick: broadcast `match.snapshot`.
   - On `TickResult.Scored`: send `match.score`, schedule rally reset after `RallyResetMs`.
   - On `TickResult.MatchEnded`: send `match.ended`, call `onMatchEnd(run)`, return.
5. Disconnect handling: if a player's `Sender` is nil (disconnected), pause ticking, set `ReconnectDeadline = now + 10s`. Send `player.disconnected` to the other player. If deadline passes without reconnect, send `match.ended{reason:opponent_disconnected}` to the still-connected player, return.

### `backend/internal/matchmgr/manager.go`

```
Manager struct:
  runs    sync.Map   // matchID → *MatchRun
  byConn  sync.Map   // connID → *MatchRun
  onEnd   func(run *MatchRun)
```

Public API:
- `NewManager(onEnd func(*MatchRun)) *Manager`
- `(m *Manager) StartMatch(room *lobby.Room)` — builds MatchRun, starts goroutine
- `(m *Manager) HandleInput(connID string, env protocol.ClientEnvelope)` — dispatches to InputCh
- `(m *Manager) HandleReconnect(connID string, sess *auth.Session, sender lobby.Sender, matchID string)` — reattaches sender, sends `match.reconnected`
- `(m *Manager) OnDisconnect(connID string)` — nilifies sender in MatchRun, triggers reconnect window

### `backend/internal/matchmgr/snapshot.go`

Pure helpers:
- `BuildSnapshot(state match.MatchState, matchID string) []byte` — marshals `match.snapshot`
- `BuildStarted(state match.MatchState, matchID string, slot int) []byte` — marshals `match.started` per-player view

### `backend/internal/wsconn/handler.go` additions

New dispatcher cases:
- `input.paddle_target` → `matchMgr.HandleInput(connID, env)`
- `match.reconnect` → parse payload, call `matchMgr.HandleReconnect(...)`

`readLoop` deferred cleanup order:
1. `lobbyMgr.OnDisconnect(connID)` (room phase)
2. `matchMgr.OnDisconnect(connID)` (match phase)

## Protocol Messages

See `NETWORK_PROTOCOL.md §10–15`.

### Outbound sequence

```
match.countdown   → both players (at match start)
match.started     → each player individually (includes playerSlot, initialState)
match.snapshot    → both players every 3rd tick (~50ms)
match.score       → both players when a point is scored
match.rally_reset → both players after RallyResetMs delay
match.ended       → both players when game ends
player.disconnected → remaining player when opponent drops
player.reconnected  → remaining player when opponent reconnects
match.reconnected   → reconnecting player (full current state)
```

### Inbound

| Type | Handler |
|---|---|
| `input.paddle_target` | `matchMgr.HandleInput` |
| `match.reconnect` | `matchMgr.HandleReconnect` |

### Input validation

- `targetX` must be 0.0–1.0 (clamped by `match.SetPlayerTarget` already)
- `clientSeq` must be > last accepted seq (enforced by `match.SetPlayerTarget`)
- Input for wrong `matchId` → `error{code:"match_not_found"}`
- Input from player not in any match → `error{code:"invalid_state"}`

## Tick Timing

```
tickInterval  = 33.333ms  (30/s)
snapshotEvery = 3 ticks   (20/s = every 50ms)
```

Use `time.NewTicker(tickInterval)`. Do not spin or sleep in a hot loop — `ticker.C` blocks between ticks.

Rally reset delay: `config.Default.RallyResetMs` (1500ms). After a score event, set a `time.AfterFunc` that calls `match.RallyReset` and resumes ticking. Ticking pauses between score and reset.

## Disconnect / Reconnect

1. `wsconn` detects close → calls `matchMgr.OnDisconnect(connID)`.
2. `MatchRun.Players[slot].Sender = nil`. Tick loop detects nil sender at next iteration.
3. Loop pauses, sends `player.disconnected{reconnectDeadline: now+10s}` to other player.
4. A `time.AfterFunc(10s, ...)` fires if no reconnect → `match.ended{reason:opponent_disconnected}`.
5. On reconnect: `HandleReconnect` sets new Sender, sends `match.reconnected{currentState}`, sends `player.reconnected` to other player, resumes tick loop.

Reconnect is identified by `sessionToken` + `matchId` from the `match.reconnect` payload. Auth store `GetByToken` validates the token.

## Testing

Unit tests in `matchmgr/match_run_test.go`:
- Snapshot broadcast fires every 3 ticks, not every tick
- Score event increments score, sends match.score
- Match ends at pointsToWin, sends match.ended
- Input channel drains before tick advances
- Disconnect pauses loop, reconnect resumes it
- Disconnect timeout sends match.ended{opponent_disconnected}

Integration test in `wsconn/handler_test.go`:
- Two clients complete a full match (mocked: pre-set ball position so score fires quickly)

## File Summary

| File | Action |
|---|---|
| `backend/internal/matchmgr/match_run.go` | Create |
| `backend/internal/matchmgr/manager.go` | Create |
| `backend/internal/matchmgr/snapshot.go` | Create |
| `backend/internal/matchmgr/match_run_test.go` | Create |
| `backend/internal/wsconn/handler.go` | Add input/reconnect cases, inject matchMgr |
| `backend/cmd/server/main.go` | Wire real matchmgr.Manager |

## ServerTick omitempty Fix

Before implementing this milestone, remove `omitempty` from `ServerEnvelope.ServerTick` in `protocol/message.go`. Tick 0 is sent in `match.started` and must appear on the wire.
