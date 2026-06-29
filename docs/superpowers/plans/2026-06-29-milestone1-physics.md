# Milestone 1: Physics Prototype Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Scaffold the monorepo, initialize the Go backend module, and implement a fully-tested server-authoritative physics simulation (ball movement, wall/paddle collision, bounce angles, scoring, tick loop) with no network layer.

**Architecture:** The `internal/physics` package owns pure stateless simulation functions (move ball, collide, bounce). The `internal/match` package owns `MatchState` and the tick loop that calls physics functions each frame. All constants come from a single `internal/config` package so they're never duplicated.

**Tech Stack:** Go 1.22+, standard library only (`math`, `testing`). No external dependencies for Milestone 1.

## Global Constraints

- Go module path: `github.com/pong-mobile/backend`
- Normalized coordinates: all positions/velocities in `[0.0, 1.0]` space
- Server tick rate: 30 ticks/second (`dt = 1.0/30.0`)
- All physics is server-side only — no client code in this milestone
- No network, no database, no WebSocket code

---

## File Map

| File | Purpose |
|---|---|
| `backend/go.mod` | Module definition |
| `backend/cmd/server/main.go` | Stub entry point |
| `backend/internal/config/config.go` | All game constants |
| `backend/internal/physics/physics.go` | Pure simulation functions |
| `backend/internal/physics/physics_test.go` | Physics unit tests |
| `backend/internal/match/state.go` | MatchState, BallState, PlayerState types |
| `backend/internal/match/simulation.go` | Tick loop, rally reset, score detection |
| `backend/internal/match/simulation_test.go` | Simulation integration tests |
| `.gitignore` | Git ignore for Go and Flutter |

---

### Task 1: Repo scaffold + Go module init

**Files:**
- Create: `.gitignore`
- Create: `backend/go.mod`
- Create: `backend/cmd/server/main.go`

**Interfaces:**
- Produces: compilable Go module at `github.com/pong-mobile/backend`

- [ ] **Step 1: Create `.gitignore`**

Create `D:\Pong-Mobile\.gitignore` with this content:

```gitignore
# Go
backend/bin/
*.exe
*.test

# Flutter
client/.dart_tool/
client/.flutter-plugins
client/.flutter-plugins-dependencies
client/build/
client/.packages
client/pubspec.lock

# IDE
.idea/
.vscode/
*.iml

# OS
.DS_Store
Thumbs.db
```

- [ ] **Step 2: Create `backend/go.mod`**

Create `D:\Pong-Mobile\backend\go.mod`:

```go
module github.com/pong-mobile/backend

go 1.22
```

- [ ] **Step 3: Create stub `main.go`**

Create `D:\Pong-Mobile\backend\cmd\server\main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("pong-mobile server starting")
}
```

- [ ] **Step 4: Verify it compiles**

```
cd D:\Pong-Mobile\backend
go build ./...
```

Expected: no output, exit code 0.

- [ ] **Step 5: Commit**

```
cd D:\Pong-Mobile
git init
git add .
git commit -m "chore: scaffold monorepo and Go module"
```

---

### Task 2: Config package

**Files:**
- Create: `backend/internal/config/config.go`

**Interfaces:**
- Produces: `config.Default` of type `config.Settings` with all game constants

- [ ] **Step 1: Create `config.go`**

Create `D:\Pong-Mobile\backend\internal\config\config.go`:

```go
package config

import "math"

// Settings holds all tunable game constants.
type Settings struct {
	PointsToWin      int
	PaddleWidth      float64
	PaddleHeight     float64
	PaddleSpeed      float64 // normalized units/second
	BallRadius       float64
	BallSpeed        float64 // normalized units/second
	CountdownMs      int
	RallyResetMs     int
	ReconnectWindowMs int
	MaxBounceAngle   float64 // radians
	Player1PaddleY   float64
	Player2PaddleY   float64
	TickRate         int
	SnapshotRate     int
	DT               float64 // seconds per tick
	InterpolationDelayMs   int
	MaxExtrapolationMs     int
}

// Default is the starting configuration per GAME_LOOP_SPEC.md.
var Default = Settings{
	PointsToWin:           5,
	PaddleWidth:           0.22,
	PaddleHeight:          0.025,
	PaddleSpeed:           0.9,
	BallRadius:            0.018,
	BallSpeed:             0.55,
	CountdownMs:           3000,
	RallyResetMs:          1500,
	ReconnectWindowMs:     10000,
	MaxBounceAngle:        60.0 * math.Pi / 180.0,
	Player1PaddleY:        0.93,
	Player2PaddleY:        0.07,
	TickRate:              30,
	SnapshotRate:          20,
	DT:                    1.0 / 30.0,
	InterpolationDelayMs:  100,
	MaxExtrapolationMs:    150,
}
```

