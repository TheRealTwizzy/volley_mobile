package wsconn

import (
	"crypto/rand"
	"fmt"
	"log"
	"time"

	gorillaws "github.com/gorilla/websocket"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/protocol"
)

const (
	writeWait    = 10 * time.Second
	pingInterval = 10 * time.Second
	pongWait     = 15 * time.Second
	maxMsgSize   = 4096
)

type conn struct {
	ws      *gorillaws.Conn
	session *auth.Session
	send    chan []byte
	id      string
}

func newConn(ws *gorillaws.Conn) *conn {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return &conn{
		ws:   ws,
		send: make(chan []byte, 64),
		id:   fmt.Sprintf("%x", b),
	}
}

// ID returns a stable unique identifier for this connection. Satisfies lobby.Sender.
func (c *conn) ID() string { return c.id }

// SendBytes is the exported send method. Satisfies lobby.Sender.
func (c *conn) SendBytes(data []byte) { c.sendBytes(data) }

func (c *conn) sendBytes(data []byte) {
	select {
	case c.send <- data:
	default:
		log.Println("wsconn: send buffer full, dropping message")
	}
}

func (c *conn) writeLoop() {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		c.ws.Close()
	}()

	pingSeq := 0
	for {
		select {
		case msg, ok := <-c.send:
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.ws.WriteMessage(gorillaws.CloseMessage, []byte{})
				return
			}
			if err := c.ws.WriteMessage(gorillaws.TextMessage, msg); err != nil {
				log.Printf("wsconn: write error: %v", err)
				return
			}

		case <-ticker.C:
			pingSeq++
			data, err := protocol.MarshalServer(protocol.ServerEnvelope{
				Type:    protocol.TypeServerPing,
				Payload: map[string]string{"pingId": fmt.Sprintf("ping_%d", pingSeq)},
			})
			if err != nil {
				continue
			}
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(gorillaws.TextMessage, data); err != nil {
				log.Printf("wsconn: ping write error: %v", err)
				return
			}
		}
	}
}

// readLoop reads messages and dispatches them. onClose (if non-nil) is called
// before the send channel is closed – use it to notify managers of disconnection.
func (c *conn) readLoop(dispatch func(*conn, protocol.ClientEnvelope), onClose func()) {
	defer func() {
		if onClose != nil {
			onClose()
		}
		close(c.send)
		c.ws.Close()
	}()

	c.ws.SetReadLimit(maxMsgSize)
	c.ws.SetReadDeadline(time.Now().Add(pongWait))

	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			if gorillaws.IsUnexpectedCloseError(err,
				gorillaws.CloseGoingAway,
				gorillaws.CloseNormalClosure,
				gorillaws.CloseAbnormalClosure,
			) {
				log.Printf("wsconn: unexpected close: %v", err)
			}
			return
		}
		c.ws.SetReadDeadline(time.Now().Add(pongWait))

		env, err := protocol.ParseClient(data)
		if err != nil {
			errMsg, _ := protocol.MakeError("bad_message", "Message could not be parsed.", "", false, 0)
			c.sendBytes(errMsg)
			continue
		}
		dispatch(c, env)
	}
}
