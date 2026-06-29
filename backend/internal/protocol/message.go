package protocol

import (
	"encoding/json"
	"time"
)

const (
	TypeClientHello = "client.hello"
	TypeServerHello = "server.hello"
	TypeServerPing  = "server.ping"
	TypeClientPong  = "client.pong"
	TypeError       = "error"
)

// ClientEnvelope is the common wrapper for all client→server messages.
type ClientEnvelope struct {
	Type      string          `json:"type"`
	RequestID string          `json:"requestId"`
	SentAt    int64           `json:"sentAt"`
	Payload   json.RawMessage `json:"payload"`
}

// ServerEnvelope is the common wrapper for all server→client messages.
type ServerEnvelope struct {
	Type       string `json:"type"`
	ServerTime int64  `json:"serverTime"`
	ServerTick int64  `json:"serverTick,omitempty"`
	Payload    any    `json:"payload"`
}

// ErrorPayload is the payload body for "error" messages.
type ErrorPayload struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	RequestID   string `json:"requestId,omitempty"`
	Recoverable bool   `json:"recoverable"`
}

// ParseClient parses a raw WebSocket text frame into a ClientEnvelope.
func ParseClient(data []byte) (ClientEnvelope, error) {
	var env ClientEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return ClientEnvelope{}, err
	}
	return env, nil
}

// MarshalServer serializes a ServerEnvelope to JSON.
// If ServerTime is zero it is set to the current UTC time in milliseconds.
func MarshalServer(env ServerEnvelope) ([]byte, error) {
	if env.ServerTime == 0 {
		env.ServerTime = time.Now().UnixMilli()
	}
	return json.Marshal(env)
}

// MakeError builds a serialized "error" message ready to send to a client.
func MakeError(code, message, requestID string, recoverable bool, tick int64) ([]byte, error) {
	return MarshalServer(ServerEnvelope{
		Type:       TypeError,
		ServerTick: tick,
		Payload: ErrorPayload{
			Code:        code,
			Message:     message,
			RequestID:   requestID,
			Recoverable: recoverable,
		},
	})
}