- [ ] **Step 2: Verify compiles**

```
cd D:\Pong-Mobile\backend
go build ./...
```

Expected: no output, exit code 0.

- [ ] **Step 3: Commit**

```
cd D:\Pong-Mobile\backend
git add internal/config/config.go
git commit -m "feat: add config package with game constants"
```

---

### Task 3: Match state types

**Files:**
- Create: `backend/internal/match/state.go`

**Interfaces:**
- Produces:
  - `match.BallState{X, Y, VX, VY float64}`
  - `match.PlayerState{PaddleX, TargetX float64; Score int; Connected bool; LastInputSeq int}`
  - `match.MatchState{MatchID string; Status match.Status; ServerTick int64; Ball match.BallState; Players [2]match.PlayerState; Settings config.Settings}`
  - `match.Status` constants: `StatusWaiting`, `StatusCountdown`, `StatusActive`, `StatusEnded`

- [ ] **Step 1: Create `state.go`**

Create `D:\Pong-Mobile\backend\internal\match\state.go`:

```go
package match

import "github.com/pong-mobile/backend/internal/config"

// Status represents the current phase of a match.
type Status string

const (
	StatusWaiting   Status = "waiting"
	StatusCountdown Status = "countdown"
	StatusActive    Status = "active"
	StatusEnded     Status = "ended"
)

// BallState holds the authoritative ball position and velocity.
// All values are in normalized [0,1] coordinates.
// VX and VY form a unit vector; speed is stored separately in Settings.
type BallState struct {
	X  float64
	Y  float64
	VX float64
	VY float64
}

// PlayerState holds per-player paddle state.
// Slot 0 = Player 1 (bottom, paddleY=0.93), Slot 1 = Player 2 (top, paddleY=0.07).
type PlayerState struct {
	PaddleX      float64
	TargetX      float64
	Score        int
	Connected    bool
	LastInputSeq int
}

// MatchState is the complete authoritative server-side match state.
type MatchState struct {
	MatchID    string
	Status     Status
	ServerTick int64
	Ball       BallState
	Players    [2]PlayerState
	Settings   config.Settings
}

// NewMatchState initializes a match with ball at center, paddles centered.
func NewMatchState(matchID string, settings config.Settings) MatchState {
	return MatchState{
		MatchID:  matchID,
		Status:   StatusWaiting,
		Settings: settings,
		Ball: BallState{
			X:  0.5,
			Y:  0.5,
			VX: 0.0,
			VY: 1.0,
		},
		Players: [2]PlayerState{
			{PaddleX: 0.5, TargetX: 0.5, Connected: true},
			{PaddleX: 0.5, TargetX: 0.5, Connected: true},
		},
	}
}
```

- [ ] **Step 2: Verify compiles**

```
cd D:\Pong-Mobile\backend
go build ./...
```

Expected: no output, exit code 0.

- [ ] **Step 3: Commit**

```
cd D:\Pong-Mobile\backend
git add internal/match/state.go
git commit -m "feat: add match state types"
```

---

### Task 4: Physics package — write failing tests first

**Files:**
- Create: `backend/internal/physics/physics_test.go`
- Create: `backend/internal/physics/physics.go`

**Interfaces:**
- Produces:
  - `physics.MoveBall(ball match.BallState, speed, dt float64) match.BallState`
  - `physics.WallCollide(ball match.BallState, radius float64) match.BallState`
  - `physics.MovePaddle(paddleX, targetX, speed, dt float64, paddleWidth float64) float64`
  - `physics.PaddleCollide(ball match.BallState, paddleX, paddleY, paddleWidth, paddleHeight, radius, maxBounceAngle float64, isPlayer1 bool) (match.BallState, bool)`
  - `physics.CheckScore(ball match.BallState, radius float64) (scored bool, slot int)` — slot 0 = P1 scored, slot 1 = P2 scored

- [ ] **Step 1: Write failing tests**

Create `D:\Pong-Mobile\backend\internal\physics\physics_test.go`:

