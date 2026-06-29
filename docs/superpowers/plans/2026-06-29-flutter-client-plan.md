# Flutter Client Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete Flutter/Flame mobile client for Volley that connects to the Go backend and allows two players to play a full match end-to-end.

**Architecture:** The client is a Flutter/Flame app organized in three layers: a network layer (`lib/network/`) that owns WebSocket transport and typed protocol message parsing, a game layer (`lib/game/`) containing the Flame game with snapshot interpolation and local paddle prediction, and a screen layer (`lib/screens/`) that drives the app's five-screen flow (Connect → Lobby → WaitingRoom → Match → Result). State shared between screens is managed via `provider` with `ChangeNotifier` models, and `VolleyClient` is provided at the app root so every screen subscribes to the same inbound message stream.

**Tech Stack:** Flutter 3.x (stable), Flame 1.x, web_socket_channel, shared_preferences, provider

## Global Constraints

- Flutter stable channel, Dart null-safe
- Flame 1.x (not 2.x — use `FlameGame`, not `World`)
- Server URL: `wss://volley-server.fly.dev/ws` (local: `ws://10.0.2.2:8080/ws`)
- Normalized coordinates [0.0, 1.0] for all physics
- Portrait-only, phone-first layout
- No new dependencies beyond: flame, web_socket_channel, shared_preferences, provider
- All tests runnable with `flutter test`
- Never hardcode credentials

---

## Task 1: Project Scaffold + pubspec + Config

**What this task delivers:** A compilable Flutter project with all dependencies pinned, a `config.dart` with the server URL constant, and the directory skeleton that every subsequent task builds into.

### Steps

- [ ] **1.1 Scaffold the project**

  Run from the repo root (`D:\Pong-Mobile`):

  ```sh
  flutter create --org com.volley --project-name volley --platforms android,ios client
  ```

  This produces:

  ```
  client/
    android/
    ios/
    lib/
      main.dart          # default counter app — will be replaced in Task 7
    test/
      widget_test.dart   # will be replaced/extended
    pubspec.yaml
    pubspec.lock
    analysis_options.yaml
  ```

- [ ] **1.2 Replace `pubspec.yaml`**

  Overwrite `client/pubspec.yaml` with:

  ```yaml
  name: volley
  description: Volley — real-time 1v1 mobile arcade game.
  publish_to: 'none'
  version: 0.1.0+1

  environment:
    sdk: '>=3.0.0 <4.0.0'

  dependencies:
    flutter:
      sdk: flutter
    flame: ^1.18.0
    web_socket_channel: ^2.4.0
    shared_preferences: ^2.2.0
    provider: ^6.1.0

  dev_dependencies:
    flutter_test:
      sdk: flutter
    flutter_lints: ^3.0.0

  flutter:
    uses-material-design: true
  ```

- [ ] **1.3 Fetch dependencies**

  ```sh
  cd client && flutter pub get
  ```

- [ ] **1.4 Create directory skeleton**

  ```sh
  mkdir client/lib/network
  mkdir client/lib/storage
  mkdir client/lib/game
  mkdir client/lib/game/components
  mkdir client/lib/game/systems
  mkdir client/lib/screens
  mkdir client/test/network
  mkdir client/test/game
  ```

- [ ] **1.5 Create `client/lib/network/config.dart`**

  ```dart
  // lib/network/config.dart

  /// Production WebSocket server URL.
  const kServerUrl = 'wss://volley-server.fly.dev/ws';

  /// Android emulator local dev URL — swap in when running against local backend.
  const kLocalServerUrl = 'ws://10.0.2.2:8080/ws';

  /// Client version sent in client.hello.
  const kClientVersion = '0.1.0';
  ```

- [ ] **1.6 Verify analyze passes**

  ```sh
  cd client && flutter analyze
  ```

  Expected: no errors.

- [ ] **1.7 Commit**

  ```sh
  git add client/
  git commit -m "feat(client): scaffold Flutter project with pubspec and config"
  ```

---

## Task 2: Protocol Message Types

**What this task delivers:** Fully typed Dart classes for every message the client sends to and receives from the server, with `fromJson`/`toJson` serialization. Tests verify round-trip parsing for every type.

### Steps

- [ ] **2.1 Write the failing test first**

  Create `client/test/network/protocol_test.dart`:

  ```dart
  import 'dart:convert';
  import 'package:flutter_test/flutter_test.dart';
  import 'package:volley/network/protocol.dart';

  void main() {
    group('ClientMessage serialization', () {
      test('ClientHello toJson', () {
        final msg = ClientHello(
          requestId: 'req_001',
          sentAt: 1710000000000,
          displayName: 'Alice',
          sessionToken: null,
        );
        final json = msg.toJson();
        expect(json['type'], 'client.hello');
        expect(json['requestId'], 'req_001');
        expect(json['sentAt'], 1710000000000);
        expect(json['payload']['displayName'], 'Alice');
        expect(json['payload']['sessionToken'], isNull);
        expect(json['payload']['clientVersion'], isNotEmpty);
        expect(json['payload']['platform'], isNotEmpty);
      });

      test('InputPaddleTarget toJson', () {
        final msg = InputPaddleTarget(
          requestId: 'req_101',
          sentAt: 1710000040033,
          matchId: 'match_123',
          clientSeq: 2,
          targetX: 0.64,
        );
        final json = msg.toJson();
        expect(json['type'], 'input.paddle_target');
        expect(json['payload']['matchId'], 'match_123');
        expect(json['payload']['clientSeq'], 2);
        expect(json['payload']['targetX'], closeTo(0.64, 0.0001));
      });

      test('RoomCreate toJson', () {
        final msg = RoomCreate(requestId: 'req_010', sentAt: 1710000010000);
        final json = msg.toJson();
        expect(json['type'], 'room.create');
        expect(json['payload']['settings'], isA<Map>());
      });

      test('RoomJoin toJson', () {
        final msg = RoomJoin(
          requestId: 'req_011',
          sentAt: 1710000015000,
          roomCode: '842913',
        );
        final json = msg.toJson();
        expect(json['type'], 'room.join');
        expect(json['payload']['roomCode'], '842913');
      });

      test('RoomReady toJson', () {
        final msg = RoomReady(
          requestId: 'req_012',
          sentAt: 1710000020000,
          ready: true,
        );
        final json = msg.toJson();
        expect(json['type'], 'room.ready');
        expect(json['payload']['ready'], true);
      });

      test('RoomLeave toJson', () {
        final msg = RoomLeave(requestId: 'req_013', sentAt: 1710000025000);
        final json = msg.toJson();
        expect(json['type'], 'room.leave');
      });

      test('MatchReconnect toJson', () {
        final msg = MatchReconnect(
          requestId: 'req_200',
          sentAt: 1710000060000,
          sessionToken: 'tok',
          matchId: 'match_123',
        );
        final json = msg.toJson();
        expect(json['type'], 'match.reconnect');
        expect(json['payload']['sessionToken'], 'tok');
        expect(json['payload']['matchId'], 'match_123');
      });

      test('ClientPong toJson', () {
        final msg = ClientPong(sentAt: 1710000005000, pingId: 'ping_123');
        final json = msg.toJson();
        expect(json['type'], 'client.pong');
        expect(json['payload']['pingId'], 'ping_123');
      });

      test('RematchRequest toJson', () {
        final msg = RematchRequest(
          requestId: 'req_300',
          sentAt: 1710000100000,
          matchId: 'match_123',
        );
        final json = msg.toJson();
        expect(json['type'], 'rematch.request');
        expect(json['payload']['matchId'], 'match_123');
      });
    });

    group('ServerMessage parsing', () {
      ServerMessage parse(Map<String, dynamic> raw) =>
          ServerMessage.fromJson(raw);

      test('ServerHello fromJson', () {
        final raw = {
          'type': 'server.hello',
          'serverTime': 1710000000030,
          'payload': {
            'sessionId': 'sess_abc',
            'playerId': 'player_123',
            'sessionToken': 'opaque-token',
            'heartbeatIntervalMs': 10000,
          },
        };
        final msg = parse(raw) as ServerHello;
        expect(msg.serverTime, 1710000000030);
        expect(msg.sessionId, 'sess_abc');
        expect(msg.playerId, 'player_123');
        expect(msg.sessionToken, 'opaque-token');
        expect(msg.heartbeatIntervalMs, 10000);
      });

      test('RoomCreated fromJson', () {
        final raw = {
          'type': 'room.created',
          'serverTime': 1710000010050,
          'payload': {
            'roomId': 'room_123',
            'roomCode': '842913',
            'hostPlayerId': 'player_123',
            'settings': {
              'pointsToWin': 5,
              'paddleSpeed': 0.9,
              'ballSpeed': 0.55,
            },
          },
        };
        final msg = parse(raw) as RoomCreated;
        expect(msg.roomCode, '842913');
        expect(msg.hostPlayerId, 'player_123');
        expect(msg.settings['pointsToWin'], 5);
      });

      test('RoomUpdated fromJson', () {
        final raw = {
          'type': 'room.updated',
          'serverTime': 1710000015100,
          'payload': {
            'roomId': 'room_123',
            'roomCode': '842913',
            'status': 'waiting',
            'players': [
              {
                'playerId': 'player_123',
                'displayName': 'Host',
                'role': 'host',
                'ready': false,
                'connected': true,
              },
            ],
            'settings': {'pointsToWin': 5, 'paddleSpeed': 0.9, 'ballSpeed': 0.55},
          },
        };
        final msg = parse(raw) as RoomUpdated;
        expect(msg.roomCode, '842913');
        expect(msg.players.length, 1);
        expect(msg.players[0].displayName, 'Host');
        expect(msg.players[0].ready, false);
      });

      test('MatchCountdown fromJson', () {
        final raw = {
          'type': 'match.countdown',
          'serverTime': 1710000030000,
          'payload': {
            'matchId': 'match_123',
            'startsAt': 1710000033000,
            'durationMs': 3000,
          },
        };
        final msg = parse(raw) as MatchCountdown;
        expect(msg.matchId, 'match_123');
        expect(msg.durationMs, 3000);
      });

      test('MatchStarted fromJson', () {
        final raw = {
          'type': 'match.started',
          'serverTime': 1710000033000,
          'serverTick': 0,
          'payload': {
            'matchId': 'match_123',
            'playerSlot': 'p1',
            'opponentSlot': 'p2',
            'settings': {'pointsToWin': 5, 'tickRate': 30, 'snapshotRate': 20},
            'initialState': {
              'ball': {'x': 0.5, 'y': 0.5, 'vx': 0.24, 'vy': -0.5},
              'players': {
                'p1': {'playerId': 'player_123', 'paddleX': 0.5, 'score': 0},
                'p2': {'playerId': 'player_456', 'paddleX': 0.5, 'score': 0},
              },
            },
          },
        };
        final msg = parse(raw) as MatchStarted;
        expect(msg.matchId, 'match_123');
        expect(msg.playerSlot, 'p1');
        expect(msg.serverTick, 0);
        expect(msg.initialState.ball.x, closeTo(0.5, 0.0001));
      });

      test('MatchSnapshot fromJson', () {
        final raw = {
          'type': 'match.snapshot',
          'serverTime': 1710000040100,
          'serverTick': 24,
          'payload': {
            'matchId': 'match_123',
            'ball': {'x': 0.482, 'y': 0.318, 'vx': 0.24, 'vy': -0.5},
            'players': {
              'p1': {'paddleX': 0.42, 'score': 0},
              'p2': {'paddleX': 0.57, 'score': 0},
            },
          },
        };
        final msg = parse(raw) as MatchSnapshot;
        expect(msg.serverTime, 1710000040100);
        expect(msg.serverTick, 24);
        expect(msg.ball.x, closeTo(0.482, 0.0001));
        expect(msg.p1PaddleX, closeTo(0.42, 0.0001));
        expect(msg.p2PaddleX, closeTo(0.57, 0.0001));
      });

      test('MatchScore fromJson', () {
        final raw = {
          'type': 'match.score',
          'serverTime': 1710000050000,
          'serverTick': 180,
          'payload': {
            'matchId': 'match_123',
            'scoringSlot': 'p1',
            'score': {'p1': 1, 'p2': 0},
            'nextRallyStartsAt': 1710000053000,
          },
        };
        final msg = parse(raw) as MatchScore;
        expect(msg.scoringSlot, 'p1');
        expect(msg.p1Score, 1);
        expect(msg.p2Score, 0);
      });

      test('MatchRallyReset fromJson', () {
        final raw = {
          'type': 'match.rally_reset',
          'serverTime': 1710000053000,
          'serverTick': 181,
          'payload': {
            'matchId': 'match_123',
            'ball': {'x': 0.5, 'y': 0.5, 'vx': -0.2, 'vy': 0.55},
          },
        };
        final msg = parse(raw) as MatchRallyReset;
        expect(msg.ball.vx, closeTo(-0.2, 0.0001));
      });

      test('MatchEnded fromJson', () {
        final raw = {
          'type': 'match.ended',
          'serverTime': 1710000090000,
          'serverTick': 1200,
          'payload': {
            'matchId': 'match_123',
            'winnerSlot': 'p1',
            'reason': 'points_to_win',
            'finalScore': {'p1': 5, 'p2': 2},
          },
        };
        final msg = parse(raw) as MatchEnded;
        expect(msg.winnerSlot, 'p1');
        expect(msg.p1FinalScore, 5);
        expect(msg.p2FinalScore, 2);
        expect(msg.reason, 'points_to_win');
      });

      test('PlayerDisconnected fromJson', () {
        final raw = {
          'type': 'player.disconnected',
          'serverTime': 1710000065000,
          'payload': {
            'matchId': 'match_123',
            'slot': 'p2',
            'reconnectDeadline': 1710000075000,
          },
        };
        final msg = parse(raw) as PlayerDisconnected;
        expect(msg.slot, 'p2');
        expect(msg.reconnectDeadline, 1710000075000);
      });

      test('PlayerReconnected fromJson', () {
        final raw = {
          'type': 'player.reconnected',
          'serverTime': 1710000069000,
          'payload': {'matchId': 'match_123', 'slot': 'p2'},
        };
        final msg = parse(raw) as PlayerReconnected;
        expect(msg.slot, 'p2');
      });

      test('MatchReconnected fromJson', () {
        final raw = {
          'type': 'match.reconnected',
          'serverTime': 1710000060100,
          'serverTick': 540,
          'payload': {
            'matchId': 'match_123',
            'slot': 'p2',
            'currentState': {
              'ball': {'x': 0.35, 'y': 0.41, 'vx': -0.19, 'vy': 0.52},
              'players': {
                'p1': {'paddleX': 0.48, 'score': 2},
                'p2': {'paddleX': 0.62, 'score': 1},
              },
            },
          },
        };
        final msg = parse(raw) as MatchReconnected;
        expect(msg.slot, 'p2');
        expect(msg.currentState.ball.x, closeTo(0.35, 0.0001));
      });

      test('ServerPing fromJson', () {
        final raw = {
          'type': 'server.ping',
          'serverTime': 1710000004000,
          'payload': {'pingId': 'ping_123'},
        };
        final msg = parse(raw) as ServerPing;
        expect(msg.pingId, 'ping_123');
      });

      test('RematchUpdated fromJson', () {
        final raw = {
          'type': 'rematch.updated',
          'serverTime': 1710000100100,
          'payload': {
            'previousMatchId': 'match_123',
            'players': {
              'p1': {'wantsRematch': true},
              'p2': {'wantsRematch': false},
            },
          },
        };
        final msg = parse(raw) as RematchUpdated;
        expect(msg.previousMatchId, 'match_123');
        expect(msg.p1WantsRematch, true);
        expect(msg.p2WantsRematch, false);
      });

      test('RematchStarted fromJson', () {
        final raw = {
          'type': 'rematch.started',
          'serverTime': 1710000105000,
          'payload': {
            'previousMatchId': 'match_123',
            'newMatchId': 'match_456',
          },
        };
        final msg = parse(raw) as RematchStarted;
        expect(msg.newMatchId, 'match_456');
      });

      test('ServerError fromJson', () {
        final raw = {
          'type': 'error',
          'serverTime': 1710000000100,
          'payload': {
            'code': 'room_not_found',
            'message': 'Room not found.',
            'requestId': 'req_123',
            'recoverable': true,
          },
        };
        final msg = parse(raw) as ServerError;
        expect(msg.code, 'room_not_found');
        expect(msg.recoverable, true);
      });

      test('unknown type throws UnknownMessageTypeException', () {
        final raw = {
          'type': 'banana.sandwich',
          'serverTime': 0,
          'payload': {},
        };
        expect(() => parse(raw), throwsA(isA<UnknownMessageTypeException>()));
      });
    });
  }
  ```

