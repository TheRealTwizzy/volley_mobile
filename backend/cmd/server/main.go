package main

import (
	"fmt"

	"github.com/pong-mobile/backend/internal/config"
	"github.com/pong-mobile/backend/internal/match"
)

func main() {
	state := match.NewMatchState("smoke-test", config.Default)
	state.Status = match.StatusActive

	// Simulate 30 ticks (1 second of game time).
	for i := 0; i < 30; i++ {
		var tr match.TickResult
		state, tr = match.Tick(state)
		if tr.Scored {
			fmt.Printf("tick %d: slot %d scored — P1=%d P2=%d\n",
				state.ServerTick, tr.ScoringSlot,
				state.Players[0].Score, state.Players[1].Score)
			if tr.MatchEnded {
				fmt.Printf("match ended, winner slot %d\n", tr.WinnerSlot)
				return
			}
			state = match.RallyReset(state, tr.ScoringSlot)
		}
	}
	fmt.Printf("smoke test complete: %d ticks, ball at (%.3f, %.3f)\n",
		state.ServerTick, state.Ball.X, state.Ball.Y)
}