```go
package physics_test

import (
	"math"
	"testing"

	"github.com/pong-mobile/backend/internal/match"
	"github.com/pong-mobile/backend/internal/physics"
)

const eps = 1e-9

func TestMoveBall_MovesAlongVelocity(t *testing.T) {
	ball := match.BallState{X: 0.5, Y: 0.5, VX: 1.0, VY: 0.0}
	result := physics.MoveBall(ball, 0.55, 1.0/30.0)
	want := 0.5 + 0.55*(1.0/30.0)
	if math.Abs(result.X-want) > eps {
		t.Errorf("X: got %v, want %v", result.X, want)
	}
	if math.Abs(result.Y-0.5) > eps {
		t.Errorf("Y should not change: got %v", result.Y)
	}
}

func TestWallCollide_LeftWall(t *testing.T) {
	radius := 0.018
	ball := match.BallState{X: radius - 0.001, Y: 0.5, VX: -0.8, VY: 0.6}
	result := physics.WallCollide(ball, radius)
	if result.X < radius {
		t.Errorf("ball clipped left wall: X=%v, radius=%v", result.X, radius)
	}
	if result.VX <= 0 {
		t.Errorf("VX should be positive after left wall bounce, got %v", result.VX)
	}
}

func TestWallCollide_RightWall(t *testing.T) {
	radius := 0.018
	ball := match.BallState{X: 1.0 - radius + 0.001, Y: 0.5, VX: 0.8, VY: 0.6}
	result := physics.WallCollide(ball, radius)
	if result.X > 1.0-radius {
		t.Errorf("ball clipped right wall: X=%v", result.X)
	}
	if result.VX >= 0 {
		t.Errorf("VX should be negative after right wall bounce, got %v", result.VX)
	}
}

func TestWallCollide_NoCollision(t *testing.T) {
	ball := match.BallState{X: 0.5, Y: 0.5, VX: 0.8, VY: 0.6}
	result := physics.WallCollide(ball, 0.018)
	if result != ball {
		t.Errorf("ball should be unchanged when no wall collision")
	}
}

func TestMovePaddle_MovesTowardTarget(t *testing.T) {
	// paddle at 0.5, target at 0.7, speed 0.9, dt 1/30
	// maxStep = 0.9/30 = 0.03 — should move exactly maxStep
	got := physics.MovePaddle(0.5, 0.7, 0.9, 1.0/30.0, 0.22)
	want := 0.5 + 0.03
	if math.Abs(got-want) > eps {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMovePaddle_ReachesTarget(t *testing.T) {
	// paddle at 0.5, target at 0.501 — gap smaller than maxStep
	got := physics.MovePaddle(0.5, 0.501, 0.9, 1.0/30.0, 0.22)
	if math.Abs(got-0.501) > eps {
		t.Errorf("should snap to target: got %v", got)
	}
}

func TestMovePaddle_ClampedAtBounds(t *testing.T) {
	paddleWidth := 0.22
	minX := paddleWidth / 2
	maxX := 1.0 - paddleWidth / 2
	// Try to push past right boundary
	got := physics.MovePaddle(maxX, 1.0, 0.9, 1.0/30.0, paddleWidth)
	if got > maxX+eps {
		t.Errorf("paddle exceeded right bound: %v > %v", got, maxX)
	}
	// Try to push past left boundary
	got = physics.MovePaddle(minX, 0.0, 0.9, 1.0/30.0, paddleWidth)
	if got < minX-eps {
		t.Errorf("paddle exceeded left bound: %v < %v", got, minX)
	}
}

func TestPaddleCollide_Player1HitCenter(t *testing.T) {
	// Player 1 is at bottom (paddleY=0.93), ball moving down (VY > 0)
	radius := 0.018
	paddleX := 0.5
	paddleY := 0.93
	paddleW := 0.22
	paddleH := 0.025
	maxAngle := 60.0 * math.Pi / 180.0

	ball := match.BallState{
		X: 0.5, Y: paddleY - paddleH/2 - radius + 0.001,
		VX: 0.0, VY: 1.0,
	}
	result, hit := physics.PaddleCollide(ball, paddleX, paddleY, paddleW, paddleH, radius, maxAngle, true)
	if !hit {
		t.Fatal("expected collision with player 1 paddle")
	}
	if result.VY >= 0 {
		t.Errorf("ball should bounce upward after P1 hit: VY=%v", result.VY)
	}
	// Velocity should remain unit length
	length := math.Sqrt(result.VX*result.VX + result.VY*result.VY)
	if math.Abs(length-1.0) > 1e-6 {
		t.Errorf("velocity not normalized after collision: length=%v", length)
	}
}

func TestPaddleCollide_Player2HitCenter(t *testing.T) {
	// Player 2 is at top (paddleY=0.07), ball moving up (VY < 0)
	radius := 0.018
	paddleX := 0.5
	paddleY := 0.07
	paddleW := 0.22
	paddleH := 0.025
	maxAngle := 60.0 * math.Pi / 180.0

	ball := match.BallState{
		X: 0.5, Y: paddleY + paddleH/2 + radius - 0.001,
		VX: 0.0, VY: -1.0,
	}
	result, hit := physics.PaddleCollide(ball, paddleX, paddleY, paddleW, paddleH, radius, maxAngle, false)
	if !hit {
		t.Fatal("expected collision with player 2 paddle")
	}
	if result.VY <= 0 {
		t.Errorf("ball should bounce downward after P2 hit: VY=%v", result.VY)
	}
}

func TestPaddleCollide_NoCollision_WrongDirection(t *testing.T) {
	// Ball moving away from P1 paddle (VY < 0) — should not collide
	radius := 0.018
	ball := match.BallState{X: 0.5, Y: 0.93, VX: 0.0, VY: -1.0}
	_, hit := physics.PaddleCollide(ball, 0.5, 0.93, 0.22, 0.025, radius, 60*math.Pi/180, true)
	if hit {
		t.Error("ball moving away from P1 should not trigger collision")
	}
}

func TestCheckScore_Player1Scores(t *testing.T) {
	// Ball exits top boundary (y < 0) → P2 missed → P1 scores (slot 0)
	ball := match.BallState{X: 0.5, Y: -0.019, VX: 0.0, VY: -1.0}
	scored, slot := physics.CheckScore(ball, 0.018)
	if !scored {
		t.Fatal("expected score")
	}
	if slot != 0 {
		t.Errorf("expected slot 0 (P1 scores), got %d", slot)
	}
}

func TestCheckScore_Player2Scores(t *testing.T) {
	// Ball exits bottom boundary (y > 1) → P1 missed → P2 scores (slot 1)
	ball := match.BallState{X: 0.5, Y: 1.019, VX: 0.0, VY: 1.0}
	scored, slot := physics.CheckScore(ball, 0.018)
	if !scored {
		t.Fatal("expected score")
	}
	if slot != 1 {
		t.Errorf("expected slot 1 (P2 scores), got %d", slot)
	}
}

func TestCheckScore_NoScore(t *testing.T) {
	ball := match.BallState{X: 0.5, Y: 0.5, VX: 0.0, VY: 1.0}
	scored, _ := physics.CheckScore(ball, 0.018)
	if scored {
		t.Error("should not score when ball is in play")
	}
}
```