- [ ] **2.2 Run test — expect failure**

  ```sh
  cd client && flutter test test/network/protocol_test.dart -v
  ```

  Expected: compile error (file not found).

- [ ] **2.3 Create `client/lib/network/protocol.dart`**

  ```dart
  // lib/network/protocol.dart
  import 'dart:io' show Platform;

  // ─────────────────────────────────────────────
  // Shared data models
  // ─────────────────────────────────────────────

  class BallState {
    final double x, y, vx, vy;
    const BallState({
      required this.x,
      required this.y,
      required this.vx,
      required this.vy,
    });
    factory BallState.fromJson(Map<String, dynamic> j) => BallState(
      x: (j['x'] as num).toDouble(),
      y: (j['y'] as num).toDouble(),
      vx: (j['vx'] as num).toDouble(),
      vy: (j['vy'] as num).toDouble(),
    );
  }

  class MatchStateSnapshot {
    final BallState ball;
    final double p1PaddleX;
    final int p1Score;
    final double p2PaddleX;
    final int p2Score;
    const MatchStateSnapshot({
      required this.ball,
      required this.p1PaddleX,
      required this.p1Score,
      required this.p2PaddleX,
      required this.p2Score,
    });
    factory MatchStateSnapshot.fromJson(Map<String, dynamic> j) {
      final players = j['players'] as Map<String, dynamic>;
      final p1 = players['p1'] as Map<String, dynamic>;
      final p2 = players['p2'] as Map<String, dynamic>;
      return MatchStateSnapshot(
        ball: BallState.fromJson(j['ball'] as Map<String, dynamic>),
        p1PaddleX: (p1['paddleX'] as num).toDouble(),
        p1Score: (p1['score'] as num).toInt(),
        p2PaddleX: (p2['paddleX'] as num).toDouble(),
        p2Score: (p2['score'] as num).toInt(),
      );
    }
  }

  class RoomPlayer {
    final String playerId;
    final String displayName;
    final String role; // 'host' | 'guest'
    final bool ready;
    final bool connected;
    const RoomPlayer({
      required this.playerId,
      required this.displayName,
      required this.role,
      required this.ready,
      required this.connected,
    });
    factory RoomPlayer.fromJson(Map<String, dynamic> j) => RoomPlayer(
      playerId: j['playerId'] as String,
      displayName: j['displayName'] as String,
      role: j['role'] as String,
      ready: j['ready'] as bool,
      connected: j['connected'] as bool,
    );
  }

  class MatchInitialState {
    final BallState ball;
    final double p1PaddleX;
    final int p1Score;
    final double p2PaddleX;
    final int p2Score;
    const MatchInitialState({
      required this.ball,
      required this.p1PaddleX,
      required this.p1Score,
      required this.p2PaddleX,
      required this.p2Score,
    });
    factory MatchInitialState.fromJson(Map<String, dynamic> j) {
      final players = j['players'] as Map<String, dynamic>;
      final p1 = players['p1'] as Map<String, dynamic>;
      final p2 = players['p2'] as Map<String, dynamic>;
      return MatchInitialState(
        ball: BallState.fromJson(j['ball'] as Map<String, dynamic>),
        p1PaddleX: (p1['paddleX'] as num).toDouble(),
        p1Score: (p1['score'] as num).toInt(),
        p2PaddleX: (p2['paddleX'] as num).toDouble(),
        p2Score: (p2['score'] as num).toInt(),
      );
    }
  }

  // ─────────────────────────────────────────────
  // Client → Server messages
  // ─────────────────────────────────────────────

  abstract class ClientMessage {
    Map<String, dynamic> toJson();
  }

  String _platform() {
    try {
      if (Platform.isAndroid) return 'android';
      if (Platform.isIOS) return 'ios';
    } catch (_) {}
    return 'unknown';
  }

  class ClientHello implements ClientMessage {
    final String requestId;
    final int sentAt;
    final String displayName;
    final String? sessionToken;
    const ClientHello({
      required this.requestId,
      required this.sentAt,
      required this.displayName,
      this.sessionToken,
    });
    @override
    Map<String, dynamic> toJson() => {
      'type': 'client.hello',
      'requestId': requestId,
      'sentAt': sentAt,
      'payload': {
        'clientVersion': '0.1.0',
        'platform': _platform(),
        'sessionToken': sessionToken,
        'displayName': displayName,
      },
    };
  }

  class RoomCreate implements ClientMessage {
    final String requestId;
    final int sentAt;
    const RoomCreate({required this.requestId, required this.sentAt});
    @override
    Map<String, dynamic> toJson() => {
      'type': 'room.create',
      'requestId': requestId,
      'sentAt': sentAt,
      'payload': {
        'settings': {'pointsToWin': 5, 'paddleSpeed': 0.9, 'ballSpeed': 0.55},
      },
    };
  }

  class RoomJoin implements ClientMessage {
    final String requestId;
    final int sentAt;
    final String roomCode;
    const RoomJoin({
      required this.requestId,
      required this.sentAt,
      required this.roomCode,
    });
    @override
    Map<String, dynamic> toJson() => {
      'type': 'room.join',
      'requestId': requestId,
      'sentAt': sentAt,
      'payload': {'roomCode': roomCode},
    };
  }

  class RoomReady implements ClientMessage {
    final String requestId;
    final int sentAt;
    final bool ready;
    const RoomReady({
      required this.requestId,
      required this.sentAt,
      required this.ready,
    });
    @override
    Map<String, dynamic> toJson() => {
      'type': 'room.ready',
      'requestId': requestId,
      'sentAt': sentAt,
      'payload': {'ready': ready},
    };
  }

  class RoomLeave implements ClientMessage {
    final String requestId;
    final int sentAt;
    const RoomLeave({required this.requestId, required this.sentAt});
    @override
    Map<String, dynamic> toJson() => {
      'type': 'room.leave',
      'requestId': requestId,
      'sentAt': sentAt,
      'payload': {},
    };
  }

  class InputPaddleTarget implements ClientMessage {
    final String requestId;
    final int sentAt;
    final String matchId;
    final int clientSeq;
    final double targetX;
    const InputPaddleTarget({
      required this.requestId,
      required this.sentAt,
      required this.matchId,
      required this.clientSeq,
      required this.targetX,
    });
    @override
    Map<String, dynamic> toJson() => {
      'type': 'input.paddle_target',
      'requestId': requestId,
      'sentAt': sentAt,
      'payload': {
        'matchId': matchId,
        'clientSeq': clientSeq,
        'targetX': targetX,
      },
    };
  }

  class MatchReconnect implements ClientMessage {
    final String requestId;
    final int sentAt;
    final String sessionToken;
    final String matchId;
    const MatchReconnect({
      required this.requestId,
      required this.sentAt,
      required this.sessionToken,
      required this.matchId,
    });
    @override
    Map<String, dynamic> toJson() => {
      'type': 'match.reconnect',
      'requestId': requestId,
      'sentAt': sentAt,
      'payload': {'sessionToken': sessionToken, 'matchId': matchId},
    };
  }

  class ClientPong implements ClientMessage {
    final int sentAt;
    final String pingId;
    const ClientPong({required this.sentAt, required this.pingId});
    @override
    Map<String, dynamic> toJson() => {
      'type': 'client.pong',
      'sentAt': sentAt,
      'payload': {'pingId': pingId},
    };
  }

  class RematchRequest implements ClientMessage {
    final String requestId;
    final int sentAt;
    final String matchId;
    const RematchRequest({
      required this.requestId,
      required this.sentAt,
      required this.matchId,
    });
    @override
    Map<String, dynamic> toJson() => {
      'type': 'rematch.request',
      'requestId': requestId,
      'sentAt': sentAt,
      'payload': {'matchId': matchId},
    };
  }

  // ─────────────────────────────────────────────
  // Server → Client messages
  // ─────────────────────────────────────────────

  class UnknownMessageTypeException implements Exception {
    final String type;
    UnknownMessageTypeException(this.type);
    @override
    String toString() => 'UnknownMessageTypeException: "$type"';
  }

  abstract class ServerMessage {
    final int serverTime;
    const ServerMessage({required this.serverTime});

    factory ServerMessage.fromJson(Map<String, dynamic> j) {
      final type = j['type'] as String;
      final serverTime = (j['serverTime'] as num).toInt();
      final payload = j['payload'] as Map<String, dynamic>;
      final serverTick =
          j['serverTick'] != null ? (j['serverTick'] as num).toInt() : null;

      switch (type) {
        case 'server.hello':
          return ServerHello(
            serverTime: serverTime,
            sessionId: payload['sessionId'] as String,
            playerId: payload['playerId'] as String,
            sessionToken: payload['sessionToken'] as String,
            heartbeatIntervalMs: (payload['heartbeatIntervalMs'] as num).toInt(),
          );
        case 'room.created':
          return RoomCreated(
            serverTime: serverTime,
            roomId: payload['roomId'] as String,
            roomCode: payload['roomCode'] as String,
            hostPlayerId: payload['hostPlayerId'] as String,
            settings: Map<String, dynamic>.from(
              payload['settings'] as Map,
            ),
          );
        case 'room.updated':
          return RoomUpdated(
            serverTime: serverTime,
            roomId: payload['roomId'] as String,
            roomCode: payload['roomCode'] as String,
            status: payload['status'] as String,
            players: (payload['players'] as List)
                .map((p) => RoomPlayer.fromJson(p as Map<String, dynamic>))
                .toList(),
            settings: Map<String, dynamic>.from(
              payload['settings'] as Map,
            ),
          );
        case 'match.countdown':
          return MatchCountdown(
            serverTime: serverTime,
            matchId: payload['matchId'] as String,
            startsAt: (payload['startsAt'] as num).toInt(),
            durationMs: (payload['durationMs'] as num).toInt(),
          );
        case 'match.started':
          return MatchStarted(
            serverTime: serverTime,
            serverTick: serverTick ?? 0,
            matchId: payload['matchId'] as String,
            playerSlot: payload['playerSlot'] as String,
            opponentSlot: payload['opponentSlot'] as String,
            settings: Map<String, dynamic>.from(
              payload['settings'] as Map,
            ),
            initialState: MatchInitialState.fromJson(
              payload['initialState'] as Map<String, dynamic>,
            ),
          );
        case 'match.snapshot':
          final players = payload['players'] as Map<String, dynamic>;
          final p1 = players['p1'] as Map<String, dynamic>;
          final p2 = players['p2'] as Map<String, dynamic>;
          return MatchSnapshot(
            serverTime: serverTime,
            serverTick: serverTick ?? 0,
            matchId: payload['matchId'] as String,
            ball: BallState.fromJson(payload['ball'] as Map<String, dynamic>),
            p1PaddleX: (p1['paddleX'] as num).toDouble(),
            p1Score: (p1['score'] as num).toInt(),
            p2PaddleX: (p2['paddleX'] as num).toDouble(),
            p2Score: (p2['score'] as num).toInt(),
          );
        case 'match.score':
          final score = payload['score'] as Map<String, dynamic>;
          return MatchScore(
            serverTime: serverTime,
            serverTick: serverTick ?? 0,
            matchId: payload['matchId'] as String,
            scoringSlot: payload['scoringSlot'] as String,
            p1Score: (score['p1'] as num).toInt(),
            p2Score: (score['p2'] as num).toInt(),
            nextRallyStartsAt: (payload['nextRallyStartsAt'] as num).toInt(),
          );
        case 'match.rally_reset':
          return MatchRallyReset(
            serverTime: serverTime,
            serverTick: serverTick ?? 0,
            matchId: payload['matchId'] as String,
            ball: BallState.fromJson(payload['ball'] as Map<String, dynamic>),
          );
        case 'match.ended':
          final finalScore = payload['finalScore'] as Map<String, dynamic>;
          return MatchEnded(
            serverTime: serverTime,
            serverTick: serverTick ?? 0,
            matchId: payload['matchId'] as String,
            winnerSlot: payload['winnerSlot'] as String,
            reason: payload['reason'] as String,
            p1FinalScore: (finalScore['p1'] as num).toInt(),
            p2FinalScore: (finalScore['p2'] as num).toInt(),
          );
        case 'player.disconnected':
          return PlayerDisconnected(
            serverTime: serverTime,
            matchId: payload['matchId'] as String,
            slot: payload['slot'] as String,
            reconnectDeadline: (payload['reconnectDeadline'] as num).toInt(),
          );
        case 'player.reconnected':
          return PlayerReconnected(
            serverTime: serverTime,
            matchId: payload['matchId'] as String,
            slot: payload['slot'] as String,
          );
        case 'match.reconnected':
          return MatchReconnected(
            serverTime: serverTime,
            serverTick: serverTick ?? 0,
            matchId: payload['matchId'] as String,
            slot: payload['slot'] as String,
            currentState: MatchStateSnapshot.fromJson(
              payload['currentState'] as Map<String, dynamic>,
            ),
          );
        case 'server.ping':
          return ServerPing(
            serverTime: serverTime,
            pingId: payload['pingId'] as String,
          );
        case 'rematch.updated':
          final players = payload['players'] as Map<String, dynamic>;
          final p1 = players['p1'] as Map<String, dynamic>;
          final p2 = players['p2'] as Map<String, dynamic>;
          return RematchUpdated(
            serverTime: serverTime,
            previousMatchId: payload['previousMatchId'] as String,
            p1WantsRematch: p1['wantsRematch'] as bool,
            p2WantsRematch: p2['wantsRematch'] as bool,
          );
        case 'rematch.started':
          return RematchStarted(
            serverTime: serverTime,
            previousMatchId: payload['previousMatchId'] as String,
            newMatchId: payload['newMatchId'] as String,
          );
        case 'error':
          return ServerError(
            serverTime: serverTime,
            code: payload['code'] as String,
            message: payload['message'] as String,
            requestId: payload['requestId'] as String?,
            recoverable: payload['recoverable'] as bool,
          );
        default:
          throw UnknownMessageTypeException(type);
      }
    }
  }

  class ServerHello extends ServerMessage {
    final String sessionId;
    final String playerId;
    final String sessionToken;
    final int heartbeatIntervalMs;
    const ServerHello({
      required super.serverTime,
      required this.sessionId,
      required this.playerId,
      required this.sessionToken,
      required this.heartbeatIntervalMs,
    });
  }

  class RoomCreated extends ServerMessage {
    final String roomId;
    final String roomCode;
    final String hostPlayerId;
    final Map<String, dynamic> settings;
    const RoomCreated({
      required super.serverTime,
      required this.roomId,
      required this.roomCode,
      required this.hostPlayerId,
      required this.settings,
    });
  }

  class RoomUpdated extends ServerMessage {
    final String roomId;
    final String roomCode;
    final String status;
    final List<RoomPlayer> players;
    final Map<String, dynamic> settings;
    const RoomUpdated({
      required super.serverTime,
      required this.roomId,
      required this.roomCode,
      required this.status,
      required this.players,
      required this.settings,
    });
  }

  class MatchCountdown extends ServerMessage {
    final String matchId;
    final int startsAt;
    final int durationMs;
    const MatchCountdown({
      required super.serverTime,
      required this.matchId,
      required this.startsAt,
      required this.durationMs,
    });
  }

  class MatchStarted extends ServerMessage {
    final int serverTick;
    final String matchId;
    final String playerSlot;
    final String opponentSlot;
    final Map<String, dynamic> settings;
    final MatchInitialState initialState;
    const MatchStarted({
      required super.serverTime,
      required this.serverTick,
      required this.matchId,
      required this.playerSlot,
      required this.opponentSlot,
      required this.settings,
      required this.initialState,
    });
  }

  class MatchSnapshot extends ServerMessage {
    final int serverTick;
    final String matchId;
    final BallState ball;
    final double p1PaddleX;
    final int p1Score;
    final double p2PaddleX;
    final int p2Score;
    const MatchSnapshot({
      required super.serverTime,
      required this.serverTick,
      required this.matchId,
      required this.ball,
      required this.p1PaddleX,
      required this.p1Score,
      required this.p2PaddleX,
      required this.p2Score,
    });
  }

  class MatchScore extends ServerMessage {
    final int serverTick;
    final String matchId;
    final String scoringSlot;
    final int p1Score;
    final int p2Score;
    final int nextRallyStartsAt;
    const MatchScore({
      required super.serverTime,
      required this.serverTick,
      required this.matchId,
      required this.scoringSlot,
      required this.p1Score,
      required this.p2Score,
      required this.nextRallyStartsAt,
    });
  }

  class MatchRallyReset extends ServerMessage {
    final int serverTick;
    final String matchId;
    final BallState ball;
    const MatchRallyReset({
      required super.serverTime,
      required this.serverTick,
      required this.matchId,
      required this.ball,
    });
  }

  class MatchEnded extends ServerMessage {
    final int serverTick;
    final String matchId;
    final String winnerSlot;
    final String reason;
    final int p1FinalScore;
    final int p2FinalScore;
    const MatchEnded({
      required super.serverTime,
      required this.serverTick,
      required this.matchId,
      required this.winnerSlot,
      required this.reason,
      required this.p1FinalScore,
      required this.p2FinalScore,
    });
  }

  class PlayerDisconnected extends ServerMessage {
    final String matchId;
    final String slot;
    final int reconnectDeadline;
    const PlayerDisconnected({
      required super.serverTime,
      required this.matchId,
      required this.slot,
      required this.reconnectDeadline,
    });
  }

  class PlayerReconnected extends ServerMessage {
    final String matchId;
    final String slot;
    const PlayerReconnected({
      required super.serverTime,
      required this.matchId,
      required this.slot,
    });
  }

  class MatchReconnected extends ServerMessage {
    final int serverTick;
    final String matchId;
    final String slot;
    final MatchStateSnapshot currentState;
    const MatchReconnected({
      required super.serverTime,
      required this.serverTick,
      required this.matchId,
      required this.slot,
      required this.currentState,
    });
  }

  class ServerPing extends ServerMessage {
    final String pingId;
    const ServerPing({required super.serverTime, required this.pingId});
  }

  class RematchUpdated extends ServerMessage {
    final String previousMatchId;
    final bool p1WantsRematch;
    final bool p2WantsRematch;
    const RematchUpdated({
      required super.serverTime,
      required this.previousMatchId,
      required this.p1WantsRematch,
      required this.p2WantsRematch,
    });
  }

  class RematchStarted extends ServerMessage {
    final String previousMatchId;
    final String newMatchId;
    const RematchStarted({
      required super.serverTime,
      required this.previousMatchId,
      required this.newMatchId,
    });
  }

  class ServerError extends ServerMessage {
    final String code;
    final String message;
    final String? requestId;
    final bool recoverable;
    const ServerError({
      required super.serverTime,
      required this.code,
      required this.message,
      this.requestId,
      required this.recoverable,
    });
  }
  ```

