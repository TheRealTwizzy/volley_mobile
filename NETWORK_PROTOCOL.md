# NETWORK_PROTOCOL.md

# WebSocket Network Protocol

## 1. Purpose

This document defines the initial WebSocket protocol for the Pong-inspired Android/iOS mobile game.

The protocol is intentionally JSON-based for early development. A binary protocol may be added later if bandwidth, latency, or payload size becomes a proven issue.

## 2. Protocol Goals

The protocol must:

- Support persistent client-server communication.
- Allow private room creation and joining.
- Support ready state.
- Carry real-time paddle input.
- Broadcast authoritative match snapshots.
- Broadcast score and match events.
- Handle disconnect and reconnect.
- Be easy to debug during early development.

## 3. Transport

- Protocol: WebSocket
- Encoding: JSON
- Compression: off initially
- Heartbeat: server ping and client pong, or explicit JSON heartbeat messages
- Authentication: guest token first, account auth later

## 4. Message Envelope

Every message should use a common envelope.

### Client to Server

```json
{
  "type": "input.paddle_target",
  "requestId": "req_123",
  "sentAt": 1710000000000,
  "payload": {}
}
```

### Server to Client

```json
{
  "type": "match.snapshot",
  "serverTime": 1710000000033,
  "serverTick": 2401,
  "payload": {}
}
```

## 5. Shared Fields

| Field | Direction | Required | Description |
|---|---:|---:|---|
| `type` | Both | Yes | Message type |
| `requestId` | Client to server | Recommended | Client-generated request ID |
| `sentAt` | Client to server | Recommended | Client timestamp in milliseconds |
| `serverTime` | Server to client | Yes | Server timestamp in milliseconds |
| `serverTick` | Server to client | For match messages | Authoritative simulation tick |
| `payload` | Both | Yes | Message-specific body |

## 6. Error Format

```json
{
  "type": "error",
  "serverTime": 1710000000100,
  "payload": {
    "code": "room_not_found",
    "message": "Room not found.",
    "requestId": "req_123",
    "recoverable": true
  }
}
```

### Error Codes

| Code | Meaning |
|---|---|
| `bad_message` | Message could not be parsed |
| `unknown_type` | Message type is not supported |
| `unauthorized` | Session is missing or invalid |
| `room_not_found` | Room code does not exist |
| `room_full` | Room already has two players |
| `already_in_room` | Client is already in a room |
| `not_room_host` | Action requires host role |
| `match_already_started` | Match has already started |
| `invalid_state` | Action is not legal in current state |
| `rate_limited` | Client sent messages too quickly |
| `invalid_input` | Input failed validation |
| `match_not_found` | Match does not exist |
| `server_error` | Unexpected server error |

## 7. Session Messages

### client.hello

Sent immediately after WebSocket connection.

```json
{
  "type": "client.hello",
  "requestId": "req_001",
  "sentAt": 1710000000000,
  "payload": {
    "clientVersion": "0.1.0",
    "platform": "android",
    "sessionToken": null,
    "displayName": "Guest1234"
  }
}
```

### server.hello

```json
{
  "type": "server.hello",
  "serverTime": 1710000000030,
  "payload": {
    "sessionId": "sess_abc",
    "playerId": "player_123",
    "sessionToken": "opaque-session-token",
    "heartbeatIntervalMs": 10000
  }
}
```

## 8. Heartbeat Messages

### client.pong

```json
{
  "type": "client.pong",
  "sentAt": 1710000005000,
  "payload": {
    "pingId": "ping_123"
  }
}
```

### server.ping

```json
{
  "type": "server.ping",
  "serverTime": 1710000004000,
  "payload": {
    "pingId": "ping_123"
  }
}
```

## 9. Room Messages

### room.create

```json
{
  "type": "room.create",
  "requestId": "req_010",
  "sentAt": 1710000010000,
  "payload": {
    "settings": {
      "pointsToWin": 5,
      "paddleSpeed": 0.9,
      "ballSpeed": 0.55
    }
  }
}
```

### room.created

```json
{
  "type": "room.created",
  "serverTime": 1710000010050,
  "payload": {
    "roomId": "room_123",
    "roomCode": "842913",
    "hostPlayerId": "player_123",
    "settings": {
      "pointsToWin": 5,
      "paddleSpeed": 0.9,
      "ballSpeed": 0.55
    }
  }
}
```

### room.join