- [ ] **Step 2: Run tests — expect compile failure**

```
cd D:\Pong-Mobile\backend
go test ./internal/physics/...
```

Expected: `cannot find package "github.com/pong-mobile/backend/internal/physics"` or similar. This is correct — we haven't written the implementation yet.

- [ ] **Step 3: Write minimal implementation**

Create `D:\Pong-Mobile\backend\internal\physics\physics.go`:

```go
package physics

import (
	"math"

	"github.com/pong-mobile/backend/internal/match"
)

// MoveBall advances the ball position one tick.
// speed is in normalized units/second. dt is seconds per tick.
func MoveBall(ball match.BallState, speed, dt float64) match.BallState {
	ball.X += ball.VX * speed * dt
	ball.Y += ball.VY * speed * dt
	return ball
}

// WallCollide bounces the ball off the left and right walls.
// Adjusts position so the ball does not clip through the wall.
func WallCollide(ball match.BallState, radius float64) match.BallState {
	if ball.X-radius <= 0 {
		ball.X = radius
		ball.VX = math.Abs(ball.VX)
	}
	if ball.X+radius >= 1.0 {
		ball.X = 1.0 - radius
		ball.VX = -math.Abs(ball.VX)
	}
	return ball
}

// MovePaddle moves paddleX toward targetX at most paddleSpeed*dt units per tick.
// Clamps the result within paddle bounds.
func MovePaddle(paddleX, targetX, paddleSpeed, dt, paddleWidth float64) float64 {
	minX := paddleWidth / 2
	maxX := 1.0 - paddleWidth/2

	delta := targetX - paddleX
	maxStep := paddleSpeed * dt
	if math.Abs(delta) <= maxStep {
		paddleX = targetX
	} else {
		paddleX += math.Copysign(maxStep, delta)
	}
	return math.Max(minX, math.Min(maxX, paddleX))
}

// PaddleCollide checks whether the ball intersects the given paddle and,
// if so, computes the bounce velocity based on hit position.
//
// isPlayer1=true means the paddle is at the bottom (paddleY=0.93); the ball
// must be moving downward (VY > 0) to register a hit.
// isPlayer1=false means the paddle is at the top (paddleY=0.07); the ball
// must be moving upward (VY < 0) to register a hit.
//
// Returns the updated BallState and whether a collision occurred.
func PaddleCollide(
	ball match.BallState,
	paddleX, paddleY, paddleWidth, paddleHeight, radius, maxBounceAngle float64,
	isPlayer1 bool,
) (match.BallState, bool) {
	// Direction guard: only check collision when ball is approaching the paddle.
	if isPlayer1 && ball.VY <= 0 {
		return ball, false
	}
	if !isPlayer1 && ball.VY >= 0 {
		return ball, false
	}

	left := paddleX - paddleWidth/2
	right := paddleX + paddleWidth/2
	top := paddleY - paddleHeight/2
	bottom := paddleY + paddleHeight/2

	// AABB vs circle overlap check
	if ball.X+radius < left || ball.X-radius > right {
		return ball, false
	}
	if ball.Y+radius < top || ball.Y-radius > bottom {
		return ball, false
	}

	// Relative hit position: -1.0 (left edge) to +1.0 (right edge)
	rel := (ball.X - paddleX) / (paddleWidth / 2)
	rel = math.Max(-1.0, math.Min(1.0, rel))

	var angle float64
	if isPlayer1 {
		// Bounce upward: base angle -90° (straight up), offset by rel
		angle = -math.Pi/2 + rel*maxBounceAngle
	} else {
		// Bounce downward: base angle +90° (straight down), offset by -rel
		angle = math.Pi/2 - rel*maxBounceAngle
	}

	ball.VX = math.Cos(angle)
	ball.VY = math.Sin(angle)

	// Normalize (should already be unit length, but guard against float drift)
	length := math.Sqrt(ball.VX*ball.VX + ball.VY*ball.VY)
	ball.VX /= length
	ball.VY /= length

	return ball, true
}

// CheckScore returns whether the ball has fully crossed a scoring boundary,
// and which slot (0=P1, 1=P2) scored.
//
// P1 scores when the ball exits the top (y+radius < 0) — P2 missed.
// P2 scores when the ball exits the bottom (y-radius > 1) — P1 missed.
func CheckScore(ball match.BallState, radius float64) (bool, int) {
	if ball.Y+radius < 0 {
		return true, 0 // P1 scores
	}
	if ball.Y-radius > 1.0 {
		return true, 1 // P2 scores
	}
	return false, -1
}
```