- [ ] **2.4 Run test — expect pass**

  ```sh
  cd client && flutter test test/network/protocol_test.dart -v
  ```

- [ ] **2.5 Run analyze**

  ```sh
  cd client && flutter analyze
  ```

- [ ] **2.6 Commit**

  ```sh
  git add client/lib/network/protocol.dart client/test/network/protocol_test.dart
  git commit -m "feat(client): typed protocol message classes with fromJson/toJson"
  ```

---

## Task 3: WebSocket Client + Session Store

**What this task delivers:** `VolleyClient` — a class that opens a WebSocket connection, parses inbound JSON into `ServerMessage` objects, exposes a `Stream<ServerMessage>`, and allows sending `ClientMessage` objects. Also `SessionStore` — a `SharedPreferences` wrapper for persisting the session token.

### Steps

- [ ] **3.1 Write the failing test**

  Create `client/test/network/websocket_client_test.dart`:

  ```dart
  import 'dart:async';
  import 'dart:convert';
  import 'package:flutter_test/flutter_test.dart';
  import 'package:volley/network/protocol.dart';
  import 'package:volley/network/websocket_client.dart';

  // Minimal fake channel that replays messages and captures sends.
  class FakeWebSocketSink implements WebSocketSink {
    final List<String> sent = [];
    @override
    void add(dynamic data) => sent.add(data as String);
    @override
    Future<void> close([int? code, String? reason]) async {}
    @override
    Future get done => Future.value();
    @override
    void addError(Object error, [StackTrace? stackTrace]) {}
    @override
    Future addStream(Stream stream) async {}
  }

  class FakeWebSocketChannel implements WebSocketChannel {
    final StreamController<dynamic> _controller = StreamController<dynamic>();
    final FakeWebSocketSink _sink = FakeWebSocketSink();

    void injectRaw(String json) => _controller.add(json);
    List<String> get sent => _sink.sent;

    @override
    Stream get stream => _controller.stream;

    @override
    WebSocketSink get sink => _sink;

    @override
    Future<void> get ready => Future.value();

    @override
    int? get closeCode => null;

    @override
    String? get closeReason => null;

    @override
    String? get protocol => null;
  }

  void main() {
    group('VolleyClient', () {
      late FakeWebSocketChannel fakeChannel;
      late VolleyClient client;

      setUp(() {
        fakeChannel = FakeWebSocketChannel();
        client = VolleyClient.fromChannel(fakeChannel);
      });

      tearDown(() => client.dispose());

      test('parses server.hello into ServerHello and emits on stream', () async {
        final raw = jsonEncode({
          'type': 'server.hello',
          'serverTime': 1710000000030,
          'payload': {
            'sessionId': 'sess_abc',
            'playerId': 'player_123',
            'sessionToken': 'opaque-token',
            'heartbeatIntervalMs': 10000,
          },
        });

        final future = client.stream.first;
        fakeChannel.injectRaw(raw);
        final msg = await future;

        expect(msg, isA<ServerHello>());
        final hello = msg as ServerHello;
        expect(hello.sessionToken, 'opaque-token');
      });

      test('parses match.snapshot into MatchSnapshot', () async {
        final raw = jsonEncode({
          'type': 'match.snapshot',
          'serverTime': 1710000040100,
          'serverTick': 24,
          'payload': {
            'matchId': 'match_123',
            'ball': {'x': 0.482, 'y': 0.318, 'vx': 0.24, 'vy': -0.5},
            'players': {
              'p1': {'paddleX': 0.42, 'score': 0},
              'p2': {'paddleX': 0.57, 'score': 0},
            },
          },
        });

        final future = client.stream.first;
        fakeChannel.injectRaw(raw);
        final msg = await future;

        expect(msg, isA<MatchSnapshot>());
      });

      test('send serializes ClientMessage to JSON string on the sink', () {
        final msg = ClientHello(
          requestId: 'req_001',
          sentAt: 1710000000000,
          displayName: 'Alice',
          sessionToken: null,
        );
        client.send(msg);

        expect(fakeChannel.sent.length, 1);
        final decoded = jsonDecode(fakeChannel.sent[0]) as Map<String, dynamic>;
        expect(decoded['type'], 'client.hello');
        expect(decoded['payload']['displayName'], 'Alice');
      });

      test('unknown message type is silently dropped (no crash)', () async {
        // Inject bad type, then a valid one — only the valid one should appear.
        final bad = jsonEncode({
          'type': 'banana.sandwich',
          'serverTime': 0,
          'payload': {},
        });
        final good = jsonEncode({
          'type': 'server.ping',
          'serverTime': 1,
          'payload': {'pingId': 'p1'},
        });

        final completer = Completer<ServerMessage>();
        client.stream.first.then(completer.complete);

        fakeChannel.injectRaw(bad);
        fakeChannel.injectRaw(good);

        final msg = await completer.future;
        expect(msg, isA<ServerPing>());
      });

      test('dispose closes the stream', () async {
        final done = client.stream.drain<void>();
        client.dispose();
        await done; // should complete without error
      });
    });
  }
  ```

  > **Note on imports:** The test imports `WebSocketChannel` and `WebSocketSink` from `package:web_socket_channel/web_socket_channel.dart`. The `FakeWebSocketChannel` implements the `WebSocketChannel` interface so `VolleyClient.fromChannel` accepts it without touching the network.

