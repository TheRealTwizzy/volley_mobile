package wsconn

import (
	"encoding/json"
	"log"
	"net/http"

	gorillaws "github.com/gorilla/websocket"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/config"
	"github.com/pong-mobile/backend/internal/lobby"
	"github.com/pong-mobile/backend/internal/protocol"
)

var upgrader = gorillaws.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow all origins during development.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handler returns an http.Handler that upgrades HTTP connections to WebSocket
// and runs the hello handshake and message dispatch loop.
func Handler(sessions *auth.Store, mgr *lobby.Manager) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("wsconn: upgrade error: %v", err)
			return
		}
		c := newConn(ws)
		go c.writeLoop()
		c.readLoop(makeDispatcher(sessions, mgr), func() {
			mgr.OnDisconnect(c.id)
		})
	})
}

// helloPayload matches client.hello payload from NETWORK_PROTOCOL.md §7.
type helloPayload struct {
	ClientVersion string  `json:"clientVersion"`
	Platform      string  `json:"platform"`
	SessionToken  *string `json:"sessionToken"`
	DisplayName   string  `json:"displayName"`
}

// serverHelloPayload matches server.hello payload from NETWORK_PROTOCOL.md §7.
type serverHelloPayload struct {
	SessionID           string `json:"sessionId"`
	PlayerID            string `json:"playerId"`
	SessionToken        string `json:"sessionToken"`
	HeartbeatIntervalMs int    `json:"heartbeatIntervalMs"`
}

// createPayload is the payload for room.create.
type createPayload struct {
	Settings struct {
		PointsToWin int     `json:"pointsToWin"`
		PaddleSpeed float64 `json:"paddleSpeed"`
		BallSpeed   float64 `json:"ballSpeed"`
	} `json:"settings"`
}

// joinPayload is the payload for room.join.
type joinPayload struct {
	RoomCode string `json:"roomCode"`
}

// readyPayload is the payload for room.ready.
type readyPayload struct {
	Ready bool `json:"ready"`
}

func makeDispatcher(sessions *auth.Store, mgr *lobby.Manager) func(*conn, protocol.ClientEnvelope) {
	return func(c *conn, env protocol.ClientEnvelope) {
		// All messages except client.hello and client.pong require a session.
		if c.session == nil && env.Type != protocol.TypeClientHello && env.Type != protocol.TypeClientPong {
			errMsg, _ := protocol.MakeError("unauthorized", "Session required. Send client.hello first.", env.RequestID, false, 0)
			c.sendBytes(errMsg)
			return
		}

		switch env.Type {
		case protocol.TypeClientHello:
			handleHello(c, env, sessions)

		case protocol.TypeClientPong:
			// JSON-level pong is a no-op; read deadline already extended in readLoop.

		case protocol.TypeRoomCreate:
			handleRoomCreate(c, env, mgr)

		case protocol.TypeRoomJoin:
			handleRoomJoin(c, env, mgr)

		case protocol.TypeRoomReady:
			handleRoomReady(c, env, mgr)

		case protocol.TypeRoomLeave:
			mgr.LeaveRoom(c.id)

		default:
			errMsg, _ := protocol.MakeError("unknown_type", "Message type is not supported.", env.RequestID, false, 0)
			c.sendBytes(errMsg)
			log.Printf("wsconn: unknown message type %q", env.Type)
		}
	}
}

func handleHello(c *conn, env protocol.ClientEnvelope, sessions *auth.Store) {
	var p helloPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		errMsg, _ := protocol.MakeError("bad_message", "Invalid hello payload.", env.RequestID, false, 0)
		c.sendBytes(errMsg)
		return
	}

	displayName := p.DisplayName
	if displayName == "" {
		displayName = "Guest"
	}

	sess, err := auth.NewSession(displayName)
	if err != nil {
		errMsg, _ := protocol.MakeError("server_error", "Could not create session.", env.RequestID, false, 0)
		c.sendBytes(errMsg)
		return
	}
	sessions.Put(sess)
	c.session = &sess

	data, err := protocol.MarshalServer(protocol.ServerEnvelope{
		Type: protocol.TypeServerHello,
		Payload: serverHelloPayload{
			SessionID:           sess.ID,
			PlayerID:            sess.PlayerID,
			SessionToken:        sess.Token,
			HeartbeatIntervalMs: int(pingInterval.Milliseconds()),
		},
	})
	if err != nil {
		log.Printf("wsconn: marshal server.hello: %v", err)
		return
	}
	c.sendBytes(data)
	log.Printf("wsconn: session created playerID=%s displayName=%q", sess.PlayerID, sess.DisplayName)
}

func handleRoomCreate(c *conn, env protocol.ClientEnvelope, mgr *lobby.Manager) {
	var p createPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		errMsg, _ := protocol.MakeError("bad_message", "Invalid room.create payload.", env.RequestID, false, 0)
		c.sendBytes(errMsg)
		return
	}

	settings := config.Settings{
		PointsToWin: p.Settings.PointsToWin,
		PaddleSpeed: p.Settings.PaddleSpeed,
		BallSpeed:   p.Settings.BallSpeed,
	}

	_, err := mgr.CreateRoom(c.session, c, settings)
	if err != nil {
		code := "server_error"
		if err == lobby.ErrAlreadyInRoom {
			code = "already_in_room"
		}
		errMsg, _ := protocol.MakeError(code, err.Error(), env.RequestID, false, 0)
		c.sendBytes(errMsg)
	}
}

func handleRoomJoin(c *conn, env protocol.ClientEnvelope, mgr *lobby.Manager) {
	var p joinPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		errMsg, _ := protocol.MakeError("bad_message", "Invalid room.join payload.", env.RequestID, false, 0)
		c.sendBytes(errMsg)
		return
	}

	_, err := mgr.JoinRoom(p.RoomCode, c.session, c)
	if err != nil {
		var code string
		switch err {
		case lobby.ErrRoomNotFound:
			code = "room_not_found"
		case lobby.ErrRoomFull:
			code = "room_full"
		case lobby.ErrAlreadyInRoom:
			code = "already_in_room"
		default:
			code = "server_error"
		}
		errMsg, _ := protocol.MakeError(code, err.Error(), env.RequestID, false, 0)
		c.sendBytes(errMsg)
	}
}

func handleRoomReady(c *conn, env protocol.ClientEnvelope, mgr *lobby.Manager) {
	var p readyPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		errMsg, _ := protocol.MakeError("bad_message", "Invalid room.ready payload.", env.RequestID, false, 0)
		c.sendBytes(errMsg)
		return
	}

	if err := mgr.SetReady(c.id, p.Ready); err != nil {
		errMsg, _ := protocol.MakeError("invalid_state", "Not in a room.", env.RequestID, false, 0)
		c.sendBytes(errMsg)
	}
}
