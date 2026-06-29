package match

import (
	"math"
	"math/rand"

	"github.com/pong-mobile/backend/internal/game"
	"github.com/pong-mobile/backend/internal/physics"
)

// TickResult reports what happened during a single server tick.
type TickResult struct {
	Scored      bool
	ScoringSlot int // 0=P1 scored, 1=P2 scored
	MatchEnded  bool
	WinnerSlot  int
}

// Tick advances the match state by one server tick (dt = 1/tickRate seconds).
// Returns the updated state and a TickResult. If match is not StatusActive, state is unchanged.
func Tick(state MatchState) (MatchState, TickResult) {
	if state.Status != StatusActive {
		return state, TickResult{}
	}

	cfg := state.Settings
	dt := cfg.DT

	for i := range state.Players {
		state.Players[i].PaddleX = physics.MovePaddle(
			state.Players[i].PaddleX,
			state.Players[i].TargetX,
			cfg.PaddleSpeed,
			dt,
			cfg.PaddleWidth,
		)
	}

	state.Ball = physics.MoveBall(state.Ball, cfg.BallSpeed, dt)
	state.Ball = physics.WallCollide(state.Ball, cfg.BallRadius)

	// Slot 0 = P1, bottom paddle (Player1PaddleY).
	// Slot 1 = P2, top paddle (Player2PaddleY).
	state.Ball, _ = physics.PaddleCollide(
		state.Ball,
		state.Players[0].PaddleX, cfg.Player1PaddleY,
		cfg.PaddleWidth, cfg.PaddleHeight,
		cfg.BallRadius, cfg.MaxBounceAngle,
		true,
	)
	state.Ball, _ = physics.PaddleCollide(
		state.Ball,
		state.Players[1].PaddleX, cfg.Player2PaddleY,
		cfg.PaddleWidth, cfg.PaddleHeight,
		cfg.BallRadius, cfg.MaxBounceAngle,
		false,
	)

	var result TickResult
	if scored, slot := physics.CheckScore(state.Ball, cfg.BallRadius); scored {
		state.Players[slot].Score++
		result.Scored = true
		result.ScoringSlot = slot

		if state.Players[slot].Score >= cfg.PointsToWin {
			state.Status = StatusEnded
			result.MatchEnded = true
			result.WinnerSlot = slot
		}
	}

	state.ServerTick++
	return state, result
}

// RallyReset resets the ball to center and launches toward the player who was just scored on.
// lastScoringSlot: 0 = P1 scored, 1 = P2 scored.
func RallyReset(state MatchState, lastScoringSlot int) MatchState {
	state.Ball = game.BallState{X: 0.5, Y: 0.5}

	var vy float64
	if lastScoringSlot == 0 {
		vy = -1.0 // P1 scored → launch toward P2 (top)
	} else {
		vy = 1.0 // P2 scored → launch toward P1 (bottom)
	}

	vx := (rand.Float64()*2 - 1) * 0.35
	length := math.Sqrt(vx*vx + vy*vy)
	state.Ball.VX = vx / length
	state.Ball.VY = vy / length

	return state
}

// SetPlayerTarget applies a client paddle input. Ignores stale or duplicate seq numbers.
// Clamps targetX to [0.0, 1.0].
func SetPlayerTarget(state MatchState, slot int, targetX float64, seq int) MatchState {
	if seq <= state.Players[slot].LastInputSeq {
		return state
	}
	state.Players[slot].TargetX = math.Max(0.0, math.Min(1.0, targetX))
	state.Players[slot].LastInputSeq = seq
	return state
}
