# GAME_LOOP_SPEC.md

# Game Loop Specification

## 1. Purpose

This document defines the initial game loop, physics model, simulation rules, scoring rules, and latency-handling approach for the Pong-inspired Android/iOS game.

The game uses a custom WebSocket backend with a server-authoritative simulation.

## 2. Design Goals

The game loop must be:

- Deterministic enough for server authority.
- Simple enough to debug.
- Independent of device resolution.
- Resistant to basic cheating.
- Stable under moderate mobile network latency.
- Extendable for future mechanics such as multiple balls, paddle modifiers, and power-ups.

## 3. Coordinate System

The server uses normalized coordinates.

```text
x: 0.0 to 1.0
y: 0.0 to 1.0
```

The client maps normalized coordinates to screen pixels.

```text
screenX = normalizedX * screenWidth
screenY = normalizedY * arenaHeight
```

For MVP, the full arena is visible to both players. Each client may optionally flip the arena so the local paddle appears at the bottom.

## 4. Simulation Authority

The server owns:

- Paddle positions
- Ball position
- Ball velocity
- Ball speed
- Collision decisions
- Score decisions
- Match timer
- Match outcome

Clients may predict local paddle movement visually, but server snapshots must eventually correct local render state.

## 5. Server Tick Rate

Recommended initial values:

| Setting | Value |
|---|---:|
| Server tick rate | 30 ticks per second |
| Tick duration | 33.333 ms |
| Snapshot rate | 20 snapshots per second |
| Snapshot interval | 50 ms |
| Client render rate | Device refresh rate |

The server should use a fixed timestep.

```text
dt = 1.0 / 30.0
```

## 6. Match State

The authoritative match state should contain:

```text
MatchState
  matchId
  status
  serverTick
  settings
  ball
  players
  score
  rallyState
```

### Ball State

```text
Ball
  x
  y
  vx
  vy
  speed
  radius
```

### Player State

```text
Player
  playerId
  slot
  paddleX
  targetX
  inputDirection
  score
  connected
  lastInputSeq
```

### Settings

```text
Settings
  pointsToWin
  paddleSpeed
  paddleWidth
  paddleHeight
  ballSpeed
  ballRadius
  countdownMs
  reconnectWindowMs
```

## 7. Initial Recommended Constants

These are starting values, not final balancing decisions.

```text
pointsToWin = 5
paddleWidth = 0.22
paddleHeight = 0.025
paddleSpeed = 0.9 units/second
ballRadius = 0.018
ballSpeed = 0.55 units/second
countdownMs = 3000
rallyResetMs = 1500
reconnectWindowMs = 10000
```

## 8. Paddle Movement

The game should support touch-friendly target movement.

Client sends:

```text
targetX: 0.0 to 1.0
```

Server clamps paddle target and movement speed.

### Paddle Bounds

The paddle center cannot move beyond:

```text
minX = paddleWidth / 2
maxX = 1.0 - paddleWidth / 2
```

### Paddle Update

```text
delta = targetX - paddleX
maxStep = paddleSpeed * dt

if abs(delta) <= maxStep:
  paddleX = targetX
else:
  paddleX += sign(delta) * maxStep

paddleX = clamp(paddleX, minX, maxX)
```

## 9. Ball Movement

The ball moves every server tick.

```text
ball.x += ball.vx * ball.speed * dt
ball.y += ball.vy * ball.speed * dt
```

The velocity vector should remain normalized after collision changes.

```text
length = sqrt(vx * vx + vy * vy)
vx = vx / length
vy = vy / length
```

## 10. Wall Collision

The ball bounces on the left and right walls.

### Left Wall

```text
if ball.x - ball.radius <= 0:
  ball.x = ball.radius
  ball.vx = abs(ball.vx)
```

### Right Wall

```text
if ball.x + ball.radius >= 1:
  ball.x = 1 - ball.radius
  ball.vx = -abs(ball.vx)
```

## 11. Paddle Positions

For MVP:

```text
Player 1 paddleY = 0.93
Player 2 paddleY = 0.07
```

Each paddle has:

```text
left = paddleX - paddleWidth / 2
right = paddleX + paddleWidth / 2
top = paddleY - paddleHeight / 2
bottom = paddleY + paddleHeight / 2
```

## 12. Paddle Collision

Collision checks should happen after ball movement.

### Player 1 Paddle

Player 1 paddle is near the bottom. The ball collides if it is moving downward.

```text
if ball.vy > 0:
  check Player 1 paddle
```

### Player 2 Paddle

Player 2 paddle is near the top. The ball collides if it is moving upward.

```text
if ball.vy < 0:
  check Player 2 paddle
```

### Collision Bounds

The ball intersects the paddle if:

```text
ball.x + radius >= paddle.left
ball.x - radius <= paddle.right
ball.y + radius >= paddle.top
ball.y - radius <= paddle.bottom
```

## 13. Paddle Bounce Angle

Bounce angle should depend on where the ball hits the paddle.

```text
relativeHit = (ball.x - paddle.x) / (paddleWidth / 2)
relativeHit = clamp(relativeHit, -1.0, 1.0)
```

Recommended maximum bounce angle:

```text
maxBounceAngle = 60 degrees
```

For Player 1:

```text
angle = -90 degrees + relativeHit * maxBounceAngle
```

For Player 2:

```text
angle = 90 degrees - relativeHit * maxBounceAngle
```