- [ ] **Step 4: Run tests — expect all pass**

```
cd D:\Pong-Mobile\backend
go test ./internal/physics/... -v
```

Expected output (all PASS):
```
--- PASS: TestMoveBall_MovesAlongVelocity
--- PASS: TestWallCollide_LeftWall
--- PASS: TestWallCollide_RightWall
--- PASS: TestWallCollide_NoCollision
--- PASS: TestMovePaddle_MovesTowardTarget
--- PASS: TestMovePaddle_ReachesTarget
--- PASS: TestMovePaddle_ClampedAtBounds
--- PASS: TestPaddleCollide_Player1HitCenter
--- PASS: TestPaddleCollide_Player2HitCenter
--- PASS: TestPaddleCollide_NoCollision_WrongDirection
--- PASS: TestCheckScore_Player1Scores
--- PASS: TestCheckScore_Player2Scores
--- PASS: TestCheckScore_NoScore
PASS
```

- [ ] **Step 5: Commit**

```
cd D:\Pong-Mobile\backend
git add internal/physics/
git commit -m "feat: physics package — ball movement, wall/paddle collision, scoring"
```

---

### Task 5: Match simulation — tick loop, rally reset, match end

**Files:**
- Create: `backend/internal/match/simulation.go`
- Create: `backend/internal/match/simulation_test.go`