- [ ] **3.2 Run test — expect failure**

  ```sh
  cd client && flutter test test/network/websocket_client_test.dart -v
  ```

- [ ] **3.3 Create `client/lib/network/websocket_client.dart`**

  ```dart
  // lib/network/websocket_client.dart
  import 'dart:async';
  import 'dart:convert';
  import 'package:web_socket_channel/web_socket_channel.dart';
  import 'package:web_socket_channel/io.dart';
  import 'protocol.dart';

  /// Owns a single WebSocket connection to the Volley backend.
  ///
  /// Usage:
  ///   final client = VolleyClient();
  ///   await client.connect(kServerUrl);
  ///   client.stream.listen((msg) { ... });
  ///   client.send(ClientHello(...));
  ///   client.dispose();
  class VolleyClient {
    WebSocketChannel? _channel;
    final StreamController<ServerMessage> _controller =
        StreamController<ServerMessage>.broadcast();

    StreamSubscription<dynamic>? _sub;

    VolleyClient();

    /// For testing: inject a pre-built channel instead of opening a real socket.
    VolleyClient.fromChannel(WebSocketChannel channel) {
      _attach(channel);
    }

    /// Connects to [url] and begins listening.
    Future<void> connect(String url) async {
      final ch = IOWebSocketChannel.connect(Uri.parse(url));
      await ch.ready;
      _attach(ch);
    }

    void _attach(WebSocketChannel channel) {
      _channel = channel;
      _sub = channel.stream.listen(
        _onData,
        onError: _controller.addError,
        onDone: _controller.close,
      );
    }

    void _onData(dynamic raw) {
      try {
        final json = jsonDecode(raw as String) as Map<String, dynamic>;
        final msg = ServerMessage.fromJson(json);
        _controller.add(msg);
      } on UnknownMessageTypeException {
        // Silently ignore unknown message types — forward-compatibility.
      } catch (e, st) {
        // Malformed JSON or unexpected parse error — log but don't crash.
        // ignore: avoid_print
        print('[VolleyClient] parse error: $e\n$st');
      }
    }

    /// Sends a [ClientMessage] as JSON over the socket.
    void send(ClientMessage msg) {
      _channel?.sink.add(jsonEncode(msg.toJson()));
    }

    /// Broadcast stream of typed server messages.
    Stream<ServerMessage> get stream => _controller.stream;

    void dispose() {
      _sub?.cancel();
      _channel?.sink.close();
      if (!_controller.isClosed) _controller.close();
    }
  }
  ```

- [ ] **3.4 Create `client/lib/storage/session_store.dart`**

  ```dart
  // lib/storage/session_store.dart
  import 'package:shared_preferences/shared_preferences.dart';

  const _kTokenKey = 'volley_session_token';

  class SessionStore {
    Future<void> saveToken(String token) async {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(_kTokenKey, token);
    }

    Future<String?> loadToken() async {
      final prefs = await SharedPreferences.getInstance();
      return prefs.getString(_kTokenKey);
    }

    Future<void> clear() async {
      final prefs = await SharedPreferences.getInstance();
      await prefs.remove(_kTokenKey);
    }
  }
  ```

- [ ] **3.5 Run test — expect pass**

  ```sh
  cd client && flutter test test/network/websocket_client_test.dart -v
  ```

- [ ] **3.6 Run analyze**

  ```sh
  cd client && flutter analyze
  ```

- [ ] **3.7 Commit**

  ```sh
  git add client/lib/network/websocket_client.dart client/lib/storage/session_store.dart client/test/network/websocket_client_test.dart
  git commit -m "feat(client): VolleyClient WebSocket wrapper and SessionStore"
  ```

---

## Task 4: Interpolation System

**What this task delivers:** `InterpolationBuffer` — a class that accepts `MatchSnapshot` objects, stores them in time order, and exposes an `interpolate(renderTimeMs)` method that returns a linearly interpolated `InterpolatedState`. Extrapolation is capped at 150ms beyond the latest snapshot; an empty buffer returns the last known state.

### Steps

- [ ] **4.1 Write the failing test**

  Create `client/test/game/interpolation_test.dart`:

  ```dart
  import 'package:flutter_test/flutter_test.dart';
  import 'package:volley/game/systems/interpolation.dart';
  import 'package:volley/network/protocol.dart';

  MatchSnapshot _snap(int serverTime, double bx, double by, double p1x, double p2x) {
    return MatchSnapshot(
      serverTime: serverTime,
      serverTick: serverTime ~/ 33,
      matchId: 'test',
      ball: BallState(x: bx, y: by, vx: 0.24, vy: -0.5),
      p1PaddleX: p1x,
      p1Score: 0,
      p2PaddleX: p2x,
      p2Score: 0,
    );
  }

  void main() {
    group('InterpolationBuffer', () {
      late InterpolationBuffer buffer;

      setUp(() => buffer = InterpolationBuffer());

      test('empty buffer returns null', () {
        expect(buffer.interpolate(1000), isNull);
      });

      test('single snapshot returns that snapshot as-is', () {
        buffer.addSnapshot(_snap(1000, 0.5, 0.5, 0.5, 0.5));
        final state = buffer.interpolate(1000);
        expect(state, isNotNull);
        expect(state!.ballX, closeTo(0.5, 0.0001));
      });

      test('interpolates halfway between two snapshots', () {
        buffer.addSnapshot(_snap(1000, 0.0, 0.0, 0.2, 0.8));
        buffer.addSnapshot(_snap(1050, 1.0, 1.0, 0.6, 0.4));
        // renderTime exactly halfway
        final state = buffer.interpolate(1025);
        expect(state, isNotNull);
        expect(state!.ballX, closeTo(0.5, 0.001));
        expect(state.ballY, closeTo(0.5, 0.001));
        expect(state.p1PaddleX, closeTo(0.4, 0.001));
        expect(state.p2PaddleX, closeTo(0.6, 0.001));
      });

      test('alpha=0 at snapshotA.time', () {
        buffer.addSnapshot(_snap(1000, 0.2, 0.3, 0.4, 0.6));
        buffer.addSnapshot(_snap(1050, 0.8, 0.7, 0.9, 0.1));
        final state = buffer.interpolate(1000);
        expect(state!.ballX, closeTo(0.2, 0.001));
      });

      test('alpha=1 at snapshotB.time', () {
        buffer.addSnapshot(_snap(1000, 0.2, 0.3, 0.4, 0.6));
        buffer.addSnapshot(_snap(1050, 0.8, 0.7, 0.9, 0.1));
        final state = buffer.interpolate(1050);
        expect(state!.ballX, closeTo(0.8, 0.001));
      });

      test('extrapolates up to 150ms beyond last snapshot', () {
        buffer.addSnapshot(_snap(1000, 0.5, 0.5, 0.5, 0.5));
        // 100ms after last snapshot → should extrapolate using velocity
        // vx=0.24, vy=-0.5, speed=0.55 in game units/s, but extrapolation
        // uses the raw vx/vy from the snapshot directly for simplicity.
        final state = buffer.interpolate(1100); // +100ms
        expect(state, isNotNull);
        // Ball should have moved in the direction of vx/vy
        expect(state!.ballX, greaterThan(0.5)); // vx positive
        expect(state.ballY, lessThan(0.5));    // vy negative
      });

      test('caps extrapolation at 150ms — returns clamped position', () {
        buffer.addSnapshot(_snap(1000, 0.5, 0.5, 0.5, 0.5));
        final at150 = buffer.interpolate(1150);
        final at300 = buffer.interpolate(1300); // 300ms — beyond cap
        expect(at150, isNotNull);
        expect(at300, isNotNull);
        // Beyond cap the ball should be frozen at the 150ms extrapolated position
        expect(at300!.ballX, closeTo(at150!.ballX, 0.0001));
        expect(at300.ballY, closeTo(at150.ballY, 0.0001));
      });

      test('old snapshots are pruned (buffer does not grow unboundedly)', () {
        for (int i = 0; i < 100; i++) {
          buffer.addSnapshot(_snap(i * 50, 0.5, 0.5, 0.5, 0.5));
        }
        expect(buffer.length, lessThanOrEqualTo(20));
      });
    });
  }
  ```

