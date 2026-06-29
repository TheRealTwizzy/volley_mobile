# Flutter Client Design

## Goal

A Flutter/Flame mobile app (Android + iOS) that connects to the Volley backend, lets two players create/join a room, plays a full match with smooth rendering, and shows the result screen. Uses server-authoritative state with local paddle prediction and snapshot interpolation.

## Stack

- Flutter 3.x (stable channel)
- Flame 1.x game engine
- `web_socket_channel` for WebSocket transport
- `shared_preferences` for session token persistence
- Portrait-only, phone-first layout

## Server URL

```dart
// lib/network/config.dart
const kServerUrl = 'wss://volley-server.fly.dev/ws';
```

Change to `ws://10.0.2.2:8080/ws` for Android emulator local dev. Never hardcode credentials.

## Screen Flow

```
ConnectScreen
  ↓ (WebSocket connected + server.hello received)
LobbyScreen
  ↓ create room               ↓ join room by code
WaitingRoomScreen ←──────────┘
  ↓ (both ready, match.started received)
MatchScreen (Flame GameWidget)
  ↓ (match.ended received)
ResultScreen
  ↓ rematch → WaitingRoomScreen
  ↓ quit → LobbyScreen
```

## Network Layer

### `lib/network/websocket_client.dart`

```
VolleyClient:
  _channel: WebSocketChannel
  _controller: StreamController<ServerMessage>

  connect(url) → Future<void>
  send(ClientMessage msg) → void
  stream → Stream<ServerMessage>
  dispose() → void
```

All inbound messages parsed from JSON into typed `ServerMessage` sealed class variants. All outbound messages serialized from typed `ClientMessage` variants.

### `lib/network/protocol.dart`

Typed message classes (sealed/union pattern):
- `ClientHello`, `RoomCreate`, `RoomJoin`, `RoomReady`, `RoomLeave`
- `InputPaddleTarget` (matchId, clientSeq, targetX)
- `MatchReconnect`
- `ServerHello`, `RoomCreated`, `RoomUpdated`, `MatchCountdown`, `MatchStarted`
- `MatchSnapshot`, `MatchScore`, `MatchRallyReset`, `MatchEnded`
- `PlayerDisconnected`, `PlayerReconnected`, `MatchReconnected`
- `ServerError`

### `lib/storage/session_store.dart`

```
SessionStore:
  saveToken(token: String) → Future<void>
  loadToken() → Future<String?>
  clear() → Future<void>
```

Uses `shared_preferences`. Token persisted across app restarts for reconnect.

## Screens

### `ConnectScreen`

- Text field: display name (default "Guest")
- "Play" button: connects WebSocket, sends `client.hello` with saved token (null if none)
- On `server.hello`: save token, navigate to LobbyScreen
- On connection error: show snackbar, allow retry

### `LobbyScreen`

- "Create Room" button → sends `room.create{settings: defaults}` → on `room.created` navigate to WaitingRoomScreen
- "Join Room" button + 6-digit code field → sends `room.join{roomCode}` → on `room.updated` navigate to WaitingRoomScreen
- On `error{room_not_found}`: show snackbar

### `WaitingRoomScreen`

- Shows room code prominently (host shares with friend)
- Lists both players with ready indicators
- "Ready" toggle button → sends `room.ready{ready: true/false}`
- On `room.updated`: refresh player list
- On `match.countdown`: show countdown timer overlay
- On `match.started`: navigate to MatchScreen

### `MatchScreen`

Wraps `GameWidget(game: VolleyGame(...))`. Passes `MatchStarted` payload and the `VolleyClient` stream into the game. Listens for `match.ended` → navigate to ResultScreen.

### `ResultScreen`

- Winner announcement
- Final score (p1 vs p2)
- "Rematch" button → sends `rematch.request`; on `rematch.started` navigate back to WaitingRoomScreen
- "Quit" button → sends `room.leave`, navigate to LobbyScreen

## Game (Flame)

### `lib/game/volley_game.dart`

```
VolleyGame extends FlameGame:
  localSlot: int          // 0=p1, 1=p2
  ball: BallComponent
  paddles: [PaddleComponent, PaddleComponent]
  interpolationBuffer: List<MatchSnapshot>
  renderTime: double      // server time we render at (serverTime - 100ms)
  inputSeq: int           // incremented on each input sent
  lastSentTargetX: double

  onLoad(): add components, subscribe to snapshot stream
  update(dt): advance renderTime, interpolate, apply local prediction
  onTapDown / onPanUpdate: send input.paddle_target
```