```json
{
  "type": "room.join",
  "requestId": "req_011",
  "sentAt": 1710000015000,
  "payload": {
    "roomCode": "842913"
  }
}
```

### room.updated

Broadcast to room members when membership or readiness changes.

```json
{
  "type": "room.updated",
  "serverTime": 1710000015100,
  "payload": {
    "roomId": "room_123",
    "roomCode": "842913",
    "status": "waiting",
    "players": [
      {
        "playerId": "player_123",
        "displayName": "Host",
        "role": "host",
        "ready": false,
        "connected": true
      },
      {
        "playerId": "player_456",
        "displayName": "Guest",
        "role": "guest",
        "ready": false,
        "connected": true
      }
    ],
    "settings": {
      "pointsToWin": 5,
      "paddleSpeed": 0.9,
      "ballSpeed": 0.55
    }
  }
}
```

### room.ready

```json
{
  "type": "room.ready",
  "requestId": "req_012",
  "sentAt": 1710000020000,
  "payload": {
    "ready": true
  }
}
```

### room.leave

```json
{
  "type": "room.leave",
  "requestId": "req_013",
  "sentAt": 1710000025000,
  "payload": {}
}
```

## 10. Match Start Messages

### match.countdown

```json
{
  "type": "match.countdown",
  "serverTime": 1710000030000,
  "payload": {
    "matchId": "match_123",
    "startsAt": 1710000033000,
    "durationMs": 3000
  }
}
```

### match.started

```json
{
  "type": "match.started",
  "serverTime": 1710000033000,
  "serverTick": 0,
  "payload": {
    "matchId": "match_123",
    "playerSlot": "p1",
    "opponentSlot": "p2",
    "settings": {
      "pointsToWin": 5,
      "tickRate": 30,
      "snapshotRate": 20
    },
    "initialState": {
      "ball": {
        "x": 0.5,
        "y": 0.5,
        "vx": 0.24,
        "vy": -0.5
      },
      "players": {
        "p1": {
          "playerId": "player_123",
          "paddleX": 0.5,
          "score": 0
        },
        "p2": {
          "playerId": "player_456",
          "paddleX": 0.5,
          "score": 0
        }
      }
    }
  }
}
```

## 11. Input Messages

Two input styles are allowed during prototyping. Choose one as the primary method before production.

## 11.1 Paddle Direction Input

Use this for keyboard/controller-style movement.

```json
{
  "type": "input.paddle_direction",
  "requestId": "req_100",
  "sentAt": 1710000040000,
  "payload": {
    "matchId": "match_123",
    "clientSeq": 1,
    "direction": 1
  }
}
```

Allowed `direction` values:

| Value | Meaning |
|---:|---|
| `-1` | Move left |
| `0` | Stop |
| `1` | Move right |

## 11.2 Paddle Target Input

Use this for touch-drag movement.

```json
{
  "type": "input.paddle_target",
  "requestId": "req_101",
  "sentAt": 1710000040033,
  "payload": {
    "matchId": "match_123",
    "clientSeq": 2,
    "targetX": 0.64
  }
}
```

Validation:

- `targetX` must be between `0.0` and `1.0`.
- Server must clamp paddle movement to maximum speed.
- Server must reject input for players not in the active match.
- Server must ignore old or duplicate `clientSeq` values.

## 12. Snapshot Messages

### match.snapshot

```json
{
  "type": "match.snapshot",
  "serverTime": 1710000040100,
  "serverTick": 24,
  "payload": {
    "matchId": "match_123",
    "ball": {
      "x": 0.482,
      "y": 0.318,
      "vx": 0.24,
      "vy": -0.5
    },
    "players": {
      "p1": {
        "paddleX": 0.42,
        "score": 0
      },
      "p2": {
        "paddleX": 0.57,
        "score": 0
      }
    }
  }
}
```

Snapshot payloads should be small. Do not include profile data, cosmetics, room metadata, or historical events in the high-frequency snapshot.

## 13. Match Event Messages

### match.score

```json
{
  "type": "match.score",
  "serverTime": 1710000050000,
  "serverTick": 180,
  "payload": {
    "matchId": "match_123",
    "scoringSlot": "p1",
    "score": {
      "p1": 1,
      "p2": 0
    },
    "nextRallyStartsAt": 1710000053000
  }
}
```

### match.rally_reset

```json
{
  "type": "match.rally_reset",
  "serverTime": 1710000053000,
  "serverTick": 181,
  "payload": {
    "matchId": "match_123",
    "ball": {
      "x": 0.5,
      "y": 0.5,
      "vx": -0.2,
      "vy": 0.55
    }
  }
}
```