- [ ] **4.2 Run test — expect failure**

  ```sh
  cd client && flutter test test/game/interpolation_test.dart -v
  ```

- [ ] **4.3 Create `client/lib/game/systems/interpolation.dart`**

  ```dart
  // lib/game/systems/interpolation.dart
  import '../../network/protocol.dart';

  const _kMaxExtrapolationMs = 150;
  const _kMaxBufferSize = 20;

  /// Result of an interpolation/extrapolation query.
  class InterpolatedState {
    final double ballX;
    final double ballY;
    final double p1PaddleX;
    final double p2PaddleX;

    const InterpolatedState({
      required this.ballX,
      required this.ballY,
      required this.p1PaddleX,
      required this.p2PaddleX,
    });
  }

  double _lerp(double a, double b, double t) => a + (b - a) * t;

  /// Stores a sliding window of [MatchSnapshot]s and interpolates game state
  /// to a caller-supplied [renderTimeMs].
  ///
  /// [renderTimeMs] should be `estimatedServerTimeMs - 100`.
  class InterpolationBuffer {
    final List<MatchSnapshot> _buffer = [];

    int get length => _buffer.length;

    /// Insert a new snapshot. Snapshots are kept in ascending serverTime order
    /// and the buffer is pruned to [_kMaxBufferSize].
    void addSnapshot(MatchSnapshot snap) {
      _buffer.add(snap);
      _buffer.sort((a, b) => a.serverTime.compareTo(b.serverTime));
      if (_buffer.length > _kMaxBufferSize) {
        _buffer.removeRange(0, _buffer.length - _kMaxBufferSize);
      }
    }

    /// Returns the interpolated/extrapolated state for [renderTimeMs],
    /// or null if the buffer is empty.
    InterpolatedState? interpolate(int renderTimeMs) {
      if (_buffer.isEmpty) return null;

      // Find the pair of snapshots that straddle renderTimeMs.
      MatchSnapshot? before;
      MatchSnapshot? after;

      for (final snap in _buffer) {
        if (snap.serverTime <= renderTimeMs) {
          before = snap;
        } else {
          after ??= snap;
          break;
        }
      }

      // Case 1: exact interpolation between two snapshots.
      if (before != null && after != null) {
        final span = after.serverTime - before.serverTime;
        final t = span > 0
            ? (renderTimeMs - before.serverTime) / span
            : 0.0;
        return InterpolatedState(
          ballX: _lerp(before.ball.x, after.ball.x, t),
          ballY: _lerp(before.ball.y, after.ball.y, t),
          p1PaddleX: _lerp(before.p1PaddleX, after.p1PaddleX, t),
          p2PaddleX: _lerp(before.p2PaddleX, after.p2PaddleX, t),
        );
      }

      // Case 2: renderTime is ahead of all snapshots — extrapolate from latest.
      final latest = _buffer.last;
      final rawDeltaMs = renderTimeMs - latest.serverTime;
      final deltaMs = rawDeltaMs.clamp(0, _kMaxExtrapolationMs);
      final dt = deltaMs / 1000.0; // seconds

      // Use the ball's velocity from the snapshot for simple extrapolation.
      // vx/vy are normalized direction vectors; we apply ball speed (0.55 u/s)
      // from the game spec.
      const ballSpeed = 0.55;
      return InterpolatedState(
        ballX: (latest.ball.x + latest.ball.vx * ballSpeed * dt).clamp(0.0, 1.0),
        ballY: (latest.ball.y + latest.ball.vy * ballSpeed * dt).clamp(0.0, 1.0),
        p1PaddleX: latest.p1PaddleX,
        p2PaddleX: latest.p2PaddleX,
      );
    }

    void clear() => _buffer.clear();
  }
  ```

- [ ] **4.4 Run test — expect pass**

  ```sh
  cd client && flutter test test/game/interpolation_test.dart -v
  ```

- [ ] **4.5 Run analyze**

  ```sh
  cd client && flutter analyze
  ```

- [ ] **4.6 Commit**

  ```sh
  git add client/lib/game/systems/interpolation.dart client/test/game/interpolation_test.dart
  git commit -m "feat(client): snapshot interpolation buffer with extrapolation cap"
  ```

---

## Task 5: Prediction + Input Controller

**What this task delivers:** `PaddlePrediction` — tracks the local paddle's predicted position and reconciles against server snapshots. `InputController` — translates touch events into normalized `targetX` values with throttling.

### Steps

- [ ] **5.1 Write the failing tests**

  Create `client/test/game/prediction_test.dart`:

  ```dart
  import 'package:flutter_test/flutter_test.dart';
  import 'package:volley/game/systems/prediction.dart';

  void main() {
    group('PaddlePrediction', () {
      late PaddlePrediction pred;

      setUp(() => pred = PaddlePrediction(halfPaddleWidth: 0.11));

      test('initial predictedX is 0.5', () {
        expect(pred.predictedX, closeTo(0.5, 0.0001));
      });

      test('applyInput clamps to [halfPaddleWidth, 1-halfPaddleWidth]', () {
        pred.applyInput(0.0); // below min
        expect(pred.predictedX, closeTo(0.11, 0.0001));

        pred.applyInput(1.0); // above max
        expect(pred.predictedX, closeTo(0.89, 0.0001));

        pred.applyInput(0.5); // valid
        expect(pred.predictedX, closeTo(0.5, 0.0001));
      });

      test('reconcile snaps when server differs by more than 0.01', () {
        pred.applyInput(0.5);
        pred.reconcile(serverPaddleX: 0.7); // diff = 0.2 > 0.01 → snap
        expect(pred.predictedX, closeTo(0.7, 0.0001));
      });

      test('reconcile keeps local value when server differs by <= 0.01', () {
        pred.applyInput(0.5);
        pred.reconcile(serverPaddleX: 0.508); // diff = 0.008 <= 0.01 → keep
        expect(pred.predictedX, closeTo(0.5, 0.0001));
      });

      test('reconcile at exactly 0.01 keeps local value', () {
        pred.applyInput(0.5);
        pred.reconcile(serverPaddleX: 0.51); // diff == 0.01 → keep
        expect(pred.predictedX, closeTo(0.5, 0.0001));
      });

      test('reconcile at just over 0.01 snaps to server', () {
        pred.applyInput(0.5);
        pred.reconcile(serverPaddleX: 0.5101); // diff > 0.01 → snap
        expect(pred.predictedX, closeTo(0.5101, 0.0001));
      });
    });
  }
  ```

  Create `client/test/game/input_controller_test.dart`:

  ```dart
  import 'package:flutter_test/flutter_test.dart';
  import 'package:volley/game/systems/input_controller.dart';

  void main() {
    group('InputController', () {
      late List<double> sentValues;
      late InputController controller;

      setUp(() {
        sentValues = [];
        controller = InputController(
          screenWidth: 400.0,
          halfPaddleWidth: 0.11,
          onSend: (x) => sentValues.add(x),
        );
      });

      test('first input always sends', () {
        controller.onDragUpdate(globalX: 200.0); // center → 0.5
        expect(sentValues.length, 1);
        expect(sentValues[0], closeTo(0.5, 0.001));
      });

      test('input within 0.005 of last sent does not send again', () {
        controller.onDragUpdate(globalX: 200.0); // sends 0.5
        controller.onDragUpdate(globalX: 201.0); // 201/400 = 0.5025 — delta 0.0025 < 0.005
        expect(sentValues.length, 1);
      });

      test('input beyond 0.005 of last sent sends again', () {
        controller.onDragUpdate(globalX: 200.0); // 0.5
        controller.onDragUpdate(globalX: 204.0); // 0.51 — delta 0.01 > 0.005
        expect(sentValues.length, 2);
        expect(sentValues[1], closeTo(0.51, 0.001));
      });

      test('input is clamped to [halfPaddleWidth, 1-halfPaddleWidth]', () {
        controller.onDragUpdate(globalX: -100.0); // far left → clamped to 0.11
        expect(sentValues[0], closeTo(0.11, 0.001));

        controller.onDragUpdate(globalX: 9999.0); // far right → clamped to 0.89
        // Difference from 0.11 is large, so this will send.
        expect(sentValues.last, closeTo(0.89, 0.001));
      });

      test('onDragStart resets lastSentTargetX so next drag always sends', () {
        controller.onDragUpdate(globalX: 200.0); // 0.5 sent
        controller.onDragStart();
        controller.onDragUpdate(globalX: 200.0); // same position but fresh drag → sends
        expect(sentValues.length, 2);
      });
    });
  }
  ```

- [ ] **5.2 Run tests — expect failure**

  ```sh
  cd client && flutter test test/game/prediction_test.dart test/game/input_controller_test.dart -v
  ```

- [ ] **5.3 Create `client/lib/game/systems/prediction.dart`**

  ```dart
  // lib/game/systems/prediction.dart

  const _kSnapThreshold = 0.01;

  /// Tracks the local player's predicted paddle position.
  ///
  /// When the player drags, [applyInput] immediately moves the paddle.
  /// When a snapshot arrives, [reconcile] snaps to the server value only if
  /// the discrepancy exceeds [_kSnapThreshold] normalized units.
  class PaddlePrediction {
    final double halfPaddleWidth;
    double _predictedX = 0.5;

    PaddlePrediction({required this.halfPaddleWidth});

    double get predictedX => _predictedX;

    /// Apply a new [targetX] from user input (already normalized [0,1]).
    void applyInput(double targetX) {
      _predictedX = targetX.clamp(halfPaddleWidth, 1.0 - halfPaddleWidth);
    }

    /// Reconcile with the server's authoritative [serverPaddleX].
    /// Snaps if |predictedX - serverPaddleX| > [_kSnapThreshold].
    void reconcile({required double serverPaddleX}) {
      if ((_predictedX - serverPaddleX).abs() > _kSnapThreshold) {
        _predictedX = serverPaddleX;
      }
    }
  }
  ```

- [ ] **5.4 Create `client/lib/game/systems/input_controller.dart`**

  ```dart
  // lib/game/systems/input_controller.dart

  const _kThrottle = 0.005;

  /// Translates touch-drag events into normalized [targetX] values and
  /// calls [onSend] when the threshold is exceeded.
  class InputController {
    final double screenWidth;
    final double halfPaddleWidth;
    final void Function(double targetX) onSend;

    double _lastSentTargetX = double.nan; // NaN → never sent

    InputController({
      required this.screenWidth,
      required this.halfPaddleWidth,
      required this.onSend,
    });

    /// Call when a new drag gesture starts so the next update always sends.
    void onDragStart() {
      _lastSentTargetX = double.nan;
    }

    /// Call with the raw [globalX] pixel position on every drag update.
    void onDragUpdate({required double globalX}) {
      final normalized = globalX / screenWidth;
      final clamped = normalized.clamp(halfPaddleWidth, 1.0 - halfPaddleWidth);

      final shouldSend = _lastSentTargetX.isNaN ||
          (clamped - _lastSentTargetX).abs() > _kThrottle;

      if (shouldSend) {
        _lastSentTargetX = clamped;
        onSend(clamped);
      }
    }
  }
  ```

- [ ] **5.5 Run tests — expect pass**

  ```sh
  cd client && flutter test test/game/prediction_test.dart test/game/input_controller_test.dart -v
  ```

- [ ] **5.6 Run analyze**

  ```sh
  cd client && flutter analyze
  ```

- [ ] **5.7 Commit**

  ```sh
  git add client/lib/game/systems/prediction.dart client/lib/game/systems/input_controller.dart client/test/game/prediction_test.dart client/test/game/input_controller_test.dart
  git commit -m "feat(client): local paddle prediction and throttled input controller"
  ```