**Interfaces:**
- Consumes:
  - `physics.MoveBall(ball match.BallState, speed, dt float64) match.BallState`
  - `physics.WallCollide(ball match.BallState, radius float64) match.BallState`
  - `physics.MovePaddle(paddleX, targetX, paddleSpeed, dt, paddleWidth float64) float64`
  - `physics.PaddleCollide(ball match.BallState, paddleX, paddleY, paddleWidth, paddleHeight, radius, maxBounceAngle float64, isPlayer1 bool) (match.BallState, bool)`
  - `physics.CheckScore(ball match.BallState, radius float64) (bool, int)`
- Produces:
  - `match.Tick(state MatchState) (MatchState, TickResult)`
  - `match.TickResult{Scored bool; ScoringSlot int; MatchEnded bool; WinnerSlot int}`
  - `match.RallyReset(state MatchState, lastScoringSlot int) MatchState`
  - `match.SetPlayerTarget(state MatchState, slot int, targetX float64, seq int) MatchState`

- [ ] **Step 1: Write failing tests**

Create `D:\Pong-Mobile\backend\internal\match\simulation_test.go`:

```go
package match_test

import (
	"testing"

	"github.com/pong-mobile/backend/internal/config"
	"github.com/pong-mobile/backend/internal/match"
)

func newTestMatch() match.MatchState {
	s := config.Default
	m := match.NewMatchState("test-match", s)
	m.Status = match.StatusActive
	return m
}

func TestTick_AdvancesServerTick(t *testing.T) {
	m := newTestMatch()
	m.Ball = match.BallState{X: 0.5, Y: 0.5, VX: 0.0, VY: 1.0}
	result, _ := match.Tick(m)
	if result.ServerTick != 1 {
		t.Errorf("ServerTick should be 1, got %d", result.ServerTick)
	}
}

func TestTick_BallMoves(t *testing.T) {
	m := newTestMatch()
	m.Ball = match.BallState{X: 0.5, Y: 0.5, VX: 0.0, VY: 1.0}
	startY := m.Ball.Y
	result, _ := match.Tick(m)
	if result.Ball.Y <= startY {
		t.Errorf("ball Y should increase when VY=1.0, got %v", result.Ball.Y)
	}
}

func TestTick_DetectsScore(t *testing.T) {
	m := newTestMatch()
	// Ball just past bottom boundary — P2 missed — P1 scores (slot 0)
	m.Ball = match.BallState{X: 0.5, Y: 1.019, VX: 0.0, VY: 1.0}
	_, tr := match.Tick(m)
	if !tr.Scored {
		t.Fatal("expected score event")
	}
	if tr.ScoringSlot != 0 {
		t.Errorf("expected P1 (slot 0) to score, got slot %d", tr.ScoringSlot)
	}
}

func TestTick_DetectsMatchEnd(t *testing.T) {
	m := newTestMatch()
	m.Players[0].Score = config.Default.PointsToWin - 1
	// Ball just past bottom — P1 scores and wins
	m.Ball = match.BallState{X: 0.5, Y: 1.019, VX: 0.0, VY: 1.0}
	_, tr := match.Tick(m)
	if !tr.MatchEnded {
		t.Fatal("expected match to end when player reaches pointsToWin")
	}
	if tr.WinnerSlot != 0 {
		t.Errorf("expected winner slot 0, got %d", tr.WinnerSlot)
	}
}

func TestTick_InactiveMatchDoesNotAdvance(t *testing.T) {
	m := newTestMatch()
	m.Status = match.StatusWaiting
	m.Ball = match.BallState{X: 0.5, Y: 0.5, VX: 0.0, VY: 1.0}
	result, _ := match.Tick(m)
	if result.Ball.Y != m.Ball.Y {
		t.Error("ball should not move when match is not active")
	}
}

func TestRallyReset_BallAtCenter(t *testing.T) {
	m := newTestMatch()
	m.Ball = match.BallState{X: 0.1, Y: 0.9, VX: 0.0, VY: 0.0}
	result := match.RallyReset(m, 0)
	if result.Ball.X != 0.5 || result.Ball.Y != 0.5 {
		t.Errorf("ball should reset to center, got (%v, %v)", result.Ball.X, result.Ball.Y)
	}
}

func TestRallyReset_Player1ScoredBallLaunchesTowardP2(t *testing.T) {
	// P1 scored (slot 0) → ball launches toward P2 (top, VY < 0)
	m := newTestMatch()
	result := match.RallyReset(m, 0)
	if result.Ball.VY >= 0 {
		t.Errorf("after P1 scores, ball should launch toward P2 (VY<0), got %v", result.Ball.VY)
	}
}

func TestRallyReset_Player2ScoredBallLaunchesTowardP1(t *testing.T) {
	// P2 scored (slot 1) → ball launches toward P1 (bottom, VY > 0)
	m := newTestMatch()
	result := match.RallyReset(m, 1)
	if result.Ball.VY <= 0 {
		t.Errorf("after P2 scores, ball should launch toward P1 (VY>0), got %v", result.Ball.VY)
	}
}

func TestSetPlayerTarget_UpdatesTargetX(t *testing.T) {
	m := newTestMatch()
	result := match.SetPlayerTarget(m, 0, 0.7, 1)
	if result.Players[0].TargetX != 0.7 {
		t.Errorf("TargetX should be 0.7, got %v", result.Players[0].TargetX)
	}
	if result.Players[0].LastInputSeq != 1 {
		t.Errorf("LastInputSeq should be 1, got %v", result.Players[0].LastInputSeq)
	}
}

func TestSetPlayerTarget_IgnoresOldSeq(t *testing.T) {
	m := newTestMatch()
	m.Players[0].TargetX = 0.6
	m.Players[0].LastInputSeq = 5
	result := match.SetPlayerTarget(m, 0, 0.9, 3) // old seq
	if result.Players[0].TargetX != 0.6 {
		t.Error("old seq input should be ignored")
	}
}

func TestSetPlayerTarget_ClampedToValidRange(t *testing.T) {
	m := newTestMatch()
	result := match.SetPlayerTarget(m, 0, 1.5, 1) // out of range
	if result.Players[0].TargetX > 1.0 {
		t.Errorf("targetX should be clamped to [0,1], got %v", result.Players[0].TargetX)
	}
}
```