### `lib/game/components/`

- `ArenaComponent` — draws court background, center line
- `PaddleComponent` — rectangle, position driven by interpolation system
- `BallComponent` — circle, position driven by interpolation system

All positions are stored in normalized coords [0,1]; `onGameResize` scales to screen pixels:
```
screenX = normalizedX * size.x
screenY = normalizedY * size.y
```

Local player's paddle is always at bottom (arena flip): if `localSlot == 0`, render normally (p1 bottom). If `localSlot == 1`, flip Y: `renderY = 1.0 - normalizedY`.

### `lib/game/systems/interpolation.dart`

Snapshot buffer: sorted list of `MatchSnapshot` keyed by `serverTime`.

`interpolate(renderTime)`:
1. Find two snapshots straddling `renderTime`.
2. If found: lerp ball and both paddles between them.
3. If only past snapshots: extrapolate using last known velocity, capped at 150ms.
4. If buffer empty: hold last known position.

`renderTime = now - 100ms`. Snapshots arrive ~50ms apart; 100ms delay gives 2-snapshot buffer at all times.

### `lib/game/systems/prediction.dart`

Local paddle prediction:
- On each input sent: immediately move the local paddle to `targetX` clamp-padded by half paddle width.
- On snapshot received: if server paddle position differs from local by > 0.01 normalized units, snap to server value. Otherwise keep local.

This gives responsive feel while staying within server authority bounds.

### `lib/game/systems/input_controller.dart`

```
InputController:
  onPanUpdate(DragUpdateDetails):
    normalizedX = details.globalPosition.dx / screenWidth
    clamped = clamp(normalizedX, halfPaddleWidth, 1-halfPaddleWidth)
    if (clamped - lastSentTargetX).abs() > 0.005:
      send InputPaddleTarget(matchId, ++inputSeq, clamped)
      lastSentTargetX = clamped
```

Throttle: only send if target moved more than 0.005 normalized units. This avoids flooding the server with tiny movements.

## Provider / State Management

Use `ChangeNotifier` + `Provider` for:
- `ConnectionState` (disconnected / connecting / connected)
- `RoomState` (current room.updated payload)
- `MatchState` (current match state for non-game-screen display)

`VolleyClient` is provided at app root. Screens listen to the shared stream via Provider.

## Error / Disconnect Handling

- WebSocket closes unexpectedly during match → show reconnect overlay, attempt `match.reconnect` with saved token
- 10s reconnect window shown as countdown
- If reconnect fails → ResultScreen with "Connection lost"
- Non-match disconnects → return to ConnectScreen

## Testing

```
flutter test test/network/protocol_test.dart   — parse/serialize all message types
flutter test test/game/interpolation_test.dart — lerp between snapshots, extrapolation cap
flutter test test/game/prediction_test.dart    — local paddle snapping logic
flutter test test/game/input_controller_test.dart — throttle logic
```

Integration: run on Android emulator + real device pair against local backend.

## File Summary

| File | Action |
|---|---|
| `client/pubspec.yaml` | Create (flame, web_socket_channel, shared_preferences, provider) |
| `client/lib/main.dart` | Create |
| `client/lib/network/config.dart` | Create |
| `client/lib/network/websocket_client.dart` | Create |
| `client/lib/network/protocol.dart` | Create |
| `client/lib/storage/session_store.dart` | Create |
| `client/lib/screens/connect_screen.dart` | Create |
| `client/lib/screens/lobby_screen.dart` | Create |
| `client/lib/screens/waiting_room_screen.dart` | Create |
| `client/lib/screens/match_screen.dart` | Create |
| `client/lib/screens/result_screen.dart` | Create |
| `client/lib/game/volley_game.dart` | Create |
| `client/lib/game/components/arena_component.dart` | Create |
| `client/lib/game/components/ball_component.dart` | Create |
| `client/lib/game/components/paddle_component.dart` | Create |
| `client/lib/game/systems/interpolation.dart` | Create |
| `client/lib/game/systems/prediction.dart` | Create |
| `client/lib/game/systems/input_controller.dart` | Create |
| `client/test/network/protocol_test.dart` | Create |
| `client/test/game/interpolation_test.dart` | Create |
| `client/test/game/prediction_test.dart` | Create |
| `client/test/game/input_controller_test.dart` | Create |