---

## Task 6: Game Components + VolleyGame

**What this task delivers:** Flame `FlameGame` subclass (`VolleyGame`) that hosts `ArenaComponent`, `BallComponent`, and `PaddleComponent`. Components render from normalized coordinates. The game subscribes to the snapshot stream, feeds `InterpolationBuffer`, advances `renderTime`, applies `PaddlePrediction`, and handles input via `InputController`. Arena is flipped for Player 2 so the local paddle is always at the bottom.

### Steps

- [ ] **6.1 Write the widget test**

  Create `client/test/game/volley_game_test.dart`:

  ```dart
  import 'package:flutter/material.dart';
  import 'package:flutter_test/flutter_test.dart';
  import 'package:flame/game.dart';
  import 'package:volley/game/volley_game.dart';
  import 'package:volley/network/protocol.dart';

  void main() {
    testWidgets('VolleyGame loads without error inside GameWidget', (tester) async {
      final matchStarted = MatchStarted(
        serverTime: 1710000033000,
        serverTick: 0,
        matchId: 'match_test',
        playerSlot: 'p1',
        opponentSlot: 'p2',
        settings: {'pointsToWin': 5, 'tickRate': 30, 'snapshotRate': 20},
        initialState: MatchInitialState(
          ball: BallState(x: 0.5, y: 0.5, vx: 0.24, vy: -0.5),
          p1PaddleX: 0.5,
          p1Score: 0,
          p2PaddleX: 0.5,
          p2Score: 0,
        ),
      );

      // Empty stream — no snapshots needed for load test.
      final game = VolleyGame(
        matchStarted: matchStarted,
        snapshotStream: const Stream.empty(),
        onMatchEnded: (_) {},
      );

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: GameWidget(game: game),
          ),
        ),
      );

      await tester.pump(); // one frame
      expect(tester.takeException(), isNull);
    });
  }
  ```

- [ ] **6.2 Run test — expect failure**

  ```sh
  cd client && flutter test test/game/volley_game_test.dart -v
  ```

- [ ] **6.3 Create `client/lib/game/components/arena_component.dart`**

  ```dart
  // lib/game/components/arena_component.dart
  import 'package:flame/components.dart';
  import 'package:flutter/painting.dart';

  class ArenaComponent extends Component with HasGameRef {
    static const _bgColor = Color(0xFF0A0A1A);
    static const _lineColor = Color(0x33FFFFFF);

    final _bgPaint = Paint()..color = _bgColor;
    final _linePaint = Paint()
      ..color = _lineColor
      ..strokeWidth = 1.0;

    @override
    void render(Canvas canvas) {
      final size = gameRef.size;
      // Background
      canvas.drawRect(Rect.fromLTWH(0, 0, size.x, size.y), _bgPaint);
      // Center line
      canvas.drawLine(
        Offset(0, size.y * 0.5),
        Offset(size.x, size.y * 0.5),
        _linePaint,
      );
    }
  }
  ```

- [ ] **6.4 Create `client/lib/game/components/ball_component.dart`**

  ```dart
  // lib/game/components/ball_component.dart
  import 'package:flame/components.dart';
  import 'package:flutter/painting.dart';

  /// Radius in normalized units (matches server constant 0.018).
  const kBallRadius = 0.018;

  class BallComponent extends Component with HasGameRef {
    /// Normalized [0,1] position. Updated each frame by VolleyGame.
    double normalizedX = 0.5;
    double normalizedY = 0.5;

    final _paint = Paint()..color = const Color(0xFFFFFFFF);

    @override
    void render(Canvas canvas) {
      final size = gameRef.size;
      final sx = normalizedX * size.x;
      final sy = normalizedY * size.y;
      final r = kBallRadius * size.x; // use width for radius scaling
      canvas.drawCircle(Offset(sx, sy), r, _paint);
    }
  }
  ```

- [ ] **6.5 Create `client/lib/game/components/paddle_component.dart`**

  ```dart
  // lib/game/components/paddle_component.dart
  import 'package:flame/components.dart';
  import 'package:flutter/painting.dart';

  /// Paddle dimensions in normalized units (matches server constants).
  const kPaddleWidth = 0.22;
  const kPaddleHeight = 0.025;

  class PaddleComponent extends Component with HasGameRef {
    /// Normalized center X. Updated each frame by VolleyGame.
    double normalizedX = 0.5;

    /// Normalized center Y (fixed per slot — 0.93 for p1, 0.07 for p2).
    final double normalizedY;

    final Color color;

    PaddleComponent({required this.normalizedY, required this.color});

    late final Paint _paint = Paint()..color = color;

    @override
    void render(Canvas canvas) {
      final size = gameRef.size;
      final cx = normalizedX * size.x;
      final cy = normalizedY * size.y;
      final w = kPaddleWidth * size.x;
      final h = kPaddleHeight * size.y;
      canvas.drawRRect(
        RRect.fromRectAndRadius(
          Rect.fromCenter(center: Offset(cx, cy), width: w, height: h),
          const Radius.circular(4),
        ),
        _paint,
      );
    }
  }
  ```

- [ ] **6.6 Create `client/lib/game/volley_game.dart`**

  ```dart
  // lib/game/volley_game.dart
  import 'dart:async';
  import 'package:flame/game.dart';
  import 'package:flame/input.dart';
  import 'package:flutter/material.dart';
  import '../network/protocol.dart';
  import 'components/arena_component.dart';
  import 'components/ball_component.dart';
  import 'components/paddle_component.dart';
  import 'systems/interpolation.dart';
  import 'systems/prediction.dart';
  import 'systems/input_controller.dart';

  class VolleyGame extends FlameGame with PanDetector {
    final MatchStarted matchStarted;
    final Stream<ServerMessage> snapshotStream;
    final void Function(MatchEnded) onMatchEnded;

    late final int _localSlot; // 0 = p1, 1 = p2
    late final String _matchId;

    late final BallComponent _ball;
    late final PaddleComponent _localPaddle;
    late final PaddleComponent _remotePaddle;

    final InterpolationBuffer _interpBuffer = InterpolationBuffer();
    late PaddlePrediction _prediction;
    late InputController _inputController;

    // Incremented on each input sent.
    int _inputSeq = 0;

    // Estimated server time in milliseconds (updated each frame).
    int _estimatedServerTimeMs = 0;

    // Timestamp of last snapshot used to seed server time estimation.
    int _lastSnapshotReceivedAt = 0;
    int _lastSnapshotServerTime = 0;

    StreamSubscription<ServerMessage>? _sub;

    // Called by VolleyClient provider when input needs sending.
    void Function(double targetX, int seq)? onSendInput;

    VolleyGame({
      required this.matchStarted,
      required this.snapshotStream,
      required this.onMatchEnded,
    });

    @override
    Future<void> onLoad() async {
      _localSlot = matchStarted.playerSlot == 'p1' ? 0 : 1;
      _matchId = matchStarted.matchId;
      _lastSnapshotServerTime = matchStarted.serverTime;
      _lastSnapshotReceivedAt = DateTime.now().millisecondsSinceEpoch;
      _estimatedServerTimeMs = matchStarted.serverTime;

      _prediction = PaddlePrediction(halfPaddleWidth: kPaddleWidth / 2);

      // Arena flip: p2 sees p2's paddle at bottom (y flipped).
      // p1: localPaddleY=0.93 (bottom), remotePaddleY=0.07 (top)
      // p2: localPaddleY=0.93 after flip (1-0.07), remotePaddleY=0.07 after flip (1-0.93)
      final localY = _localSlot == 0 ? 0.93 : 1.0 - 0.07;
      final remoteY = _localSlot == 0 ? 0.07 : 1.0 - 0.93;

      _ball = BallComponent();
      _localPaddle = PaddleComponent(
        normalizedY: localY,
        color: const Color(0xFF4FC3F7),
      );
      _remotePaddle = PaddleComponent(
        normalizedY: remoteY,
        color: const Color(0xFFEF5350),
      );

      await addAll([ArenaComponent(), _ball, _localPaddle, _remotePaddle]);

      // Seed initial state from match.started
      _ball.normalizedX = _flip(matchStarted.initialState.ball.x, isY: false);
      _ball.normalizedY = _flip(matchStarted.initialState.ball.y, isY: true);
      _localPaddle.normalizedX = _localSlot == 0
          ? matchStarted.initialState.p1PaddleX
          : matchStarted.initialState.p2PaddleX;
      _remotePaddle.normalizedX = _localSlot == 0
          ? matchStarted.initialState.p2PaddleX
          : matchStarted.initialState.p1PaddleX;

      _sub = snapshotStream.listen(_onServerMessage);
    }

    void _onServerMessage(ServerMessage msg) {
      if (msg is MatchSnapshot) {
        // Update server time estimate.
        _lastSnapshotServerTime = msg.serverTime;
        _lastSnapshotReceivedAt = DateTime.now().millisecondsSinceEpoch;
        _interpBuffer.addSnapshot(msg);

        // Reconcile local paddle.
        final serverLocalX = _localSlot == 0 ? msg.p1PaddleX : msg.p2PaddleX;
        _prediction.reconcile(serverPaddleX: serverLocalX);
      } else if (msg is MatchEnded) {
        onMatchEnded(msg);
      }
    }

    @override
    void update(double dt) {
      super.update(dt);

      // Advance estimated server time.
      final now = DateTime.now().millisecondsSinceEpoch;
      _estimatedServerTimeMs =
          _lastSnapshotServerTime + (now - _lastSnapshotReceivedAt);

      final renderTimeMs = _estimatedServerTimeMs - 100;
      final state = _interpBuffer.interpolate(renderTimeMs);

      if (state != null) {
        _ball.normalizedX = _flip(state.ballX, isY: false);
        _ball.normalizedY = _flip(state.ballY, isY: true);

        final remoteX = _localSlot == 0 ? state.p2PaddleX : state.p1PaddleX;
        _remotePaddle.normalizedX = remoteX;
      }

      // Apply local paddle prediction.
      _localPaddle.normalizedX = _prediction.predictedX;
    }

    /// If [_localSlot] is 1 (p2), flip Y so local player is always at bottom.
    double _flip(double normalized, {required bool isY}) {
      if (isY && _localSlot == 1) return 1.0 - normalized;
      return normalized;
    }

    @override
    void onPanStart(DragStartInfo info) {
      _inputController = InputController(
        screenWidth: size.x,
        halfPaddleWidth: kPaddleWidth / 2,
        onSend: (targetX) {
          _prediction.applyInput(targetX);
          onSendInput?.call(targetX, ++_inputSeq);
        },
      );
      _inputController.onDragStart();
    }

    @override
    void onPanUpdate(DragUpdateInfo info) {
      _inputController.onDragUpdate(
        globalX: info.eventPosition.global.x,
      );
    }

    @override
    void onRemove() {
      _sub?.cancel();
      super.onRemove();
    }
  }
  ```

- [ ] **6.7 Run test — expect pass**

  ```sh
  cd client && flutter test test/game/volley_game_test.dart -v
  ```

- [ ] **6.8 Run analyze**

  ```sh
  cd client && flutter analyze
  ```

- [ ] **6.9 Commit**

  ```sh
  git add client/lib/game/ client/test/game/volley_game_test.dart
  git commit -m "feat(client): Flame game components and VolleyGame with interpolation"
  ```

---

## Task 7: Screens + App Wiring

**What this task delivers:** All five screens, `main.dart` with Provider setup, and widget tests for `ConnectScreen`. The app is fully navigable end-to-end. `VolleyClient` is provided at the root; screens listen to `client.stream` via Provider.

### Steps