- [ ] **Step 2: Run tests — expect compile failure**

```
cd D:\Pong-Mobile\backend
go test ./internal/match/...
```

Expected: `undefined: match.Tick` or similar.

- [ ] **Step 3: Write simulation implementation**

Create `D:\Pong-Mobile\backend\internal\match\simulation.go`:

```go
package match

import (
	"math"
	"math/rand"

	"github.com/pong-mobile/backend/internal/physics"
)

// TickResult reports what happened during a single server tick.
type TickResult struct {
	Scored      bool
	ScoringSlot int // 0=P1 scored, 1=P2 scored
	MatchEnded  bool
	WinnerSlot  int
}

// Tick advances the match state by one server tick (dt = 1/tickRate seconds).
// Returns the updated state and a TickResult describing events that occurred.
// If the match is not StatusActive, the state is returned unchanged.
func Tick(state MatchState) (MatchState, TickResult) {
	if state.Status != StatusActive {
		return state, TickResult{}
	}

	cfg := state.Settings
	dt := cfg.DT

	// Move paddles toward their targets.
	for i := range state.Players {
		state.Players[i].PaddleX = physics.MovePaddle(
			state.Players[i].PaddleX,
			state.Players[i].TargetX,
			cfg.PaddleSpeed,
			dt,
			cfg.PaddleWidth,
		)
	}

	// Move ball.
	state.Ball = physics.MoveBall(state.Ball, cfg.BallSpeed, dt)

	// Wall collisions (left/right).
	state.Ball = physics.WallCollide(state.Ball, cfg.BallRadius)

	// Paddle collisions.
	// Player 1 is slot 0, bottom paddle (paddleY = Player1PaddleY).
	// Player 2 is slot 1, top paddle (paddleY = Player2PaddleY).
	newBall, _ := physics.PaddleCollide(
		state.Ball,
		state.Players[0].PaddleX, cfg.Player1PaddleY,
		cfg.PaddleWidth, cfg.PaddleHeight,
		cfg.BallRadius, cfg.MaxBounceAngle,
		true,
	)
	state.Ball = newBall

	newBall, _ = physics.PaddleCollide(
		state.Ball,
		state.Players[1].PaddleX, cfg.Player2PaddleY,
		cfg.PaddleWidth, cfg.PaddleHeight,
		cfg.BallRadius, cfg.MaxBounceAngle,
		false,
	)
	state.Ball = newBall

	// Score detection.
	var result TickResult
	if scored, slot := physics.CheckScore(state.Ball, cfg.BallRadius); scored {
		state.Players[slot].Score++
		result.Scored = true
		result.ScoringSlot = slot

		if state.Players[slot].Score >= cfg.PointsToWin {
			state.Status = StatusEnded
			result.MatchEnded = true
			result.WinnerSlot = slot
		}
	}

	state.ServerTick++
	return state, result
}

// RallyReset resets the ball to center and launches it toward the player
// who was just scored on. lastScoringSlot is 0 if P1 scored, 1 if P2 scored.
// A small random horizontal component is added using server-side randomness.
func RallyReset(state MatchState, lastScoringSlot int) MatchState {
	state.Ball.X = 0.5
	state.Ball.Y = 0.5

	// Launch toward the player who was scored on (they serve next).
	var vy float64
	if lastScoringSlot == 0 {
		// P1 scored → ball goes toward P2 (top) → VY negative
		vy = -1.0
	} else {
		// P2 scored → ball goes toward P1 (bottom) → VY positive
		vy = 1.0
	}

	// Random horizontal component in [-0.35, 0.35]
	vx := (rand.Float64()*2 - 1) * 0.35

	// Normalize
	length := math.Sqrt(vx*vx + vy*vy)
	state.Ball.VX = vx / length
	state.Ball.VY = vy / length

	return state
}

// SetPlayerTarget applies a client paddle input to the match state.
// Ignores inputs with seq <= lastInputSeq (old or duplicate).
// Clamps targetX to [0.0, 1.0].
func SetPlayerTarget(state MatchState, slot int, targetX float64, seq int) MatchState {
	if seq <= state.Players[slot].LastInputSeq {
		return state
	}
	targetX = math.Max(0.0, math.Min(1.0, targetX))
	state.Players[slot].TargetX = targetX
	state.Players[slot].LastInputSeq = seq
	return state
}
```

