# PROJECT_BLUEPRINT.md

# Pong-Inspired Mobile Remake: Project Blueprint

## 1. Purpose

This document defines the starting blueprint for a fresh Android/iOS Pong-inspired multiplayer game. The project begins with a custom backend using WebSockets. No Firebase-first architecture is assumed.

The project goal is to create a real-time competitive mobile game with a stable 1v1 online core before adding customization, monetization, leaderboards, power-ups, or special match variants.

## 2. Product Summary

The game is a mobile, Pong-inspired, real-time 1v1 arcade game.

Each player controls a paddle on their own mobile device. The backend owns the official match state, validates player input, simulates the ball and paddles, determines scoring, and broadcasts match updates to both clients over WebSockets.

The first version should be simple, deterministic, and resilient under normal mobile network conditions.

## 3. Platform Targets

- Android
- iOS
- Portrait orientation
- Phone-first layout
- Tablet support can be added later
- Offline local testing should be supported during development

## 4. Recommended Client Stack

### Primary Recommendation

- Flutter
- Flame game engine
- Dart WebSocket client
- Local storage for lightweight preferences/session tokens

### Rationale

Flutter provides a single codebase for Android and iOS. Flame provides a game loop, components, collision helpers, and rendering abstractions that are suitable for a 2D arcade game.

### Alternative Stacks

| Stack | Use Case |
|---|---|
| Unity | Better if future 3D, complex animation tooling, or advanced visual effects become central |
| Godot | Good open-source game engine option, but mobile build and networking workflows may require extra care |
| Native Android/iOS | Avoid unless separate platform-specific codebases are desired |

## 5. Recommended Backend Stack

### Primary Recommendation

- Go backend
- WebSockets
- PostgreSQL
- In-memory match manager
- Optional Redis later for scaling and presence

### Rationale

Go is suitable for concurrent WebSocket connections, long-running match loops, low memory overhead, and simple deployment. PostgreSQL should persist durable entities such as accounts, matches, and results. Live match state should remain in memory during active play.

## 6. Core Architecture

The game should use a server-authoritative architecture.

Clients send input. The server validates input, advances the official simulation, detects collisions and scoring, and sends snapshots/events to clients.

```text
Client A input ┐
               ├── WebSocket Server ── Authoritative Match Simulation ── Snapshots/Events
Client B input ┘
```

## 7. Authority Model

The server is authoritative for:

- Match creation
- Room membership
- Ready state
- Game start
- Paddle bounds
- Paddle speed limits
- Ball position
- Ball velocity
- Wall collisions
- Paddle collisions
- Scoring
- Match completion
- Disconnect handling
- Reconnect handling
- Final match result

Clients are responsible for:

- Rendering
- Touch input
- Local paddle prediction
- Snapshot interpolation
- Sound effects
- UI transitions
- Client-side visual polish

## 8. MVP Scope

### Must Have

- App launches on Android and iOS
- Connect to WebSocket backend
- Create private room
- Join private room by room code
- Ready state for both players
- Match countdown
- Server-authoritative paddle movement
- Server-authoritative ball physics
- Score detection
- Match ends at points-to-win
- Result screen
- Basic disconnect handling
- Basic reconnect window

### Should Have

- Guest session support
- Display name
- Ping/latency indicator
- Basic sound effects
- Basic match settings
- Rematch option

### Not In MVP

- Ranked matchmaking
- Leaderboards
- Cosmetics
- Monetization
- In-app purchases
- Ads
- Multiple balls
- Power-ups
- Paddle upgrades
- Tournament mode
- Chat
- Friend system
- Spectators

## 9. Core Gameplay Rules

### Arena

Use normalized coordinates on the server:

```text
x: 0.0 to 1.0
y: 0.0 to 1.0
```

The server simulation is independent from device resolution.

### Players

- Player 1 paddle is near the bottom of the normalized arena.
- Player 2 paddle is near the top of the normalized arena.
- Each client may render the arena from their own perspective.
- For MVP debugging, showing both paddles in one shared arena is recommended.

### Ball

- Ball starts near the center after countdown.
- Ball moves at a configurable speed.
- Ball bounces off left and right walls.
- Ball bounces off paddles.
- A score occurs when the ball crosses a scoring boundary.

### Scoring

- If Player 1 misses, Player 2 scores.
- If Player 2 misses, Player 1 scores.
- First player to `pointsToWin` wins the match.

## 10. Match Lifecycle

