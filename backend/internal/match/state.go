package match

import (
	"github.com/pong-mobile/backend/internal/config"
	"github.com/pong-mobile/backend/internal/game"
)

// Status represents the current phase of a match.
type Status string

const (
	StatusWaiting   Status = "waiting"
	StatusCountdown Status = "countdown"
	StatusActive    Status = "active"
	StatusEnded     Status = "ended"
)

// MatchState is the complete authoritative server-side match state.
type MatchState struct {
	MatchID    string
	Status     Status
	ServerTick int64
	Ball       game.BallState
	Players    [2]game.PlayerState
	Settings   config.Settings
}

// NewMatchState initializes a match with ball at center, paddles centered.
func NewMatchState(matchID string, settings config.Settings) MatchState {
	return MatchState{
		MatchID:  matchID,
		Status:   StatusWaiting,
		Settings: settings,
		Ball: game.BallState{
			X:  0.5,
			Y:  0.5,
			VX: 0.0,
			VY: 1.0,
		},
		Players: [2]game.PlayerState{
			{PaddleX: 0.5, TargetX: 0.5, Connected: true},
			{PaddleX: 0.5, TargetX: 0.5, Connected: true},
		},
	}
}
