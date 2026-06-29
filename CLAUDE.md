# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Volley** — real-time 1v1 mobile arcade game (Pong-inspired). Server-authoritative architecture. Two components: Go backend and Flutter/Flame client.

> The game is named **Volley**. Do not use "Pong" as the product name.

## Stack

**Backend:** Go, WebSockets (JSON), PostgreSQL, in-memory match state  
**Client:** Flutter + Flame engine, Dart WebSocket client, portrait-only Android/iOS

## Backend Commands

```sh
# Run server
go run ./cmd/server

# Run all tests
go test ./...

# Run single package tests
go test ./internal/match/...

# Build
go build ./cmd/server
```

## Client Commands

```sh
# Run on connected device/emulator
flutter run

# Run tests
flutter test

# Run single test file
flutter test test/game/physics_test.dart

# Build Android APK
flutter build apk

# Build iOS
flutter build ios
```

## Architecture

### Server Authority Model

Server owns all game state. Clients send only:
- `targetX` (0.0–1.0) for paddle position
- Room/session control messages

Server simulates everything: paddle movement, ball physics, collisions, scoring. Never trust client positions.

### Backend Internal Layout (`backend/internal/`)

| Package | Responsibility |
|---|---|
| `match/` | Authoritative match simulation, tick loop, state |
| `physics/` | Ball movement, wall/paddle collision, bounce angles |
| `lobby/` | Room creation, join, ready state, match start trigger |
| `websocket/` | Connection manager, message routing, rate limiting |
| `protocol/` | Message types, envelope parsing, serialization |
| `auth/` | Guest session tokens, player identity |
| `storage/` | PostgreSQL persistence (users, matches, results) |

### Client Layout (`client/lib/`)

| Path | Responsibility |
|---|---|
| `game/` | Flame game, components (ball, paddle, arena) |
| `game/systems/` | Input controller, snapshot interpolation, local prediction |
| `network/` | WebSocket client, protocol message handling |
| `screens/` | Lobby, room, match, result UI screens |
| `storage/` | Session token persistence |

### Tick/Snapshot Model

- Server tick: 30/s (`dt = 1/30`)
- Snapshot broadcast: 20/s
- Client renders at device refresh rate using interpolation

Client renders 100ms behind server time (`interpolationDelayMs = 100`). Extrapolation capped at 150ms before showing connection warning.

### Coordinate System

All simulation uses normalized coordinates `0.0–1.0`. Client maps to pixels:
```
screenX = normalizedX * screenWidth
screenY = normalizedY * arenaHeight
```

Player 1 paddle at `y=0.93`, Player 2 at `y=0.07`.

### Protocol

JSON WebSocket. All messages use envelope:
- Client→Server: `{ type, requestId, sentAt, payload }`
- Server→Client: `{ type, serverTime, serverTick, payload }`

See `NETWORK_PROTOCOL.md` for full message specs. See `GAME_LOOP_SPEC.md` for physics constants and simulation rules.

### Database

Three tables: `users`, `matches`, `match_results`. Only persist match outcomes — never per-tick state. Schema in `PROJECT_BLUEPRINT.md` §14.

### Disconnect/Reconnect

Match pauses on disconnect. 10s reconnect window. Forfeit if exceeded. Session token used to resume (`match.reconnect` message).

## Development Milestones

1. Local prototype (physics only, no network)
2. WebSocket skeleton + heartbeat
3. Room system
4. Server-authoritative match loop
5. Client smoothing (interpolation + prediction)
6. Results + persistence

First playable = two clients can complete a full match end-to-end.