```text
1. Client connects to backend.
2. Client creates or joins a room.
3. Both players enter ready state.
4. Server locks match configuration.
5. Server starts countdown.
6. Server starts match simulation.
7. Clients send paddle input.
8. Server sends snapshots/events.
9. Server detects scoring.
10. Server resets rally.
11. Server ends match when points-to-win is reached.
12. Server stores result.
13. Clients show result screen.
```

## 11. Network Model

Use WebSockets for persistent bidirectional communication.

Client-to-server messages:

- Session hello
- Create room
- Join room
- Leave room
- Ready/unready
- Paddle input
- Reconnect
- Request rematch

Server-to-client messages:

- Session accepted
- Room created
- Room joined
- Room updated
- Match countdown
- Match started
- Match snapshot
- Score event
- Match ended
- Error
- Ping/pong heartbeat

## 12. Tick and Snapshot Model

Recommended starting values:

| Item | Value |
|---|---|
| Server tick rate | 30 ticks per second |
| Snapshot rate | 20 snapshots per second |
| Client render rate | Device refresh rate |
| Physics units | Normalized |
| Match state storage | In memory |
| Durable storage | PostgreSQL |

The server should simulate at a fixed tick rate. Clients should render smoothly using interpolation between snapshots.

## 13. Persistence Model

Persist:

- Users or guest profiles
- Match records
- Match results
- Room creation metadata
- Optional key match events

Do not persist every simulation tick.

## 14. Initial Database Tables

### users

```sql
CREATE TABLE users (
  id UUID PRIMARY KEY,
  display_name TEXT NOT NULL,
  is_guest BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### matches

```sql
CREATE TABLE matches (
  id UUID PRIMARY KEY,
  room_code TEXT UNIQUE,
  player_one_id UUID REFERENCES users(id),
  player_two_id UUID REFERENCES users(id),
  status TEXT NOT NULL,
  points_to_win INTEGER NOT NULL,
  winner_id UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ,
  ended_at TIMESTAMPTZ
);
```

### match_results

```sql
CREATE TABLE match_results (
  id UUID PRIMARY KEY,
  match_id UUID NOT NULL REFERENCES matches(id),
  player_id UUID NOT NULL REFERENCES users(id),
  score INTEGER NOT NULL,
  result TEXT NOT NULL
);
```

## 15. Backend Folder Structure

```text
backend/
  cmd/
    server/
      main.go
  internal/
    auth/
    config/
    lobby/
    match/
    physics/
    protocol/
    storage/
    websocket/
  migrations/
  tests/
```

## 16. Client Folder Structure

```text
client/
  lib/
    main.dart
    app/
    game/
      pong_game.dart
      components/
        arena.dart
        ball.dart
        paddle.dart
      systems/
        input_controller.dart
        interpolation.dart
        prediction.dart
    network/
      websocket_client.dart
      protocol.dart
    screens/
      lobby_screen.dart
      room_screen.dart
      match_screen.dart
      result_screen.dart
    storage/
      session_store.dart
```

## 17. Major Technical Risks

| Risk | Mitigation |
|---|---|
| Paddle input feels delayed | Local prediction for the local paddle |
| Ball appears jittery | Snapshot interpolation and buffering |
| Clients disagree with match state | Keep server authoritative |
| Cheating through fake inputs | Validate all movement server-side |
| Mobile app backgrounding breaks matches | Add reconnect window and forfeit rules |
| Match loops become hard to scale | Keep one match manager first, add Redis or sharding later |
| Database writes become excessive | Do not persist per-tick state |

## 18. Development Milestones

### Milestone 1: Local Game Prototype

- Paddle movement
- Ball movement
- Wall collision
- Paddle collision
- Score detection
- Rally reset

### Milestone 2: WebSocket Skeleton

- Server starts
- Client connects
- Heartbeat works
- Basic message parsing
- Error response format

### Milestone 3: Room System

- Create room
- Join room
- Leave room
- Ready state
- Match start trigger

### Milestone 4: Server-Authoritative Match

- Server tick loop
- Server paddle movement
- Server ball physics
- Snapshot broadcasts
- Score events

### Milestone 5: Client Smoothing

- Snapshot interpolation
- Local paddle prediction
- Latency indicator
- Correct visual reconciliation

### Milestone 6: Results and Persistence

- Match end
- Winner detection
- Result storage
- Result screen
- Rematch flow

## 19. Definition of First Playable

The first playable version is complete when two mobile clients can:

1. Connect to the backend.
2. Enter the same room.
3. Ready up.
4. Play a full 1v1 match.
5. See synchronized scores.
6. Finish the match.
7. Return to a result screen.

No monetization, cosmetics, leaderboards, or advanced mechanics should be added before this milestone.
