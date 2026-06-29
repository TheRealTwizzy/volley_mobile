# M4 Match Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the existing physics/match engine into a live server-authoritative `matchmgr` package so two clients can play a full match end-to-end: countdown → input → snapshots → scoring → match end → disconnect/reconnect.

**Architecture:** New `backend/internal/matchmgr` package owns running matches. Each match runs in its own goroutine ticking at 30/s with snapshots at 20/s. `matchmgr.Manager` replaces the M3 stub `onStart` callback. `wsconn/handler.go` gains `input.paddle_target` and `match.reconnect` dispatch cases.

**Tech Stack:** Go 1.22, stdlib only (context, sync, time, encoding/json, crypto/rand)

## Global Constraints

- Go 1.22, module `github.com/pong-mobile/backend`
- No new external dependencies
- Tick rate: 30/s (`tickInterval = time.Second/30`); snapshot every 3rd tick (20/s)
- `match.Tick` signature: `Tick(state MatchState) (MatchState, TickResult)` — pure, returns new state
- `match.RallyReset(state MatchState, lastScoringSlot int) MatchState` — pure
- `match.SetPlayerTarget(state MatchState, slot int, targetX float64, seq int) MatchState` — pure
- Lobby `Sender` interface: `{ ID() string; SendBytes([]byte) }` in `backend/internal/lobby`
- `lobby.Room.Players[2]*lobby.Slot` — each Slot has `.Session *auth.Session`, `.Conn lobby.Sender`
- `lobby.Room.Code string`, `lobby.Room.Settings config.Settings`
- `protocol.MarshalServer(msgType string, tick int64, payload any) []byte` — use for all outbound
- `protocol.MakeError(code, message, requestID string) []byte` — use for error frames
- `protocol.ParseClient(data []byte) (ClientEnvelope, error)` — already used in handler
- Slot 0 = Player 1 (bottom, `paddleY=0.93`); Slot 1 = Player 2 (top, `paddleY=0.07`)
- Reconnect window: 10s (`config.Default.ReconnectWindowMs = 10000`)
- Rally reset delay: 1500ms (`config.Default.RallyResetMs`)
- `ServerTick omitempty` is already fixed (commit 79ee2fd) — do NOT touch protocol/message.go
- Game is named **Volley** — no "Pong" in any user-visible string
- YAGNI: implement exactly what this plan specifies

---

### Task 1: Protocol constants + matchmgr snapshot helpers

**Files:**
- Modify: `backend/internal/protocol/message.go`
- Create: `backend/internal/matchmgr/snapshot.go`
- Create: `backend/internal/matchmgr/snapshot_test.go`

**Interfaces:**
- Consumes: `match.MatchState`, `protocol.MarshalServer`, `protocol.ServerEnvelope`
- Produces:
  - `TypeInputPaddleTarget`, `TypeMatchCountdown`, `TypeMatchStarted`, `TypeMatchSnapshot`, `TypeMatchScore`, `TypeMatchRallyReset`, `TypeMatchEnded`, `TypeMatchReconnect`, `TypeMatchReconnected`, `TypePlayerDisconnected`, `TypePlayerReconnected` constants
  - `matchmgr.BuildSnapshot(state match.MatchState, matchID string) []byte`
  - `matchmgr.BuildStarted(state match.MatchState, matchID string, slot int) []byte`
  - `matchmgr.BuildCountdown(matchID string, startsAt time.Time) []byte`

- [ ] **Step 1: Add protocol type constants**

In `backend/internal/protocol/message.go`, append to the const block:

```go
	TypeInputPaddleTarget  = "input.paddle_target"
	TypeMatchCountdown     = "match.countdown"
	TypeMatchStarted       = "match.started"
	TypeMatchSnapshot      = "match.snapshot"
	TypeMatchScore         = "match.score"
	TypeMatchRallyReset    = "match.rally_reset"
	TypeMatchEnded         = "match.ended"
	TypeMatchReconnect     = "match.reconnect"
	TypeMatchReconnected   = "match.reconnected"
	TypePlayerDisconnected = "player.disconnected"
	TypePlayerReconnected  = "player.reconnected"
```

- [ ] **Step 2: Verify protocol still compiles**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
cd D:\Pong-Mobile\backend
go test ./internal/protocol/... -v
```

Expected: 6 tests pass, no compile errors.

- [ ] **Step 3: Write snapshot_test.go with failing tests**

Create `backend/internal/matchmgr/snapshot_test.go`:

```go
package matchmgr_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/pong-mobile/backend/internal/config"
	"github.com/pong-mobile/backend/internal/match"
	"github.com/pong-mobile/backend/internal/matchmgr"
	"github.com/pong-mobile/backend/internal/protocol"
)

func baseState() match.MatchState {
	s := match.NewMatchState("m1", config.Default)
	s.Status = match.StatusActive
	s.ServerTick = 7
	s.Players[0].Score = 2
	s.Players[1].Score = 1
	s.Players[0].PaddleX = 0.3
	s.Players[1].PaddleX = 0.7
	s.Ball.X = 0.5
	s.Ball.Y = 0.5
	s.Ball.VX = 0.1
	s.Ball.VY = -0.9
	return s
}

func TestBuildSnapshot_EnvelopeShape(t *testing.T) {
	raw := matchmgr.BuildSnapshot(baseState(), "m1")
	var env protocol.ServerEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Type != protocol.TypeMatchSnapshot {
		t.Errorf("type=%q want %q", env.Type, protocol.TypeMatchSnapshot)
	}
	if env.ServerTick != 7 {
		t.Errorf("serverTick=%d want 7", env.ServerTick)
	}
	if env.ServerTime == 0 {
		t.Error("serverTime must be nonzero")
	}
}