- [ ] **Step 4: Run all tests — expect all pass**

```
cd D:\Pong-Mobile\backend
go test ./... -v
```

Expected: all tests PASS across `physics` and `match` packages.

- [ ] **Step 5: Commit**

```
cd D:\Pong-Mobile\backend
git add internal/match/simulation.go internal/match/simulation_test.go
git commit -m "feat: match simulation — tick loop, rally reset, score detection, match end"
```

---

### Task 6: Smoke-test main

**Files:**
- Modify: `backend/cmd/server/main.go`

**Interfaces:**
- Consumes: `match.NewMatchState`, `match.Tick`, `match.RallyReset`, `match.SetPlayerTarget`

- [ ] **Step 1: Update main.go with smoke test loop**

Replace `D:\Pong-Mobile\backend\cmd\server\main.go`:

```go
package main

import (
	"fmt"

	"github.com/pong-mobile/backend/internal/config"
	"github.com/pong-mobile/backend/internal/match"
)

func main() {
	state := match.NewMatchState("smoke-test", config.Default)
	state.Status = match.StatusActive

	// Simulate 30 ticks (1 second of game time).
	for i := 0; i < 30; i++ {
		var tr match.TickResult
		state, tr = match.Tick(state)
		if tr.Scored {
			fmt.Printf("tick %d: slot %d scored! score P1=%d P2=%d\n",
				state.ServerTick, tr.ScoringSlot,
				state.Players[0].Score, state.Players[1].Score)
			if tr.MatchEnded {
				fmt.Printf("match ended, winner slot %d\n", tr.WinnerSlot)
				return
			}
			state = match.RallyReset(state, tr.ScoringSlot)
		}
	}
	fmt.Printf("smoke test complete: %d ticks, ball at (%.3f, %.3f)\n",
		state.ServerTick, state.Ball.X, state.Ball.Y)
}
```

- [ ] **Step 2: Run it**

```
cd D:\Pong-Mobile\backend
go run ./cmd/server/
```

Expected: prints tick count and ball position, no panic.

- [ ] **Step 3: Run all tests one final time**

```
cd D:\Pong-Mobile\backend
go test ./... -v
```

Expected: all PASS.

- [ ] **Step 4: Commit**

```
cd D:\Pong-Mobile\backend
git add cmd/server/main.go
git commit -m "chore: smoke-test main loop for physics simulation"
```