- [ ] **7.1 Write the failing widget test**

  Create `client/test/screens/connect_screen_test.dart`:

  ```dart
  import 'package:flutter/material.dart';
  import 'package:flutter_test/flutter_test.dart';
  import 'package:provider/provider.dart';
  import 'package:volley/network/websocket_client.dart';
  import 'package:volley/screens/connect_screen.dart';

  // Stub VolleyClient that never actually connects.
  class StubVolleyClient extends VolleyClient {
    bool connectCalled = false;
    @override
    Future<void> connect(String url) async {
      connectCalled = true;
      throw Exception('No real network in tests');
    }
  }

  Widget _wrap(Widget child) {
    return MultiProvider(
      providers: [
        Provider<VolleyClient>(create: (_) => StubVolleyClient()),
      ],
      child: MaterialApp(home: child),
    );
  }

  void main() {
    group('ConnectScreen', () {
      testWidgets('renders display name field and Play button', (tester) async {
        await tester.pumpWidget(_wrap(const ConnectScreen()));
        expect(find.byType(TextFormField), findsOneWidget);
        expect(find.text('Play'), findsOneWidget);
      });

      testWidgets('Play button is enabled when name field is non-empty', (tester) async {
        await tester.pumpWidget(_wrap(const ConnectScreen()));
        // Default text is 'Guest' — button should be enabled.
        final button = tester.widget<ElevatedButton>(find.byType(ElevatedButton));
        expect(button.onPressed, isNotNull);
      });

      testWidgets('Play button is disabled when name field is empty', (tester) async {
        await tester.pumpWidget(_wrap(const ConnectScreen()));
        await tester.enterText(find.byType(TextFormField), '');
        await tester.pump();
        final button = tester.widget<ElevatedButton>(find.byType(ElevatedButton));
        expect(button.onPressed, isNull);
      });

      testWidgets('tapping Play shows loading indicator', (tester) async {
        await tester.pumpWidget(_wrap(const ConnectScreen()));
        await tester.tap(find.text('Play'));
        await tester.pump(); // trigger setState
        expect(find.byType(CircularProgressIndicator), findsOneWidget);
      });

      testWidgets('connection error shows snackbar', (tester) async {
        await tester.pumpWidget(_wrap(const ConnectScreen()));
        await tester.tap(find.text('Play'));
        await tester.pump();          // loading state
        await tester.pumpAndSettle(); // let async error propagate
        expect(find.byType(SnackBar), findsOneWidget);
      });
    });
  }
  ```

- [ ] **7.2 Run test — expect failure**

  ```sh
  cd client && flutter test test/screens/connect_screen_test.dart -v
  ```

- [ ] **7.3 Create `client/lib/screens/connect_screen.dart`**

  ```dart
  // lib/screens/connect_screen.dart
  import 'package:flutter/material.dart';
  import 'package:provider/provider.dart';
  import '../network/config.dart';
  import '../network/protocol.dart';
  import '../network/websocket_client.dart';
  import '../storage/session_store.dart';
  import 'lobby_screen.dart';

  class ConnectScreen extends StatefulWidget {
    const ConnectScreen({super.key});

    @override
    State<ConnectScreen> createState() => _ConnectScreenState();
  }

  class _ConnectScreenState extends State<ConnectScreen> {
    final _nameController = TextEditingController(text: 'Guest');
    final _sessionStore = SessionStore();
    bool _loading = false;
    String? _error;

    @override
    void dispose() {
      _nameController.dispose();
      super.dispose();
    }

    Future<void> _onPlay() async {
      final name = _nameController.text.trim();
      if (name.isEmpty) return;

      setState(() {
        _loading = true;
        _error = null;
      });

      final client = context.read<VolleyClient>();
      final token = await _sessionStore.loadToken();

      try {
        await client.connect(kServerUrl);

        // Send hello and wait for server.hello.
        client.send(ClientHello(
          requestId: 'req_hello',
          sentAt: DateTime.now().millisecondsSinceEpoch,
          displayName: name,
          sessionToken: token,
        ));

        // Wait for ServerHello (or error).
        final response = await client.stream
            .firstWhere((m) => m is ServerHello || m is ServerError)
            .timeout(const Duration(seconds: 10));

        if (response is ServerHello) {
          await _sessionStore.saveToken(response.sessionToken);
          if (mounted) {
            Navigator.of(context).pushReplacement(
              MaterialPageRoute(builder: (_) => const LobbyScreen()),
            );
          }
        } else if (response is ServerError) {
          throw Exception(response.message);
        }
      } catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('Connection failed: $e')),
          );
        }
      } finally {
        if (mounted) setState(() => _loading = false);
      }
    }

    @override
    Widget build(BuildContext context) {
      return Scaffold(
        body: SafeArea(
          child: Padding(
            padding: const EdgeInsets.all(32.0),
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                const Text(
                  'Volley',
                  style: TextStyle(fontSize: 48, fontWeight: FontWeight.bold),
                ),
                const SizedBox(height: 48),
                TextFormField(
                  controller: _nameController,
                  decoration: const InputDecoration(
                    labelText: 'Display Name',
                    border: OutlineInputBorder(),
                  ),
                  onChanged: (_) => setState(() {}),
                  enabled: !_loading,
                ),
                const SizedBox(height: 24),
                SizedBox(
                  width: double.infinity,
                  child: ElevatedButton(
                    onPressed: (_loading || _nameController.text.trim().isEmpty)
                        ? null
                        : _onPlay,
                    child: _loading
                        ? const SizedBox(
                            width: 20,
                            height: 20,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Text('Play'),
                  ),
                ),
                if (_error != null) ...[
                  const SizedBox(height: 12),
                  Text(_error!, style: const TextStyle(color: Colors.red)),
                ],
              ],
            ),
          ),
        ),
      );
    }
  }
  ```

- [ ] **7.4 Create `client/lib/screens/lobby_screen.dart`**

  ```dart
  // lib/screens/lobby_screen.dart
  import 'dart:async';
  import 'package:flutter/material.dart';
  import 'package:provider/provider.dart';
  import '../network/protocol.dart';
  import '../network/websocket_client.dart';
  import 'waiting_room_screen.dart';

  class LobbyScreen extends StatefulWidget {
    const LobbyScreen({super.key});

    @override
    State<LobbyScreen> createState() => _LobbyScreenState();
  }

  class _LobbyScreenState extends State<LobbyScreen> {
    final _codeController = TextEditingController();
    StreamSubscription<ServerMessage>? _sub;
    bool _loading = false;

    @override
    void initState() {
      super.initState();
      final client = context.read<VolleyClient>();
      _sub = client.stream.listen(_onMessage);
    }

    @override
    void dispose() {
      _sub?.cancel();
      _codeController.dispose();
      super.dispose();
    }

    void _onMessage(ServerMessage msg) {
      if (msg is RoomCreated) {
        if (mounted) {
          Navigator.of(context).push(
            MaterialPageRoute(
              builder: (_) => WaitingRoomScreen(roomCode: msg.roomCode),
            ),
          );
        }
        setState(() => _loading = false);
      } else if (msg is RoomUpdated) {
        // Join acknowledged — navigate.
        if (mounted) {
          Navigator.of(context).push(
            MaterialPageRoute(
              builder: (_) => WaitingRoomScreen(roomCode: msg.roomCode),
            ),
          );
        }
        setState(() => _loading = false);
      } else if (msg is ServerError) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text(msg.message)),
          );
        }
        setState(() => _loading = false);
      }
    }

    void _createRoom() {
      setState(() => _loading = true);
      final client = context.read<VolleyClient>();
      client.send(RoomCreate(
        requestId: 'req_create',
        sentAt: DateTime.now().millisecondsSinceEpoch,
      ));
    }

    void _joinRoom() {
      final code = _codeController.text.trim();
      if (code.length != 6) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Enter a 6-digit room code')),
        );
        return;
      }
      setState(() => _loading = true);
      final client = context.read<VolleyClient>();
      client.send(RoomJoin(
        requestId: 'req_join',
        sentAt: DateTime.now().millisecondsSinceEpoch,
        roomCode: code,
      ));
    }

    @override
    Widget build(BuildContext context) {
      return Scaffold(
        appBar: AppBar(title: const Text('Lobby')),
        body: Padding(
          padding: const EdgeInsets.all(24.0),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              SizedBox(
                width: double.infinity,
                child: ElevatedButton(
                  onPressed: _loading ? null : _createRoom,
                  child: const Text('Create Room'),
                ),
              ),
              const SizedBox(height: 32),
              const Divider(),
              const SizedBox(height: 32),
              TextFormField(
                controller: _codeController,
                decoration: const InputDecoration(
                  labelText: 'Room Code',
                  border: OutlineInputBorder(),
                  hintText: '6 digits',
                ),
                keyboardType: TextInputType.number,
                maxLength: 6,
              ),
              const SizedBox(height: 12),
              SizedBox(
                width: double.infinity,
                child: ElevatedButton(
                  onPressed: _loading ? null : _joinRoom,
                  child: const Text('Join Room'),
                ),
              ),
              if (_loading) const Padding(
                padding: EdgeInsets.only(top: 24),
                child: CircularProgressIndicator(),
              ),
            ],
          ),
        ),
      );
    }
  }
  ```

- [ ] **7.5 Create `client/lib/screens/waiting_room_screen.dart`**

  ```dart
  // lib/screens/waiting_room_screen.dart
  import 'dart:async';
  import 'package:flutter/material.dart';
  import 'package:provider/provider.dart';
  import '../network/protocol.dart';
  import '../network/websocket_client.dart';
  import 'match_screen.dart';

  class WaitingRoomScreen extends StatefulWidget {
    final String roomCode;
    const WaitingRoomScreen({super.key, required this.roomCode});

    @override
    State<WaitingRoomScreen> createState() => _WaitingRoomScreenState();
  }

  class _WaitingRoomScreenState extends State<WaitingRoomScreen> {
    StreamSubscription<ServerMessage>? _sub;
    List<RoomPlayer> _players = [];
    bool _isReady = false;
    int? _countdownSeconds;

    @override
    void initState() {
      super.initState();
      _sub = context.read<VolleyClient>().stream.listen(_onMessage);
    }

    @override
    void dispose() {
      _sub?.cancel();
      super.dispose();
    }

    void _onMessage(ServerMessage msg) {
      if (msg is RoomUpdated) {
        setState(() => _players = msg.players);
      } else if (msg is MatchCountdown) {
        final remaining = (msg.durationMs / 1000).ceil();
        setState(() => _countdownSeconds = remaining);
        _tickCountdown(remaining);
      } else if (msg is MatchStarted) {
        Navigator.of(context).pushReplacement(
          MaterialPageRoute(
            builder: (_) => MatchScreen(matchStarted: msg),
          ),
        );
      }
    }

    void _tickCountdown(int remaining) {
      if (remaining <= 0 || !mounted) return;
      Future.delayed(const Duration(seconds: 1), () {
        if (mounted) {
          setState(() => _countdownSeconds = remaining - 1);
          _tickCountdown(remaining - 1);
        }
      });
    }

    void _toggleReady() {
      final newReady = !_isReady;
      setState(() => _isReady = newReady);
      context.read<VolleyClient>().send(RoomReady(
        requestId: 'req_ready',
        sentAt: DateTime.now().millisecondsSinceEpoch,
        ready: newReady,
      ));
    }

    @override
    Widget build(BuildContext context) {
      return Scaffold(
        appBar: AppBar(title: const Text('Waiting Room')),
        body: Padding(
          padding: const EdgeInsets.all(24.0),
          child: Column(
            children: [
              const Text('Room Code', style: TextStyle(fontSize: 14)),
              const SizedBox(height: 4),
              Text(
                widget.roomCode,
                style: const TextStyle(
                  fontSize: 48,
                  fontWeight: FontWeight.bold,
                  letterSpacing: 8,
                ),
              ),
              const SizedBox(height: 32),
              ..._players.map(
                (p) => ListTile(
                  title: Text(p.displayName),
                  subtitle: Text(p.role),
                  trailing: Icon(
                    p.ready ? Icons.check_circle : Icons.radio_button_unchecked,
                    color: p.ready ? Colors.green : Colors.grey,
                  ),
                ),
              ),
              const Spacer(),
              if (_countdownSeconds != null)
                Text(
                  'Starting in $_countdownSeconds...',
                  style: const TextStyle(fontSize: 32, fontWeight: FontWeight.bold),
                )
              else
                SizedBox(
                  width: double.infinity,
                  child: ElevatedButton(
                    onPressed: _toggleReady,
                    style: ElevatedButton.styleFrom(
                      backgroundColor: _isReady ? Colors.green : null,
                    ),
                    child: Text(_isReady ? 'Ready!' : 'Ready'),
                  ),
                ),
            ],
          ),
        ),
      );
    }
  }
  ```

