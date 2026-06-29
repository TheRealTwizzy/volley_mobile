package matchmgr

import (
	"time"

	"github.com/pong-mobile/backend/internal/match"
	"github.com/pong-mobile/backend/internal/protocol"
)

type snapshotBallPayload struct {
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	VX float64 `json:"vx"`
	VY float64 `json:"vy"`
}

type snapshotPlayerEntry struct {
	PaddleX float64 `json:"paddleX"`
	Score   int     `json:"score"`
}

type snapshotPayload struct {
	MatchID string `json:"matchId"`
	Ball    snapshotBallPayload `json:"ball"`
	Players struct {
		P1 snapshotPlayerEntry `json:"p1"`
		P2 snapshotPlayerEntry `json:"p2"`
	} `json:"players"`
}

// BuildSnapshot marshals a match.snapshot server envelope.
func BuildSnapshot(state match.MatchState, matchID string) []byte {
	p := snapshotPayload{MatchID: matchID}
	p.Ball = snapshotBallPayload{
		X: state.Ball.X, Y: state.Ball.Y,
		VX: state.Ball.VX, VY: state.Ball.VY,
	}
	p.Players.P1 = snapshotPlayerEntry{PaddleX: state.Players[0].PaddleX, Score: state.Players[0].Score}
	p.Players.P2 = snapshotPlayerEntry{PaddleX: state.Players[1].PaddleX, Score: state.Players[1].Score}

	data, err := protocol.MarshalServer(protocol.ServerEnvelope{
		Type:       protocol.TypeMatchSnapshot,
		ServerTick: state.ServerTick,
		Payload:    p,
	})
	if err != nil {
		panic(err)
	}
	return data
}

type startedPlayerEntry struct {
	PlayerID string  `json:"playerId"`
	PaddleX  float64 `json:"paddleX"`
	Score    int     `json:"score"`
}

type startedInitialState struct {
	Ball    snapshotBallPayload `json:"ball"`
	Players struct {
		P1 startedPlayerEntry `json:"p1"`
		P2 startedPlayerEntry `json:"p2"`
	} `json:"players"`
}

type startedPayload struct {
	MatchID      string              `json:"matchId"`
	PlayerSlot   string              `json:"playerSlot"`
	OpponentSlot string              `json:"opponentSlot"`
	Settings     startedSettings     `json:"settings"`
	InitialState startedInitialState `json:"initialState"`
}

type startedSettings struct {
	PointsToWin int `json:"pointsToWin"`
	TickRate    int `json:"tickRate"`
	SnapshotRate int `json:"snapshotRate"`
}

// BuildStarted marshals a per-player match.started envelope.
// slot: 0=P1, 1=P2.
func BuildStarted(state match.MatchState, matchID string, slot int) []byte {
	slotNames := [2]string{"p1", "p2"}
	p := startedPayload{
		MatchID:      matchID,
		PlayerSlot:   slotNames[slot],
		OpponentSlot: slotNames[1-slot],
		Settings: startedSettings{
			PointsToWin:  state.Settings.PointsToWin,
			TickRate:     state.Settings.TickRate,
			SnapshotRate: state.Settings.SnapshotRate,
		},
	}
	p.InitialState.Ball = snapshotBallPayload{
		X: state.Ball.X, Y: state.Ball.Y,
		VX: state.Ball.VX, VY: state.Ball.VY,
	}
	p.InitialState.Players.P1 = startedPlayerEntry{PaddleX: state.Players[0].PaddleX}
	p.InitialState.Players.P2 = startedPlayerEntry{PaddleX: state.Players[1].PaddleX}

	data, err := protocol.MarshalServer(protocol.ServerEnvelope{
		Type:       protocol.TypeMatchStarted,
		ServerTick: state.ServerTick,
		Payload:    p,
	})
	if err != nil {
		panic(err)
	}
	return data
}

type countdownPayload struct {
	MatchID    string `json:"matchId"`
	StartsAt   int64  `json:"startsAt"`
	DurationMs int    `json:"durationMs"`
}

// BuildCountdown marshals a match.countdown envelope.
func BuildCountdown(matchID string, startsAt time.Time) []byte {
	p := countdownPayload{
		MatchID:    matchID,
		StartsAt:   startsAt.UnixMilli(),
		DurationMs: 3000,
	}

	data, err := protocol.MarshalServer(protocol.ServerEnvelope{
		Type:       protocol.TypeMatchCountdown,
		ServerTick: 0,
		Payload:    p,
	})
	if err != nil {
		panic(err)
	}
	return data
}
