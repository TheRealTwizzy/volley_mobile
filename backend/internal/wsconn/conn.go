package wsconn

import (
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

// conn manages a single WebSocket client connection.
type conn struct {
	ws      *gorillaws.Conn
	session *auth.Session
	send    chan []byte // buffered outbound message queue
}

func newConn(ws *gorillaws.Conn) *conn {
	return &conn{
		ws:   ws,
		send: make(chan []byte, 64),
	}
}

// sendBytes queues a raw message for the write loop. Drops silently if full.
func (c *conn) sendBytes(data []byte) {
	select {
	case c.send <- data:
	default:
		log.Println("wsconn: send buffer full, dropping message")
	}
}

// writeLoop drains the send channel and sends JSON ping frames on a ticker.
// Closes the WebSocket when the send channel is closed or a write fails.
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

// readLoop reads messages and dispatches them until the connection closes.
// Closing the send channel signals the writeLoop to shut down.
func (c *conn) readLoop(dispatch func(*conn, protocol.ClientEnvelope)) {
	defer func() {
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
		// Extend deadline on any received frame (heartbeat or input).
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