### match.ended

```json
{
  "type": "match.ended",
  "serverTime": 1710000090000,
  "serverTick": 1200,
  "payload": {
    "matchId": "match_123",
    "winnerSlot": "p1",
    "reason": "points_to_win",
    "finalScore": {
      "p1": 5,
      "p2": 2
    }
  }
}
```

Allowed `reason` values:

| Reason | Meaning |
|---|---|
| `points_to_win` | A player reached the target score |
| `opponent_disconnected` | Opponent failed to reconnect |
| `room_closed` | Match was closed before completion |
| `server_shutdown` | Server ended match due to maintenance or failure |

## 14. Reconnect Messages

### match.reconnect

```json
{
  "type": "match.reconnect",
  "requestId": "req_200",
  "sentAt": 1710000060000,
  "payload": {
    "sessionToken": "opaque-session-token",
    "matchId": "match_123"
  }
}
```

### match.reconnected

```json
{
  "type": "match.reconnected",
  "serverTime": 1710000060100,
  "serverTick": 540,
  "payload": {
    "matchId": "match_123",
    "slot": "p2",
    "currentState": {
      "ball": {
        "x": 0.35,
        "y": 0.41,
        "vx": -0.19,
        "vy": 0.52
      },
      "players": {
        "p1": {
          "paddleX": 0.48,
          "score": 2
        },
        "p2": {
          "paddleX": 0.62,
          "score": 1
        }
      }
    }
  }
}
```

## 15. Disconnect Messages

### player.disconnected

```json
{
  "type": "player.disconnected",
  "serverTime": 1710000065000,
  "payload": {
    "matchId": "match_123",
    "slot": "p2",
    "reconnectDeadline": 1710000075000
  }
}
```

### player.reconnected

```json
{
  "type": "player.reconnected",
  "serverTime": 1710000069000,
  "payload": {
    "matchId": "match_123",
    "slot": "p2"
  }
}
```

## 16. Rematch Messages

### rematch.request

```json
{
  "type": "rematch.request",
  "requestId": "req_300",
  "sentAt": 1710000100000,
  "payload": {
    "matchId": "match_123"
  }
}
```

### rematch.updated

```json
{
  "type": "rematch.updated",
  "serverTime": 1710000100100,
  "payload": {
    "previousMatchId": "match_123",
    "players": {
      "p1": {
        "wantsRematch": true
      },
      "p2": {
        "wantsRematch": false
      }
    }
  }
}
```

### rematch.started

```json
{
  "type": "rematch.started",
  "serverTime": 1710000105000,
  "payload": {
    "previousMatchId": "match_123",
    "newMatchId": "match_456"
  }
}
```

## 17. Rate Limits

Suggested initial limits:

| Message Type | Limit |
|---|---|
| `input.paddle_target` | 30 per second |
| `input.paddle_direction` | 30 per second |
| `room.create` | 5 per minute |
| `room.join` | 20 per minute |
| `room.ready` | 10 per minute |
| `rematch.request` | 10 per minute |

The backend should rate-limit per connection and per player ID.

## 18. Validation Rules

The server must validate:

- Message is valid JSON.
- `type` is known.
- Required fields exist.
- Player session is valid.
- Player is in the room or match.
- Match is in the correct state.
- Input values are in allowed ranges.
- Input sequence number is newer than the last accepted input.
- Player cannot control the opponent paddle.
- Player cannot send match input after match end.

## 19. Protocol Versioning

The client should send `clientVersion` in `client.hello`.

The server should include protocol compatibility in `server.hello` later:

```json
{
  "protocolVersion": "0.1"
}
```

Breaking protocol changes should increment the protocol version.

## 20. Debugging Requirements

During early development, the backend should log:

- Connection opened
- Connection closed
- Session created
- Room created
- Room joined
- Ready state changed
- Match started
- Match ended
- Error responses
- Reconnect attempts

Avoid logging every input message in normal mode because it will generate too much noise.

## 21. First Implementation Target

The first implementation should support only these messages:

Client to server:

- `client.hello`
- `room.create`
- `room.join`
- `room.ready`
- `input.paddle_target`
- `client.pong`

Server to client:

- `server.hello`
- `room.created`
- `room.updated`
- `match.countdown`
- `match.started`
- `match.snapshot`
- `match.score`
- `match.ended`
- `server.ping`
- `error`