func TestBuildSnapshot_Payload(t *testing.T) {
	raw := matchmgr.BuildSnapshot(baseState(), "m1")
	var env struct {
		Payload struct {
			MatchID string  `json:"matchId"`
			Ball    struct {
				X float64 `json:"x"`
			} `json:"ball"`
			Players struct {
				P1 struct {
					PaddleX float64 `json:"paddleX"`
					Score   int     `json:"score"`
				} `json:"p1"`
				P2 struct {
					PaddleX float64 `json:"paddleX"`
					Score   int     `json:"score"`
				} `json:"p2"`
			} `json:"players"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Payload.MatchID != "m1" {
		t.Errorf("matchId=%q want m1", env.Payload.MatchID)
	}
	if env.Payload.Players.P1.Score != 2 {
		t.Errorf("p1.score=%d want 2", env.Payload.Players.P1.Score)
	}
	if env.Payload.Players.P2.PaddleX != 0.7 {
		t.Errorf("p2.paddleX=%.2f want 0.7", env.Payload.Players.P2.PaddleX)
	}
}

func TestBuildStarted_SlotAssignment(t *testing.T) {
	s := baseState()
	s.Players[0].Score = 0
	s.Players[1].Score = 0

	rawP1 := matchmgr.BuildStarted(s, "m1", 0)
	rawP2 := matchmgr.BuildStarted(s, "m1", 1)

	var envP1 struct {
		Payload struct {
			PlayerSlot   string `json:"playerSlot"`
			OpponentSlot string `json:"opponentSlot"`
		} `json:"payload"`
	}
	var envP2 struct {
		Payload struct {
			PlayerSlot   string `json:"playerSlot"`
			OpponentSlot string `json:"opponentSlot"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(rawP1, &envP1); err != nil {
		t.Fatalf("p1 unmarshal: %v", err)
	}
	if err := json.Unmarshal(rawP2, &envP2); err != nil {
		t.Fatalf("p2 unmarshal: %v", err)
	}
	if envP1.Payload.PlayerSlot != "p1" || envP1.Payload.OpponentSlot != "p2" {
		t.Errorf("p1 slots wrong: got %s/%s", envP1.Payload.PlayerSlot, envP1.Payload.OpponentSlot)
	}
	if envP2.Payload.PlayerSlot != "p2" || envP2.Payload.OpponentSlot != "p1" {
		t.Errorf("p2 slots wrong: got %s/%s", envP2.Payload.PlayerSlot, envP2.Payload.OpponentSlot)
	}
}

func TestBuildCountdown_Fields(t *testing.T) {
	startsAt := time.Now().Add(3 * time.Second)
	raw := matchmgr.BuildCountdown("m1", startsAt)
	var env struct {
		Type    string `json:"type"`
		Payload struct {
			MatchID    string `json:"matchId"`
			StartsAt   int64  `json:"startsAt"`
			DurationMs int    `json:"durationMs"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Type != protocol.TypeMatchCountdown {
		t.Errorf("type=%q want match.countdown", env.Type)
	}
	if env.Payload.MatchID != "m1" {
		t.Errorf("matchId=%q want m1", env.Payload.MatchID)
	}
	if env.Payload.DurationMs != 3000 {
		t.Errorf("durationMs=%d want 3000", env.Payload.DurationMs)
	}
	if env.Payload.StartsAt == 0 {
		t.Error("startsAt must be nonzero")
	}
}
```

- [ ] **Step 4: Run tests — verify they fail**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
cd D:\Pong-Mobile\backend
go test ./internal/matchmgr/... 2>&1
```

Expected: compile error (package doesn't exist yet).

- [ ] **Step 5: Create snapshot.go**

Create `backend/internal/matchmgr/snapshot.go`:

```go
package matchmgr

import (
	"time"

	"github.com/pong-mobile/backend/internal/match"
	"github.com/pong-mobile/backend/internal/protocol"
)

type snapshotBallPayload struct {
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	VX float64 `json:"vx"`
	VY float64 `json:"vy"`
}

type snapshotPlayerEntry struct {
	PaddleX float64 `json:"paddleX"`
	Score   int     `json:"score"`
}

type snapshotPayload struct {
	MatchID string `json:"matchId"`
	Ball    snapshotBallPayload `json:"ball"`
	Players struct {
		P1 snapshotPlayerEntry `json:"p1"`
		P2 snapshotPlayerEntry `json:"p2"`
	} `json:"players"`
}

// BuildSnapshot marshals a match.snapshot server envelope.
func BuildSnapshot(state match.MatchState, matchID string) []byte {
	p := snapshotPayload{MatchID: matchID}
	p.Ball = snapshotBallPayload{
		X: state.Ball.X, Y: state.Ball.Y,
		VX: state.Ball.VX, VY: state.Ball.VY,
	}
	p.Players.P1 = snapshotPlayerEntry{PaddleX: state.Players[0].PaddleX, Score: state.Players[0].Score}
	p.Players.P2 = snapshotPlayerEntry{PaddleX: state.Players[1].PaddleX, Score: state.Players[1].Score}
	return protocol.MarshalServer(protocol.TypeMatchSnapshot, state.ServerTick, p)
}

type startedPlayerEntry struct {
	PlayerID string  `json:"playerId"`
	PaddleX  float64 `json:"paddleX"`
	Score    int     `json:"score"`
}

type startedInitialState struct {
	Ball    snapshotBallPayload `json:"ball"`
	Players struct {
		P1 startedPlayerEntry `json:"p1"`
		P2 startedPlayerEntry `json:"p2"`
	} `json:"players"`
}

type startedPayload struct {
	MatchID      string              `json:"matchId"`
	PlayerSlot   string              `json:"playerSlot"`
	OpponentSlot string              `json:"opponentSlot"`
	Settings     startedSettings     `json:"settings"`
	InitialState startedInitialState `json:"initialState"`
}

type startedSettings struct {
	PointsToWin int `json:"pointsToWin"`
	TickRate    int `json:"tickRate"`
	SnapshotRate int `json:"snapshotRate"`
}

// BuildStarted marshals a per-player match.started envelope.
// slot: 0=P1, 1=P2.
func BuildStarted(state match.MatchState, matchID string, slot int) []byte {
	slotNames := [2]string{"p1", "p2"}
	p := startedPayload{
		MatchID:      matchID,
		PlayerSlot:   slotNames[slot],
		OpponentSlot: slotNames[1-slot],
		Settings: startedSettings{
			PointsToWin:  state.Settings.PointsToWin,
			TickRate:     state.Settings.TickRate,
			SnapshotRate: state.Settings.SnapshotRate,
		},
	}
	p.InitialState.Ball = snapshotBallPayload{
		X: state.Ball.X, Y: state.Ball.Y,
		VX: state.Ball.VX, VY: state.Ball.VY,
	}
	p.InitialState.Players.P1 = startedPlayerEntry{PaddleX: state.Players[0].PaddleX}
	p.InitialState.Players.P2 = startedPlayerEntry{PaddleX: state.Players[1].PaddleX}
	return protocol.MarshalServer(protocol.TypeMatchStarted, state.ServerTick, p)
}

type countdownPayload struct {
	MatchID    string `json:"matchId"`
	StartsAt   int64  `json:"startsAt"`
	DurationMs int    `json:"durationMs"`
}

// BuildCountdown marshals a match.countdown envelope.
func BuildCountdown(matchID string, startsAt time.Time) []byte {
	p := countdownPayload{
		MatchID:    matchID,
		StartsAt:   startsAt.UnixMilli(),
		DurationMs: 3000,
	}
	return protocol.MarshalServer(protocol.TypeMatchCountdown, 0, p)
}
```

- [ ] **Step 6: Run tests — verify they pass**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
cd D:\Pong-Mobile\backend
go test ./internal/matchmgr/... -v 2>&1
```

Expected: 4 snapshot tests pass.

- [ ] **Step 7: Commit**

```powershell
git add backend/internal/protocol/message.go backend/internal/matchmgr/
git commit -m "feat(m4): protocol match constants + matchmgr snapshot helpers"
```

---

### Task 2: MatchRun struct + tick loop + unit tests

**Files:**
- Create: `backend/internal/matchmgr/match_run.go`
- Create: `backend/internal/matchmgr/match_run_test.go`

**Interfaces:**
- Consumes: `match.Tick`, `match.RallyReset`, `match.SetPlayerTarget`, `match.NewMatchState`, snapshot helpers from Task 1
- Produces:
  - `InputMsg` struct
  - `PlayerConn` struct
  - `MatchRun` struct with `InputCh chan InputMsg`, `cancelFn context.CancelFunc`
  - `matchLoop(ctx context.Context, run *MatchRun)` (unexported, started as goroutine)
  - `newMatchRun(matchID string, settings config.Settings, players [2]PlayerConn, onEnd func(*MatchRun)) *MatchRun`

- [ ] **Step 1: Write failing unit tests**

Create `backend/internal/matchmgr/match_run_test.go`:

```go
package matchmgr

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/pong-mobile/backend/internal/config"
	"github.com/pong-mobile/backend/internal/match"
	"github.com/pong-mobile/backend/internal/protocol"
)

// mockSender records every SendBytes call.
type mockSender struct {
	mu   sync.Mutex
	id   string
	msgs [][]byte
}

func (m *mockSender) ID() string { return m.id }
func (m *mockSender) SendBytes(b []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = append(m.msgs, b)
}
func (m *mockSender) received() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]byte, len(m.msgs))
	copy(out, m.msgs)
	return out
}
func (m *mockSender) lastType() string {
	msgs := m.received()
	if len(msgs) == 0 {
		return ""
	}
	var env protocol.ServerEnvelope
	_ = json.Unmarshal(msgs[len(msgs)-1], &env)
	return env.Type
}
func (m *mockSender) countType(msgType string) int {
	n := 0
	for _, raw := range m.received() {
		var env protocol.ServerEnvelope
		_ = json.Unmarshal(raw, &env)
		if env.Type == msgType {
			n++
		}
	}
	return n
}

func newTestRun(t *testing.T) (*MatchRun, *mockSender, *mockSender, context.CancelFunc) {
	t.Helper()
	p1 := &mockSender{id: "c1"}
	p2 := &mockSender{id: "c2"}
	players := [2]PlayerConn{
		{Sender: p1, ConnID: "c1"},
		{Sender: p2, ConnID: "c2"},
	}
	cfg := config.Default
	cfg.PointsToWin = 1 // short match for tests
	run := newMatchRun("m-test", cfg, players, func(*MatchRun) {})
	ctx, cancel := context.WithCancel(context.Background())
	return run, p1, p2, func() {
		cancel()
		time.Sleep(50 * time.Millisecond) // let goroutine drain
	}
}

func TestMatchLoop_SendsCountdownAndStarted(t *testing.T) {
	run, p1, p2, stop := newTestRun(t)
	defer stop()
	go matchLoop(context.Background(), run)
	time.Sleep(4 * time.Second) // wait past 3s countdown
	if p1.countType(protocol.TypeMatchCountdown) != 1 {
		t.Error("p1 did not receive match.countdown")
	}
	if p2.countType(protocol.TypeMatchStarted) != 1 {
		t.Error("p2 did not receive match.started")
	}
}

func TestMatchLoop_SnapshotEvery3Ticks(t *testing.T) {
	run, p1, _, stop := newTestRun(t)
	defer stop()
	// Pre-set active state to skip countdown
	run.mu.Lock()
	run.State.Status = match.StatusActive
	run.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go matchLoopActive(ctx, run) // test helper that skips countdown
	<-ctx.Done()
	snapshots := p1.countType(protocol.TypeMatchSnapshot)
	// ~500ms / 50ms per snapshot = ~10; accept 7–13
	if snapshots < 7 || snapshots > 13 {
		t.Errorf("snapshots=%d want ~10 in 500ms", snapshots)
	}
}

func TestMatchLoop_ScoreIncrementsAndSendsEvent(t *testing.T) {
	run, p1, p2, stop := newTestRun(t)
	defer stop()
	// Force a score on next tick by positioning ball past P2's goal
	run.mu.Lock()
	run.State.Status = match.StatusActive
	run.State.Ball.Y = 0.001 // near top → P1 scores when ball.y - radius < 0
	run.State.Ball.VY = -1.0
	run.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	go matchLoopActive(ctx, run)
	<-ctx.Done()
	if p1.countType(protocol.TypeMatchScore) == 0 {
		t.Error("p1 did not receive match.score")
	}
	if p2.countType(protocol.TypeMatchScore) == 0 {
		t.Error("p2 did not receive match.score")
	}
}

func TestMatchLoop_MatchEndsAtPointsToWin(t *testing.T) {
	run, p1, p2, stop := newTestRun(t)
	defer stop()
	ended := make(chan struct{})
	run.onEnd = func(*MatchRun) { close(ended) }
	run.mu.Lock()
	run.State.Status = match.StatusActive
	run.State.Ball.Y = 0.001
	run.State.Ball.VY = -1.0
	run.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go matchLoopActive(ctx, run)
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("match did not end within timeout")
	}
	if p1.countType(protocol.TypeMatchEnded) == 0 {
		t.Error("p1 did not receive match.ended")
	}
	if p2.countType(protocol.TypeMatchEnded) == 0 {
		t.Error("p2 did not receive match.ended")
	}
}

func TestMatchLoop_InputApplied(t *testing.T) {
	run, _, _, stop := newTestRun(t)
	defer stop()
	run.mu.Lock()
	run.State.Status = match.StatusActive
	run.mu.Unlock()
	// Queue input before starting loop
	run.InputCh <- InputMsg{Slot: 0, TargetX: 0.8, Seq: 1}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go matchLoopActive(ctx, run)
	time.Sleep(60 * time.Millisecond) // wait 2 ticks
	run.mu.Lock()
	x := run.State.Players[0].TargetX
	run.mu.Unlock()
	if x != 0.8 {
		t.Errorf("targetX=%.2f want 0.8 after input applied", x)
	}
}

func TestMatchLoop_DisconnectPausesLoop(t *testing.T) {
	run, p1, p2, stop := newTestRun(t)
	defer stop()
	run.mu.Lock()
	run.State.Status = match.StatusActive
	run.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go matchLoopActive(ctx, run)
	time.Sleep(100 * time.Millisecond) // let loop run briefly
	// Disconnect P2
	run.mu.Lock()
	run.Players[1].Sender = nil
	run.mu.Unlock()
	snapsBefore := p1.countType(protocol.TypeMatchSnapshot)
	time.Sleep(200 * time.Millisecond)
	snapsAfter := p1.countType(protocol.TypeMatchSnapshot)
	// Should get player.disconnected
	if p1.countType(protocol.TypePlayerDisconnected) == 0 {
		t.Error("p1 did not receive player.disconnected")
	}
	// Snapshot rate should stop (or slow significantly) after disconnect
	if snapsAfter-snapsBefore > 3 {
		t.Errorf("snapshots continued during pause: %d new snapshots", snapsAfter-snapsBefore)
	}
}

func TestMatchLoop_ReconnectResumes(t *testing.T) {
	run, p1, p2, stop := newTestRun(t)
	defer stop()
	run.mu.Lock()
	run.State.Status = match.StatusActive
	run.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go matchLoopActive(ctx, run)
	time.Sleep(100 * time.Millisecond)
	// Disconnect then reconnect P2
	run.mu.Lock()
	run.Players[1].Sender = nil
	run.mu.Unlock()
	time.Sleep(150 * time.Millisecond)
	newP2 := &mockSender{id: "c2-new"}
	run.mu.Lock()
	run.Players[1].Sender = newP2
	run.Players[1].ConnID = "c2-new"
	run.Players[1].ReconnectDeadline = time.Time{} // clear deadline
	run.mu.Unlock()
	run.reconnectCh <- 1 // signal reconnect to slot 1
	time.Sleep(300 * time.Millisecond)
	// New sender should receive snapshots after reconnect
	if newP2.countType(protocol.TypeMatchReconnected) == 0 {
		t.Error("reconnecting player did not receive match.reconnected")
	}
	if p1.countType(protocol.TypePlayerReconnected) == 0 {
		t.Error("p1 did not receive player.reconnected")
	}
}
```

Note: `matchLoopActive` is a test-only entry point that skips the 3s countdown and goes straight to the tick loop. Export or expose it via a `var` in match_run.go for test access.

- [ ] **Step 2: Run tests — verify compile fails**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
cd D:\Pong-Mobile\backend
go test ./internal/matchmgr/... 2>&1
```

Expected: compile errors (types don't exist yet).

- [ ] **Step 3: Create match_run.go**

Create `backend/internal/matchmgr/match_run.go`:

```go
package matchmgr

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/pong-mobile/backend/internal/config"
	"github.com/pong-mobile/backend/internal/lobby"
	"github.com/pong-mobile/backend/internal/match"
	"github.com/pong-mobile/backend/internal/protocol"
)

// InputMsg carries one client paddle input to the match loop.
type InputMsg struct {
	Slot    int     // 0=P1, 1=P2
	TargetX float64
	Seq     int
}

// PlayerConn holds one player's connection state inside a running match.
type PlayerConn struct {
	Sender            lobby.Sender // nil if disconnected
	ConnID            string
	ReconnectDeadline time.Time // zero = connected
}

// MatchRun is the complete state for one running match.
type MatchRun struct {
	MatchID     string
	State       match.MatchState
	Players     [2]PlayerConn
	InputCh     chan InputMsg
	reconnectCh chan int // slot index signaling reconnect
	mu          sync.Mutex
	cancelFn    context.CancelFunc
	onEnd       func(*MatchRun)
}

func newMatchRun(matchID string, settings config.Settings, players [2]PlayerConn, onEnd func(*MatchRun)) *MatchRun {
	state := match.NewMatchState(matchID, settings)
	state.Status = match.StatusWaiting
	ctx, cancel := context.WithCancel(context.Background())
	_ = ctx // ctx used inside matchLoop; cancel stored on run
	run := &MatchRun{
		MatchID:     matchID,
		State:       state,
		Players:     players,
		InputCh:     make(chan InputMsg, 64),
		reconnectCh: make(chan int, 4),
		cancelFn:    cancel,
		onEnd:       onEnd,
	}
	return run
}

// send delivers a message to a player slot, no-op if sender is nil.
func (r *MatchRun) send(slot int, data []byte) {
	r.mu.Lock()
	s := r.Players[slot].Sender
	r.mu.Unlock()
	if s != nil {
		s.SendBytes(data)
	}
}

// broadcast sends to both players.
func (r *MatchRun) broadcast(data []byte) {
	r.send(0, data)
	r.send(1, data)
}

// matchLoop is the main goroutine for a running match.
// Sequence: countdown → active tick loop → end.
func matchLoop(ctx context.Context, run *MatchRun) {
	defer func() {
		if run.onEnd != nil {
			run.onEnd(run)
		}
	}()

	// --- Countdown phase ---
	startsAt := time.Now().Add(3 * time.Second)
	countdown := BuildCountdown(run.MatchID, startsAt)
	run.broadcast(countdown)

	select {
	case <-time.After(time.Until(startsAt)):
	case <-ctx.Done():
		return
	}

	// --- Start phase ---
	run.mu.Lock()
	run.State.Status = match.StatusActive
	st := run.State
	run.mu.Unlock()

	run.send(0, BuildStarted(st, run.MatchID, 0))
	run.send(1, BuildStarted(st, run.MatchID, 1))

	matchLoopActive(ctx, run)
}

// matchLoopActive runs the tick loop (exported for tests via var).
// Assumes State.Status == StatusActive when called.
var matchLoopActive = func(ctx context.Context, run *MatchRun) {
	ticker := time.NewTicker(time.Second / 30)
	defer ticker.Stop()

	tickCount := 0
	paused := false
	var forfeitTimer *time.Timer

	for {
		select {
		case <-ctx.Done():
			return

		case slot := <-run.reconnectCh:
			// Player reconnected — resume
			run.mu.Lock()
			s := run.State
			sender := run.Players[slot].Sender
			run.mu.Unlock()
			if sender != nil {
				sender.SendBytes(buildReconnected(run.MatchID, s, slot))
			}
			// Notify other player
			other := 1 - slot
			run.send(other, buildPlayerReconnected(run.MatchID, slot))
			if forfeitTimer != nil {
				forfeitTimer.Stop()
				forfeitTimer = nil
			}
			paused = false

		case <-ticker.C:
			if paused {
				// Check if a sender came back without a reconnectCh signal (shouldn't happen but guard)
				continue
			}

			// Check for disconnected players
			run.mu.Lock()
			p0nil := run.Players[0].Sender == nil
			p1nil := run.Players[1].Sender == nil
			run.mu.Unlock()

			if p0nil || p1nil {
				slot := 0
				if p1nil {
					slot = 1
				}
				paused = true
				deadline := time.Now().Add(time.Duration(run.State.Settings.ReconnectWindowMs) * time.Millisecond)
				run.mu.Lock()
				run.Players[slot].ReconnectDeadline = deadline
				run.mu.Unlock()
				// Notify the other player
				other := 1 - slot
				run.send(other, buildPlayerDisconnected(run.MatchID, slot, deadline))
				// Forfeit timer
				forfeitTimer = time.AfterFunc(time.Until(deadline), func() {
					other := 1 - slot
					run.mu.Lock()
					stillNil := run.Players[slot].Sender == nil
					run.mu.Unlock()
					if stillNil {
						run.send(other, buildMatchEnded(run.MatchID, other, "opponent_disconnected"))
						run.cancelFn()
					}
				})
				continue
			}

			// Drain input channel
			drained := false
			for !drained {
				select {
				case inp := <-run.InputCh:
					run.mu.Lock()
					run.State = match.SetPlayerTarget(run.State, inp.Slot, inp.TargetX, inp.Seq)
					run.mu.Unlock()
				default:
					drained = true
				}
			}

			// Tick
			run.mu.Lock()
			newState, result := match.Tick(run.State)
			run.State = newState
			run.mu.Unlock()
			tickCount++

			// Snapshot every 3rd tick
			if tickCount%3 == 0 {
				run.mu.Lock()
				snap := BuildSnapshot(run.State, run.MatchID)
				run.mu.Unlock()
				run.broadcast(snap)
			}

			if result.Scored {
				run.mu.Lock()
				scoreMsg := buildScoreMsg(run.MatchID, run.State, result.ScoringSlot)
				run.mu.Unlock()
				run.broadcast(scoreMsg)

				if result.MatchEnded {
					run.broadcast(buildMatchEnded(run.MatchID, result.WinnerSlot, "points_to_win"))
					return
				}

				// Pause for rally reset
				paused = true
				scoringSlot := result.ScoringSlot
				time.AfterFunc(time.Duration(run.State.Settings.RallyResetMs)*time.Millisecond, func() {
					run.mu.Lock()
					run.State = match.RallyReset(run.State, scoringSlot)
					resetMsg := buildRallyReset(run.MatchID, run.State)
					run.mu.Unlock()
					run.broadcast(resetMsg)
					paused = false
				})
			}
		}
	}
}

// --- Outbound message builders ---

type scorePayload struct {
	MatchID     string         `json:"matchId"`
	ScoringSlot string         `json:"scoringSlot"`
	Score       map[string]int `json:"score"`
}

func buildScoreMsg(matchID string, state match.MatchState, scoringSlot int) []byte {
	slots := [2]string{"p1", "p2"}
	p := scorePayload{
		MatchID:     matchID,
		ScoringSlot: slots[scoringSlot],
		Score:       map[string]int{"p1": state.Players[0].Score, "p2": state.Players[1].Score},
	}
	return protocol.MarshalServer(protocol.TypeMatchScore, state.ServerTick, p)
}

type rallyResetPayload struct {
	MatchID string              `json:"matchId"`
	Ball    snapshotBallPayload `json:"ball"`
}

func buildRallyReset(matchID string, state match.MatchState) []byte {
	p := rallyResetPayload{
		MatchID: matchID,
		Ball:    snapshotBallPayload{X: state.Ball.X, Y: state.Ball.Y, VX: state.Ball.VX, VY: state.Ball.VY},
	}
	return protocol.MarshalServer(protocol.TypeMatchRallyReset, state.ServerTick, p)
}

type matchEndedPayload struct {
	MatchID    string         `json:"matchId"`
	WinnerSlot string         `json:"winnerSlot"`
	Reason     string         `json:"reason"`
	FinalScore map[string]int `json:"finalScore"`
}

func buildMatchEnded(matchID string, winnerSlot int, reason string) []byte {
	slots := [2]string{"p1", "p2"}
	p := matchEndedPayload{
		MatchID:    matchID,
		WinnerSlot: slots[winnerSlot],
		Reason:     reason,
	}
	return protocol.MarshalServer(protocol.TypeMatchEnded, 0, p)
}

type disconnectedPayload struct {
	MatchID           string `json:"matchId"`
	Slot              string `json:"slot"`
	ReconnectDeadline int64  `json:"reconnectDeadline"`
}

func buildPlayerDisconnected(matchID string, slot int, deadline time.Time) []byte {
	slots := [2]string{"p1", "p2"}
	p := disconnectedPayload{
		MatchID:           matchID,
		Slot:              slots[slot],
		ReconnectDeadline: deadline.UnixMilli(),
	}
	return protocol.MarshalServer(protocol.TypePlayerDisconnected, 0, p)
}

type reconnectedPayload struct {
	MatchID string `json:"matchId"`
	Slot    string `json:"slot"`
}

func buildPlayerReconnected(matchID string, slot int) []byte {
	slots := [2]string{"p1", "p2"}
	p := reconnectedPayload{MatchID: matchID, Slot: slots[slot]}
	return protocol.MarshalServer(protocol.TypePlayerReconnected, 0, p)
}

type matchReconnectedPayload struct {
	MatchID      string      `json:"matchId"`
	Slot         string      `json:"slot"`
	CurrentState interface{} `json:"currentState"`
}

func buildReconnected(matchID string, state match.MatchState, slot int) []byte {
	slots := [2]string{"p1", "p2"}
	snapshot := map[string]interface{}{
		"ball": map[string]float64{
			"x": state.Ball.X, "y": state.Ball.Y,
			"vx": state.Ball.VX, "vy": state.Ball.VY,
		},
		"players": map[string]interface{}{
			"p1": map[string]interface{}{"paddleX": state.Players[0].PaddleX, "score": state.Players[0].Score},
			"p2": map[string]interface{}{"paddleX": state.Players[1].PaddleX, "score": state.Players[1].Score},
		},
	}
	p := matchReconnectedPayload{MatchID: matchID, Slot: slots[slot], CurrentState: snapshot}
	return protocol.MarshalServer(protocol.TypeMatchReconnected, state.ServerTick, p)
}

// inputPaddleTargetPayload is used by the handler to parse input.paddle_target.
type inputPaddleTargetPayload struct {
	MatchID   string  `json:"matchId"`
	ClientSeq int     `json:"clientSeq"`
	TargetX   float64 `json:"targetX"`
}

// ParseInputPaddleTarget extracts an InputMsg from a raw ClientEnvelope payload.
// Returns ok=false if the payload is malformed.
func ParseInputPaddleTarget(rawPayload json.RawMessage) (matchID string, targetX float64, seq int, ok bool) {
	var p inputPaddleTargetPayload
	if err := json.Unmarshal(rawPayload, &p); err != nil {
		return "", 0, 0, false
	}
	return p.MatchID, p.TargetX, p.ClientSeq, true
}

// reconnectPayload is used by the handler to parse match.reconnect.
type reconnectPayload struct {
	SessionToken string `json:"sessionToken"`
	MatchID      string `json:"matchId"`
}

// ParseReconnectPayload extracts sessionToken and matchID.
func ParseReconnectPayload(rawPayload json.RawMessage) (sessionToken, matchID string, ok bool) {
	var p reconnectPayload
	if err := json.Unmarshal(rawPayload, &p); err != nil {
		return "", "", false
	}
	return p.SessionToken, p.MatchID, true
}
```

- [ ] **Step 4: Adjust test file to match the actual API**

The test uses `run.reconnectCh <- 1` directly and calls `matchLoopActive`. Verify these are accessible (they are: `reconnectCh` is exported as-is inside `match_run.go` since the test is in package `matchmgr`, not `matchmgr_test`). Change the test file package line from `matchmgr_test` to `matchmgr` so it can access unexported fields.

Update the first line of `match_run_test.go`:
```go
package matchmgr
```

Also fix the `mockSender` type — it conflicts with lobby.Sender; rename to `testSender` in the test file.

- [ ] **Step 5: Run tests**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
cd D:\Pong-Mobile\backend
go test ./internal/matchmgr/... -v -timeout 60s 2>&1
```

Expected: all snapshot tests pass (4) + match_run tests (6). Some tests are timing-dependent — `TestMatchLoop_SendsCountdownAndStarted` takes 4s. Accept ±1 test flake on snapshot count.

If `TestMatchLoop_DisconnectPausesLoop` is flaky, adjust sleep durations. The criterion is: snapshot count stops growing after disconnect is set, not a hard number.

- [ ] **Step 6: Commit**

```powershell
git add backend/internal/matchmgr/
git commit -m "feat(m4): MatchRun tick loop, disconnect/reconnect, unit tests"
```

---

### Task 3: matchmgr.Manager + handler wiring + main.go + integration test

**Files:**
- Create: `backend/internal/matchmgr/manager.go`
- Modify: `backend/internal/wsconn/handler.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/internal/wsconn/handler_test.go` (add integration test)

**Interfaces:**
- Consumes: `MatchRun`, `newMatchRun`, `matchLoop`, `ParseInputPaddleTarget`, `ParseReconnectPayload` from Task 2; `lobby.Room`, `lobby.Sender`; `auth.Store.GetByToken`
- Produces:
  - `matchmgr.NewManager(onEnd func(*MatchRun)) *Manager`
  - `(*Manager).StartMatch(room *lobby.Room)`
  - `(*Manager).HandleInput(connID string, env protocol.ClientEnvelope)`
  - `(*Manager).HandleReconnect(connID string, sess *auth.Session, sender lobby.Sender, rawPayload json.RawMessage)`
  - `(*Manager).OnDisconnect(connID string)`

- [ ] **Step 1: Create manager.go**

Create `backend/internal/matchmgr/manager.go`:

```go
package matchmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/lobby"
	"github.com/pong-mobile/backend/internal/protocol"
)

// Manager tracks all active matches and routes messages to them.
type Manager struct {
	runs   sync.Map // matchID → *MatchRun
	byConn sync.Map // connID → *MatchRun
	onEnd  func(*MatchRun)
}

// NewManager creates a Manager. onEnd is called when a match finishes (may be nil).
func NewManager(onEnd func(*MatchRun)) *Manager {
	return &Manager{onEnd: onEnd}
}

// StartMatch creates a MatchRun from a lobby.Room and starts the tick goroutine.
// Called by lobby.Manager.onStart — room is in RoomStatusStarting.
func (m *Manager) StartMatch(room *lobby.Room) {
	matchID := fmt.Sprintf("match_%s", room.Code)
	players := [2]PlayerConn{
		{Sender: room.Players[0].Conn, ConnID: room.Players[0].Conn.ID()},
		{Sender: room.Players[1].Conn, ConnID: room.Players[1].Conn.ID()},
	}
	run := newMatchRun(matchID, room.Settings, players, func(r *MatchRun) {
		m.runs.Delete(r.MatchID)
		m.byConn.Delete(players[0].ConnID)
		m.byConn.Delete(players[1].ConnID)
		if m.onEnd != nil {
			m.onEnd(r)
		}
	})
	m.runs.Store(matchID, run)
	m.byConn.Store(players[0].ConnID, run)
	m.byConn.Store(players[1].ConnID, run)

	ctx, cancel := context.WithCancel(context.Background())
	run.cancelFn = cancel
	go matchLoop(ctx, run)
	log.Printf("match started: %s (room %s)", matchID, room.Code)
}

// HandleInput routes an input.paddle_target message to the correct match.
func (m *Manager) HandleInput(connID string, env protocol.ClientEnvelope) {
	v, ok := m.byConn.Load(connID)
	if !ok {
		return // not in a match — ignore silently
	}
	run := v.(*MatchRun)

	matchIDFromPayload, targetX, seq, ok := ParseInputPaddleTarget(env.Payload)
	if !ok {
		return
	}
	if matchIDFromPayload != run.MatchID {
		return
	}

	// Determine slot
	slot := -1
	run.mu.Lock()
	for i, p := range run.Players {
		if p.ConnID == connID {
			slot = i
			break
		}
	}
	run.mu.Unlock()
	if slot < 0 {
		return
	}

	select {
	case run.InputCh <- InputMsg{Slot: slot, TargetX: targetX, Seq: seq}:
	default:
		// channel full — drop oldest by draining one
		select {
		case <-run.InputCh:
		default:
		}
		run.InputCh <- InputMsg{Slot: slot, TargetX: targetX, Seq: seq}
	}
}

// HandleReconnect processes a match.reconnect message.
func (m *Manager) HandleReconnect(connID string, sess *auth.Session, sender lobby.Sender, rawPayload json.RawMessage) {
	_, matchID, ok := ParseReconnectPayload(rawPayload)
	if !ok {
		return
	}
	v, ok := m.runs.Load(matchID)
	if !ok {
		return
	}
	run := v.(*MatchRun)

	// Find which slot this session belongs to
	slot := -1
	run.mu.Lock()
	for i, p := range run.Players {
		if p.ConnID == connID || (sess != nil && run.State.Players[i].Connected) {
			// Match by original connID or by session (reconnect uses new connID)
			_ = p
		}
	}
	// Try by session ID stored during original connection
	// Since we don't store sessionID in PlayerConn yet, match by old connID first
	for i, p := range run.Players {
		if p.ConnID == connID {
			slot = i
			break
		}
	}
	run.mu.Unlock()

	if slot < 0 {
		// New connID — find disconnected slot
		run.mu.Lock()
		for i, p := range run.Players {
			if p.Sender == nil {
				slot = i
				// Reattach
				run.Players[i].Sender = sender
				run.Players[i].ConnID = connID
				run.Players[i].ReconnectDeadline = zeroTime
				break
			}
		}
		run.mu.Unlock()
	} else {
		run.mu.Lock()
		run.Players[slot].Sender = sender
		run.Players[slot].ConnID = connID
		run.Players[slot].ReconnectDeadline = zeroTime
		run.mu.Unlock()
	}

	if slot < 0 {
		return
	}

	m.byConn.Store(connID, run)
	// Signal the tick loop
	select {
	case run.reconnectCh <- slot:
	default:
	}
}

// OnDisconnect is called when a WebSocket connection closes.
func (m *Manager) OnDisconnect(connID string) {
	v, ok := m.byConn.Load(connID)
	if !ok {
		return
	}
	run := v.(*MatchRun)
	run.mu.Lock()
	for i, p := range run.Players {
		if p.ConnID == connID {
			run.Players[i].Sender = nil
			break
		}
	}
	run.mu.Unlock()
	// Tick loop will detect nil sender on next tick
}

var zeroTime = func() (t interface{}) { return }
```

Note: `zeroTime` at the end is wrong syntax. Replace with:
```go
var zeroTime = time.Time{}
```
Add `"time"` to the imports.

- [ ] **Step 2: Fix manager.go syntax issues**

The `HandleReconnect` function has overly complex slot-matching logic. Simplify: add a `SessionID string` field to `PlayerConn` (populated from `room.Players[slot].Session.ID` in `StartMatch`). Then match by session ID on reconnect.

Update `PlayerConn` in `match_run.go`:
```go
type PlayerConn struct {
	Sender            lobby.Sender
	ConnID            string
	SessionID         string    // auth.Session.ID for reconnect matching
	ReconnectDeadline time.Time
}
```

Update `StartMatch` in `manager.go`:
```go
players := [2]PlayerConn{
	{Sender: room.Players[0].Conn, ConnID: room.Players[0].Conn.ID(), SessionID: room.Players[0].Session.ID},
	{Sender: room.Players[1].Conn, ConnID: room.Players[1].Conn.ID(), SessionID: room.Players[1].Session.ID},
}
```

Update `HandleReconnect` slot-finding logic:
```go
// Find slot by session ID
slot := -1
run.mu.Lock()
for i, p := range run.Players {
	if sess != nil && p.SessionID == sess.ID {
		slot = i
		run.Players[i].Sender = sender
		run.Players[i].ConnID = connID
		run.Players[i].ReconnectDeadline = time.Time{}
		break
	}
}
run.mu.Unlock()
if slot < 0 {
	return
}
m.byConn.Store(connID, run)
select {
case run.reconnectCh <- slot:
default:
}
```

Also check `auth.Session` — it has an `ID` field. Check `backend/internal/auth/`:

- [ ] **Step 3: Check auth.Session fields**

Read `backend/internal/auth/store.go` (or equivalent) to confirm `Session.ID` field name. If it's `SessionID` or `Token`, adjust accordingly.

```powershell
Get-Content D:\Pong-Mobile\backend\internal\auth\*.go | Select-String -Pattern "type Session"
```

Update the `SessionID` field in PlayerConn and manager.go to use the correct field name.

- [ ] **Step 4: Verify matchmgr compiles**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
cd D:\Pong-Mobile\backend
go build ./internal/matchmgr/... 2>&1
```

Fix any compile errors before proceeding.

- [ ] **Step 5: Update wsconn/handler.go**

Read the current `handler.go`. Add `matchMgr *matchmgr.Manager` parameter to `Handler` and `makeDispatcher`. Add two new dispatch cases and update the cleanup callback.

Current cleanup:
```go
c.readLoop(makeDispatcher(sessions, mgr), func() { mgr.OnDisconnect(c.id) })
```

New cleanup:
```go
c.readLoop(makeDispatcher(sessions, mgr, matchMgr), func() {
	mgr.OnDisconnect(c.id)
	matchMgr.OnDisconnect(c.id)
})
```

New Handler signature:
```go
func Handler(sessions *auth.Store, mgr *lobby.Manager, matchMgr *matchmgr.Manager) http.Handler
```

New dispatcher cases (add after the `TypeRoomLeave` case):

```go
case protocol.TypeInputPaddleTarget:
	if c.session == nil {
		c.SendBytes(protocol.MakeError("unauthorized", "Session required.", env.RequestID))
		return
	}
	matchMgr.HandleInput(c.id, env)

case protocol.TypeMatchReconnect:
	if c.session == nil {
		c.SendBytes(protocol.MakeError("unauthorized", "Session required.", env.RequestID))
		return
	}
	matchMgr.HandleReconnect(c.id, c.session, c, env.Payload)
```

Import `matchmgr` package:
```go
"github.com/pong-mobile/backend/internal/matchmgr"
```

- [ ] **Step 6: Update cmd/server/main.go**

Replace the M3 stub onStart with real matchmgr:

```go
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/lobby"
	"github.com/pong-mobile/backend/internal/matchmgr"
	"github.com/pong-mobile/backend/internal/wsconn"
)

func main() {
	sessions := auth.NewStore()
	matchMgr := matchmgr.NewManager(nil)
	lobbyMgr := lobby.NewManager(func(room *lobby.Room) {
		matchMgr.StartMatch(room)
	})
	mux := http.NewServeMux()
	mux.Handle("/ws", wsconn.Handler(sessions, lobbyMgr, matchMgr))
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("volley server listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
```

- [ ] **Step 7: Run full test suite**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
cd D:\Pong-Mobile\backend
go test ./... -timeout 120s 2>&1
```

Expected: all packages pass (auth, lobby, match, matchmgr, physics, protocol, wsconn).

- [ ] **Step 8: Add integration test**

Add to `backend/internal/wsconn/handler_test.go`:

```go
func TestHandler_FullMatch_TwoClients(t *testing.T) {
	// This test runs a full match between two mock clients.
	// Ball is pre-positioned to score immediately after countdown.
	sessions := auth.NewStore()
	matchMgr := matchmgr.NewManager(nil)
	lobbyMgr := lobby.NewManager(func(room *lobby.Room) {
		run := matchmgr.StartMatchForTest(room) // test entry: pre-positions ball
		_ = run
	})
	handler := wsconn.Handler(sessions, lobbyMgr, matchMgr)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	// Helper: connect and do hello handshake, return conn + sessionId
	connect := func(name string) (*websocket.Conn, string) {
		c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("%s: dial: %v", name, err)
		}
		helloPayload := map[string]interface{}{
			"clientVersion": "0.1.0",
			"platform":      "test",
			"displayName":   name,
		}
		sendJSON(t, c, "client.hello", helloPayload)
		raw := readMsg(t, c)
		var env protocol.ServerEnvelope
		_ = json.Unmarshal(raw, &env)
		if env.Type != protocol.TypeServerHello {
			t.Fatalf("%s: expected server.hello, got %s", name, env.Type)
		}
		return c, ""
	}

	c1, _ := connect("Player1")
	defer c1.Close()
	c2, _ := connect("Player2")
	defer c2.Close()

	// P1 creates room
	sendJSON(t, c1, "room.create", map[string]interface{}{
		"settings": map[string]interface{}{"pointsToWin": 1, "paddleSpeed": 0.9, "ballSpeed": 0.55},
	})
	raw := readMsg(t, c1)
	var created struct {
		Payload struct{ RoomCode string `json:"roomCode"` } `json:"payload"`
	}
	json.Unmarshal(raw, &created)
	code := created.Payload.RoomCode
	if len(code) != 6 {
		t.Fatalf("invalid room code: %q", code)
	}

	// P2 joins
	sendJSON(t, c2, "room.join", map[string]interface{}{"roomCode": code})
	readMsg(t, c2) // room.updated

	// Both ready
	sendJSON(t, c1, "room.ready", map[string]interface{}{"ready": true})
	readMsg(t, c1) // room.updated
	sendJSON(t, c2, "room.ready", map[string]interface{}{"ready": true})

	// Expect countdown then started
	waitForType(t, c1, protocol.TypeMatchCountdown, 5*time.Second)
	waitForType(t, c1, protocol.TypeMatchStarted, 5*time.Second)
	waitForType(t, c2, protocol.TypeMatchStarted, 5*time.Second)

	// Expect at least one snapshot
	waitForType(t, c1, protocol.TypeMatchSnapshot, 10*time.Second)
}
```

Add helpers `sendJSON`, `readMsg`, `waitForType` to the test file (or reuse existing helpers).

Note: The full-match integration test is complex. If the `lobby.NewManager.onStart` stub approach is too difficult to wire with a ball pre-position, simplify: just test that after both clients ready-up, they both receive `match.countdown` and `match.started`. That validates the M4 wiring without requiring a simulated score.

Simplest acceptable integration test:
```go
// After both ready, P1 receives match.countdown within 1s
waitForType(t, c1, protocol.TypeMatchCountdown, 2*time.Second)
// After 3s countdown, P1 receives match.started
waitForType(t, c1, protocol.TypeMatchStarted, 5*time.Second)
// And at least one snapshot arrives
waitForType(t, c1, protocol.TypeMatchSnapshot, 3*time.Second)
```

This test takes ~3s total (countdown duration). Use `t.Parallel()` or accept the runtime.

- [ ] **Step 9: Run full suite including integration test**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
cd D:\Pong-Mobile\backend
go test ./... -timeout 120s -v 2>&1 | tail -30
```

Expected: all packages pass.

- [ ] **Step 10: Build server**

```powershell
$env:PATH = "C:\Users\trent\sdk\go\bin;$env:PATH"
cd D:\Pong-Mobile\backend
go build ./cmd/server 2>&1
```

Expected: clean compile.

- [ ] **Step 11: Commit**

```powershell
git add backend/internal/matchmgr/manager.go backend/internal/wsconn/handler.go backend/cmd/server/main.go backend/internal/wsconn/handler_test.go
git commit -m "feat(m4): matchmgr.Manager, handler input/reconnect wiring, integration test"
```

---

## Post-M4 Notes

Before M4 merge:
- Address the `onStart` TOCTOU noted in M3 final review: `StartMatch` snapshots room player data (already done via `players [2]PlayerConn` copy) — this is the correct fix.
- `go test -race ./...` requires CGO on Windows. Recommend verifying in CI/Linux environment.
- The Flutter client cannot be tested until M6 (Fly.io deploy). M5 (client smoothing) can begin locally using `localhost:8080` once M4 is verified working.
