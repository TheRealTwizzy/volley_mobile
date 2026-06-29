package protocol_test

import (
	"encoding/json"
	"testing"

	"github.com/pong-mobile/backend/internal/protocol"
)

func TestParseClient_ValidHello(t *testing.T) {
	raw := `{"type":"client.hello","requestId":"req_001","sentAt":1710000000000,"payload":{"clientVersion":"0.1.0","platform":"android","sessionToken":null,"displayName":"Guest1234"}}`
	env, err := protocol.ParseClient([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Type != protocol.TypeClientHello {
		t.Errorf("type: got %q, want %q", env.Type, protocol.TypeClientHello)
	}
	if env.RequestID != "req_001" {
		t.Errorf("requestId: got %q", env.RequestID)
	}
	if env.SentAt != 1710000000000 {
		t.Errorf("sentAt: got %d", env.SentAt)
	}
	if len(env.Payload) == 0 {
		t.Error("payload should not be empty")
	}
}

func TestParseClient_InvalidJSON(t *testing.T) {
	_, err := protocol.ParseClient([]byte("not json"))
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}

func TestParseClient_MissingType(t *testing.T) {
	raw := `{"requestId":"req_001","sentAt":1710000000000,"payload":{}}`
	env, err := protocol.ParseClient([]byte(raw))
	if err != nil {
		t.Fatalf("parse should succeed even with missing type: %v", err)
	}
	if env.Type != "" {
		t.Errorf("type should be empty string, got %q", env.Type)
	}
}

func TestMarshalServer_ProducesValidJSON(t *testing.T) {
	env := protocol.ServerEnvelope{
		Type:       protocol.TypeServerHello,
		ServerTime: 1710000000030,
		ServerTick: 0,
		Payload:    map[string]string{"sessionId": "sess_abc"},
	}
	data, err := protocol.MarshalServer(env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if out["type"] != protocol.TypeServerHello {
		t.Errorf("type field: got %v", out["type"])
	}
	if out["serverTime"] == nil {
		t.Error("serverTime field missing")
	}
}

func TestMarshalServer_SetsServerTimeWhenZero(t *testing.T) {
	env := protocol.ServerEnvelope{Type: protocol.TypeServerPing, Payload: map[string]string{}}
	data, _ := protocol.MarshalServer(env)
	var out map[string]any
	json.Unmarshal(data, &out)
	st, ok := out["serverTime"].(float64)
	if !ok || st == 0 {
		t.Errorf("serverTime should be auto-set when zero, got %v", out["serverTime"])
	}
}

func TestMakeError_ContainsCode(t *testing.T) {
	data, err := protocol.MakeError("room_not_found", "Room not found.", "req_001", true, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if out["type"] != protocol.TypeError {
		t.Errorf("type: got %v, want %q", out["type"], protocol.TypeError)
	}
	payload, ok := out["payload"].(map[string]any)
	if !ok {
		t.Fatal("payload not an object")
	}
	if payload["code"] != "room_not_found" {
		t.Errorf("code: got %v", payload["code"])
	}
	if payload["recoverable"] != true {
		t.Errorf("recoverable: got %v", payload["recoverable"])
	}
	if payload["requestId"] != "req_001" {
		t.Errorf("requestId: got %v", payload["requestId"])
	}
}
