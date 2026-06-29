package wsconn

import (
	"encoding/json"
	"log"
	"net/http"

	gorillaws "github.com/gorilla/websocket"

	"github.com/pong-mobile/backend/internal/auth"
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
func Handler(sessions *auth.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("wsconn: upgrade error: %v", err)
			return
		}
		c := newConn(ws)
		go c.writeLoop()
		c.readLoop(makeDispatcher(sessions))
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

func makeDispatcher(sessions *auth.Store) func(*conn, protocol.ClientEnvelope) {
	return func(c *conn, env protocol.ClientEnvelope) {
		switch env.Type {
		case protocol.TypeClientHello:
			handleHello(c, env, sessions)

		case protocol.TypeClientPong:
			// JSON-level pong is a no-op; read deadline already extended in readLoop.

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