Then convert angle to velocity:

```text
vx = cos(angle)
vy = sin(angle)
```

This gives the player control over the return direction.

## 14. Score Detection

A score occurs when the ball fully crosses the top or bottom boundary.

### Player 1 Scores

```text
if ball.y + ball.radius < 0:
  player1.score += 1
```

### Player 2 Scores

```text
if ball.y - ball.radius > 1:
  player2.score += 1
```

After scoring:

1. Stop active rally.
2. Broadcast `match.score`.
3. Wait rally reset delay.
4. Start next rally unless match has ended.

## 15. Match End

The match ends when either player reaches `pointsToWin`.

```text
if player.score >= pointsToWin:
  endMatch(winner)
```

The server then:

1. Sets match status to ended.
2. Broadcasts `match.ended`.
3. Persists match result.
4. Stops simulation loop.

## 16. Rally Reset

After a score, the ball resets near center.

Recommended:

```text
ball.x = 0.5
ball.y = 0.5
```

The ball should launch toward the player who was just scored on, unless testing shows this feels wrong.

```text
if player1Scored:
  ball.vy = 1
else:
  ball.vy = -1
```

Add a small randomized horizontal component:

```text
ball.vx = random between -0.35 and 0.35
normalize velocity
```

Use a server-side random source only. Clients should not decide rally direction.

## 17. Input Handling

The server should store the latest valid input from each player.

### Input Validation

Reject or ignore input if:

- Match does not exist.
- Match is not active.
- Player is not in match.
- Input sequence number is old or duplicate.
- `targetX` is outside valid range and cannot be safely clamped.
- Client is sending input too frequently.
- Player is disconnected or forfeited.

### Input Sequence

Each client sends an increasing `clientSeq`.

```text
if clientSeq <= player.lastInputSeq:
  ignore input
else:
  accept input
```

## 18. Client Prediction

The local client may visually move its paddle immediately after touch input.

This reduces perceived latency.

The client should:

1. Send `targetX` to server.
2. Move local paddle visually toward `targetX`.
3. Receive snapshots.
4. Reconcile predicted paddle position with authoritative paddle position.

Do not predict ball collisions on the client for official logic.

## 19. Snapshot Interpolation

The client should render slightly behind server time.

Recommended starting buffer:

```text
interpolationDelayMs = 100
```

The client stores recent snapshots and renders between two known server snapshots.

```text
renderTime = estimatedServerTime - interpolationDelayMs
```

Find two snapshots:

```text
snapshotA.serverTime <= renderTime
snapshotB.serverTime >= renderTime
```

Interpolate:

```text
alpha = (renderTime - snapshotA.time) / (snapshotB.time - snapshotA.time)
renderX = lerp(snapshotA.x, snapshotB.x, alpha)
renderY = lerp(snapshotA.y, snapshotB.y, alpha)
```

## 20. Extrapolation Limit

If the client does not have a newer snapshot, it may extrapolate briefly.

Recommended maximum:

```text
maxExtrapolationMs = 150
```

Beyond this, the client should show a connection warning or freeze remote state rather than continuing to guess.

## 21. Disconnect Handling

When a player disconnects during a match:

1. Server marks player disconnected.
2. Server broadcasts `player.disconnected`.
3. Server starts reconnect timer.
4. Match may pause during reconnect window.
5. If player reconnects in time, match resumes.
6. If player does not reconnect, opponent wins by disconnect.

Recommended MVP policy:

```text
Pause match during reconnect window.
Forfeit after 10 seconds without reconnect.
```

## 22. Mobile Backgrounding Policy

Mobile apps can be backgrounded unexpectedly.

Initial policy:

| Situation | Result |
|---|---|
| Background for less than reconnect window | Allow reconnect |
| Background longer than reconnect window | Forfeit |
| App closed intentionally | Treat as disconnect |
| Network drops briefly | Allow reconnect |

## 23. Anti-Cheat Rules

The server must never trust client positions.

Clients may send:

- Target paddle position
- Direction
- Ready state
- Room actions

Clients may not send:

- Current ball position
- Current official paddle position
- Score
- Collision results
- Match winner
- Opponent state changes

The server must clamp movement and validate all state transitions.

## 24. Latency Expectations

The MVP should be playable under moderate latency, but it will not feel identical to local play.

Recommended client features:

- Ping display
- Local paddle prediction
- Snapshot interpolation
- Connection warning
- Graceful reconnect UI

## 25. Extensibility Rules

Future mechanics should attach to the server simulation, not client-only logic.

Examples:

- Multiple balls
- Paddle size changes
- Ball speed modifiers
- Temporary shields
- Power-ups
- Special arenas

The server must own the official version of every mechanic.

## 26. First Playable Completion Criteria

The game loop is complete when:

1. Two clients can join a match.
2. Both can move paddles.
3. The server simulates ball movement.
4. Ball collisions are authoritative.
5. Scores are synchronized.
6. Match ends correctly.
7. Disconnect and reconnect behavior is defined.
8. Clients render smoothly using snapshots.

## 27. Known Open Decisions

These should be decided after prototype testing:

- Should both paddles be visible in MVP?
- Should the client flip the arena for Player 2?
- What paddle size feels best on phones?
- What ball speed feels fair under mobile latency?
- Should the ball speed increase over time?
- Should the first launch direction be random or alternate?
- Should reconnect pause the match or let AI/idle movement continue?