- [ ] **7.6 Create `client/lib/screens/match_screen.dart`**

  ```dart
  // lib/screens/match_screen.dart
  import 'dart:async';
  import 'package:flame/game.dart';
  import 'package:flutter/material.dart';
  import 'package:provider/provider.dart';
  import '../game/volley_game.dart';
  import '../network/protocol.dart';
  import '../network/websocket_client.dart';
  import 'result_screen.dart';

  class MatchScreen extends StatefulWidget {
    final MatchStarted matchStarted;
    const MatchScreen({super.key, required this.matchStarted});

    @override
    State<MatchScreen> createState() => _MatchScreenState();
  }

  class _MatchScreenState extends State<MatchScreen> {
    late final VolleyGame _game;
    late final StreamController<ServerMessage> _gameStreamController;
    StreamSubscription<ServerMessage>? _sub;
    bool _disconnected = false;
    int? _reconnectSecondsLeft;

    @override
    void initState() {
      super.initState();
      _gameStreamController = StreamController<ServerMessage>.broadcast();
      final client = context.read<VolleyClient>();

      _game = VolleyGame(
        matchStarted: widget.matchStarted,
        snapshotStream: _gameStreamController.stream,
        onMatchEnded: _onMatchEnded,
      );

      // Wire input sending through the client.
      _game.onSendInput = (targetX, seq) {
        client.send(InputPaddleTarget(
          requestId: 'req_input_$seq',
          sentAt: DateTime.now().millisecondsSinceEpoch,
          matchId: widget.matchStarted.matchId,
          clientSeq: seq,
          targetX: targetX,
        ));
      };

      _sub = client.stream.listen(_onMessage);
    }

    @override
    void dispose() {
      _sub?.cancel();
      _gameStreamController.close();
      super.dispose();
    }

    void _onMessage(ServerMessage msg) {
      // Forward all server messages into the game's stream.
      _gameStreamController.add(msg);

      if (msg is PlayerDisconnected) {
        final deadline = msg.reconnectDeadline;
        final secondsLeft =
            ((deadline - DateTime.now().millisecondsSinceEpoch) / 1000).ceil();
        setState(() {
          _disconnected = true;
          _reconnectSecondsLeft = secondsLeft;
        });
        _tickReconnect(secondsLeft);
      } else if (msg is PlayerReconnected) {
        setState(() {
          _disconnected = false;
          _reconnectSecondsLeft = null;
        });
      }
    }

    void _tickReconnect(int remaining) {
      if (remaining <= 0 || !mounted) return;
      Future.delayed(const Duration(seconds: 1), () {
        if (mounted && _disconnected) {
          setState(() => _reconnectSecondsLeft = remaining - 1);
          _tickReconnect(remaining - 1);
        }
      });
    }

    void _onMatchEnded(MatchEnded msg) {
      if (mounted) {
        Navigator.of(context).pushReplacement(
          MaterialPageRoute(
            builder: (_) => ResultScreen(matchEnded: msg),
          ),
        );
      }
    }

    @override
    Widget build(BuildContext context) {
      return Scaffold(
        body: Stack(
          children: [
            GameWidget(game: _game),
            if (_disconnected)
              Container(
                color: Colors.black54,
                child: Center(
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      const Text(
                        'Opponent disconnected',
                        style: TextStyle(color: Colors.white, fontSize: 20),
                      ),
                      const SizedBox(height: 8),
                      Text(
                        'Reconnect window: ${_reconnectSecondsLeft ?? 0}s',
                        style: const TextStyle(color: Colors.white70),
                      ),
                    ],
                  ),
                ),
              ),
          ],
        ),
      );
    }
  }
  ```

- [ ] **7.7 Create `client/lib/screens/result_screen.dart`**

  ```dart
  // lib/screens/result_screen.dart
  import 'dart:async';
  import 'package:flutter/material.dart';
  import 'package:provider/provider.dart';
  import '../network/protocol.dart';
  import '../network/websocket_client.dart';
  import 'lobby_screen.dart';
  import 'waiting_room_screen.dart';

  class ResultScreen extends StatefulWidget {
    final MatchEnded matchEnded;
    const ResultScreen({super.key, required this.matchEnded});

    @override
    State<ResultScreen> createState() => _ResultScreenState();
  }

  class _ResultScreenState extends State<ResultScreen> {
    StreamSubscription<ServerMessage>? _sub;

    @override
    void initState() {
      super.initState();
      _sub = context.read<VolleyClient>().stream.listen(_onMessage);
    }

    @override
    void dispose() {
      _sub?.cancel();
      super.dispose();
    }

    void _onMessage(ServerMessage msg) {
      if (msg is RematchStarted) {
        // Server will send match.countdown + match.started — go back to waiting.
        Navigator.of(context).pushAndRemoveUntil(
          MaterialPageRoute(
            builder: (_) => const WaitingRoomScreen(roomCode: ''),
          ),
          (route) => false,
        );
      }
    }

    void _requestRematch() {
      context.read<VolleyClient>().send(RematchRequest(
        requestId: 'req_rematch',
        sentAt: DateTime.now().millisecondsSinceEpoch,
        matchId: widget.matchEnded.matchId,
      ));
    }

    void _quit() {
      context.read<VolleyClient>().send(
        RoomLeave(
          requestId: 'req_leave',
          sentAt: DateTime.now().millisecondsSinceEpoch,
        ),
      );
      Navigator.of(context).pushAndRemoveUntil(
        MaterialPageRoute(builder: (_) => const LobbyScreen()),
        (route) => false,
      );
    }

    @override
    Widget build(BuildContext context) {
      final m = widget.matchEnded;
      final winnerLabel = m.winnerSlot == 'p1' ? 'Player 1' : 'Player 2';

      return Scaffold(
        body: SafeArea(
          child: Padding(
            padding: const EdgeInsets.all(32.0),
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Text(
                  '$winnerLabel wins!',
                  style: const TextStyle(
                    fontSize: 36,
                    fontWeight: FontWeight.bold,
                  ),
                ),
                const SizedBox(height: 16),
                Text(
                  '${m.p1FinalScore} – ${m.p2FinalScore}',
                  style: const TextStyle(fontSize: 48),
                ),
                const SizedBox(height: 8),
                Text(
                  'Reason: ${m.reason.replaceAll('_', ' ')}',
                  style: const TextStyle(color: Colors.grey),
                ),
                const SizedBox(height: 48),
                SizedBox(
                  width: double.infinity,
                  child: ElevatedButton(
                    onPressed: _requestRematch,
                    child: const Text('Rematch'),
                  ),
                ),
                const SizedBox(height: 12),
                SizedBox(
                  width: double.infinity,
                  child: OutlinedButton(
                    onPressed: _quit,
                    child: const Text('Quit to Lobby'),
                  ),
                ),
              ],
            ),
          ),
        ),
      );
    }
  }
  ```

- [ ] **7.8 Create `client/lib/main.dart`**

  ```dart
  // lib/main.dart
  import 'package:flutter/material.dart';
  import 'package:flutter/services.dart';
  import 'package:provider/provider.dart';
  import 'network/websocket_client.dart';
  import 'screens/connect_screen.dart';

  void main() {
    WidgetsFlutterBinding.ensureInitialized();
    // Portrait-only, phone-first.
    SystemChrome.setPreferredOrientations([
      DeviceOrientation.portraitUp,
      DeviceOrientation.portraitDown,
    ]);
    runApp(const VolleyApp());
  }

  class VolleyApp extends StatelessWidget {
    const VolleyApp({super.key});

    @override
    Widget build(BuildContext context) {
      return Provider<VolleyClient>(
        create: (_) => VolleyClient(),
        dispose: (_, client) => client.dispose(),
        child: MaterialApp(
          title: 'Volley',
          debugShowCheckedModeBanner: false,
          theme: ThemeData.dark(useMaterial3: true).copyWith(
            colorScheme: ColorScheme.fromSeed(
              seedColor: const Color(0xFF4FC3F7),
              brightness: Brightness.dark,
            ),
          ),
          home: const ConnectScreen(),
        ),
      );
    }
  }
  ```

- [ ] **7.9 Create test directory for screens**

  ```sh
  mkdir client/test/screens
  ```

- [ ] **7.10 Run the ConnectScreen test — expect pass**

  ```sh
  cd client && flutter test test/screens/connect_screen_test.dart -v
  ```

- [ ] **7.11 Run full test suite**

  ```sh
  cd client && flutter test
  ```

- [ ] **7.12 Run analyze**

  ```sh
  cd client && flutter analyze
  ```

- [ ] **7.13 Commit**

  ```sh
  git add client/lib/ client/test/screens/
  git commit -m "feat(client): all screens wired with Provider, portrait-only layout"
  ```

---

## Self-Review Checklist

### Class/method name consistency

| Symbol | Defined in | Referenced in |
|---|---|---|
| `VolleyClient` | `lib/network/websocket_client.dart` | `main.dart`, all screens, `match_screen.dart` |
| `VolleyClient.fromChannel` | `websocket_client.dart` | `websocket_client_test.dart` |
| `SessionStore` | `lib/storage/session_store.dart` | `connect_screen.dart` |
| `ServerMessage`, `ClientMessage` | `lib/network/protocol.dart` | `websocket_client.dart`, all screens |
| `MatchSnapshot` | `protocol.dart` | `interpolation.dart`, `interpolation_test.dart`, `volley_game.dart` |
| `BallState` | `protocol.dart` | `MatchSnapshot`, `MatchStarted`, `MatchRallyReset`, `MatchReconnected`, `interpolation_test.dart` |
| `InterpolationBuffer`, `InterpolatedState` | `lib/game/systems/interpolation.dart` | `volley_game.dart`, `interpolation_test.dart` |
| `PaddlePrediction` | `lib/game/systems/prediction.dart` | `volley_game.dart`, `prediction_test.dart` |
| `InputController` | `lib/game/systems/input_controller.dart` | `volley_game.dart`, `input_controller_test.dart` |
| `ArenaComponent`, `BallComponent`, `PaddleComponent` | `lib/game/components/` | `volley_game.dart` |
| `kPaddleWidth`, `kPaddleHeight` | `paddle_component.dart` | `volley_game.dart`, `input_controller.dart` (via game) |
| `kBallRadius` | `ball_component.dart` | (visual only — not cross-referenced) |
| `VolleyGame` | `lib/game/volley_game.dart` | `match_screen.dart`, `volley_game_test.dart` |
| `ConnectScreen` | `lib/screens/connect_screen.dart` | `main.dart`, `result_screen.dart` |
| `LobbyScreen` | `lib/screens/lobby_screen.dart` | `connect_screen.dart`, `result_screen.dart` |
| `WaitingRoomScreen` | `lib/screens/waiting_room_screen.dart` | `lobby_screen.dart`, `result_screen.dart` |
| `MatchScreen` | `lib/screens/match_screen.dart` | `waiting_room_screen.dart` |
| `ResultScreen` | `lib/screens/result_screen.dart` | `match_screen.dart` |

### Forward-reference check (no task may reference a class defined in a later task)

- Task 1 (config): references nothing from Tasks 2–7. ✓
- Task 2 (protocol): references nothing from Tasks 3–7. ✓
- Task 3 (VolleyClient, SessionStore): references `protocol.dart` (Task 2). ✓
- Task 4 (InterpolationBuffer): references `protocol.dart` (Task 2). ✓
- Task 5 (PaddlePrediction, InputController): references nothing from Tasks 6–7. ✓
- Task 6 (components, VolleyGame): references `protocol.dart` (2), `interpolation.dart` (4), `prediction.dart` (5), `input_controller.dart` (5). ✓
- Task 7 (screens, main): references `websocket_client.dart` (3), `session_store.dart` (3), `protocol.dart` (2), `volley_game.dart` (6). ✓

### Import path check

All `import` statements in implementation files use paths relative to `lib/`, matching the file structure defined in the tasks. Test files import via `package:volley/` which resolves to `client/lib/`. ✓
