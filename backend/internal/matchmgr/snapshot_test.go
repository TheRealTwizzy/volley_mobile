package matchmgr_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/pong-mobile/backend/internal/config"
	"github.com/pong-mobile/backend/internal/match"
	"github.com/pong-mobile/backend/internal/matchmgr"
	"github.com/pong-mobile/backend/internal/protocol"
)

func baseState() match.MatchState {
	s := match.NewMatchState("m1", config.Default)
	s.Status = match.StatusActive
	s.ServerTick = 7
	s.Players[0].Score = 2
	s.Players[1].Score = 1
	s.Players[0].PaddleX = 0.3
	s.Players[1].PaddleX = 0.7
	s.Ball.X = 0.5
	s.Ball.Y = 0.5
	s.Ball.VX = 0.1
	s.Ball.VY = -0.9
	return s
}

func TestBuildSnapshot_EnvelopeShape(t *testing.T) {
	raw := matchmgr.BuildSnapshot(baseState(), "m1")
	var env protocol.ServerEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Type != protocol.TypeMatchSnapshot {
		t.Errorf("type=%q want %q", env.Type, protocol.TypeMatchSnapshot)
	}
	if env.ServerTick != 7 {
		t.Errorf("serverTick=%d want 7", env.ServerTick)
	}
	if env.ServerTime == 0 {
		t.Error("serverTime must be nonzero")
	}
}

func TestBuildSnapshot_Payload(t *testing.T) {
	raw := matchmgr.BuildSnapshot(baseState(), "m1")
	var env struct {
		Payload struct {
			MatchID string  `json:"matchId"`
			Ball    struct {
				X float64 `json:"x"`
			} `json:"ball"`
			Players struct {
				P1 struct {
					PaddleX float64 `json:"paddleX"`
					Score   int     `json:"score"`
				} `json:"p1"`
				P2 struct {
					PaddleX float64 `json:"paddleX"`
					Score   int     `json:"score"`
				} `json:"p2"`
			} `json:"players"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Payload.MatchID != "m1" {
		t.Errorf("matchId=%q want m1", env.Payload.MatchID)
	}
	if env.Payload.Players.P1.Score != 2 {
		t.Errorf("p1.score=%d want 2", env.Payload.Players.P1.Score)
	}
	if env.Payload.Players.P2.PaddleX != 0.7 {
		t.Errorf("p2.paddleX=%.2f want 0.7", env.Payload.Players.P2.PaddleX)
	}
}

func TestBuildStarted_SlotAssignment(t *testing.T) {
	s := baseState()
	s.Players[0].Score = 0
	s.Players[1].Score = 0

	rawP1 := matchmgr.BuildStarted(s, "m1", 0)
	rawP2 := matchmgr.BuildStarted(s, "m1", 1)

	var envP1 struct {
		Payload struct {
			PlayerSlot   string `json:"playerSlot"`
			OpponentSlot string `json:"opponentSlot"`
		} `json:"payload"`
	}
	var envP2 struct {
		Payload struct {
			PlayerSlot   string `json:"playerSlot"`
			OpponentSlot string `json:"opponentSlot"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(rawP1, &envP1); err != nil {
		t.Fatalf("p1 unmarshal: %v", err)
	}
	if err := json.Unmarshal(rawP2, &envP2); err != nil {
		t.Fatalf("p2 unmarshal: %v", err)
	}
	if envP1.Payload.PlayerSlot != "p1" || envP1.Payload.OpponentSlot != "p2" {
		t.Errorf("p1 slots wrong: got %s/%s", envP1.Payload.PlayerSlot, envP1.Payload.OpponentSlot)
	}
	if envP2.Payload.PlayerSlot != "p2" || envP2.Payload.OpponentSlot != "p1" {
		t.Errorf("p2 slots wrong: got %s/%s", envP2.Payload.PlayerSlot, envP2.Payload.OpponentSlot)
	}
}

func TestBuildCountdown_Fields(t *testing.T) {
	startsAt := time.Now().Add(3 * time.Second)
	raw := matchmgr.BuildCountdown("m1", startsAt)
	var env struct {
		Type    string `json:"type"`
		Payload struct {
			MatchID    string `json:"matchId"`
			StartsAt   int64  `json:"startsAt"`
			DurationMs int    `json:"durationMs"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Type != protocol.TypeMatchCountdown {
		t.Errorf("type=%q want match.countdown", env.Type)
	}
	if env.Payload.MatchID != "m1" {
		t.Errorf("matchId=%q want m1", env.Payload.MatchID)
	}
	if env.Payload.DurationMs != 3000 {
		t.Errorf("durationMs=%d want 3000", env.Payload.DurationMs)
	}
	if env.Payload.StartsAt == 0 {
		t.Error("startsAt must be nonzero")
	}
}
