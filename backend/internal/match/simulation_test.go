package match_test

import (
	"testing"

	"github.com/pong-mobile/backend/internal/config"
	"github.com/pong-mobile/backend/internal/game"
	"github.com/pong-mobile/backend/internal/match"
)

func newTestMatch() match.MatchState {
	m := match.NewMatchState("test-match", config.Default)
	m.Status = match.StatusActive
	return m
}

func TestTick_AdvancesServerTick(t *testing.T) {
	m := newTestMatch()
	m.Ball = game.BallState{X: 0.5, Y: 0.5, VX: 0.0, VY: 1.0}
	result, _ := match.Tick(m)
	if result.ServerTick != 1 {
		t.Errorf("ServerTick should be 1, got %d", result.ServerTick)
	}
}

func TestTick_BallMoves(t *testing.T) {
	m := newTestMatch()
	m.Ball = game.BallState{X: 0.5, Y: 0.5, VX: 0.0, VY: 1.0}
	startY := m.Ball.Y
	result, _ := match.Tick(m)
	if result.Ball.Y <= startY {
		t.Errorf("ball Y should increase when VY=1.0, got %v", result.Ball.Y)
	}
}

func TestTick_DetectsScore(t *testing.T) {
	m := newTestMatch()
	// Ball past bottom boundary — P1 missed — P2 scores (slot 1)
	m.Ball = game.BallState{X: 0.5, Y: 1.019, VX: 0.0, VY: 1.0}
	_, tr := match.Tick(m)
	if !tr.Scored {
		t.Fatal("expected score event")
	}
	if tr.ScoringSlot != 1 {
		t.Errorf("expected P2 (slot 1) to score, got slot %d", tr.ScoringSlot)
	}
}

func TestTick_DetectsMatchEnd(t *testing.T) {
	m := newTestMatch()
	m.Players[1].Score = config.Default.PointsToWin - 1
	// Ball past bottom — P2 scores and wins
	m.Ball = game.BallState{X: 0.5, Y: 1.019, VX: 0.0, VY: 1.0}
	_, tr := match.Tick(m)
	if !tr.MatchEnded {
		t.Fatal("expected match to end when player reaches pointsToWin")
	}
	if tr.WinnerSlot != 1 {
		t.Errorf("expected winner slot 1, got %d", tr.WinnerSlot)
	}
}

func TestTick_InactiveMatchDoesNotAdvance(t *testing.T) {
	m := newTestMatch()
	m.Status = match.StatusWaiting
	m.Ball = game.BallState{X: 0.5, Y: 0.5, VX: 0.0, VY: 1.0}
	result, _ := match.Tick(m)
	if result.Ball.Y != m.Ball.Y {
		t.Error("ball should not move when match is not active")
	}
}

func TestRallyReset_BallAtCenter(t *testing.T) {
	m := newTestMatch()
	m.Ball = game.BallState{X: 0.1, Y: 0.9, VX: 0.0, VY: 0.0}
	result := match.RallyReset(m, 0)
	if result.Ball.X != 0.5 || result.Ball.Y != 0.5 {
		t.Errorf("ball should reset to center, got (%v, %v)", result.Ball.X, result.Ball.Y)
	}
}

func TestRallyReset_Player1ScoredBallLaunchesTowardP2(t *testing.T) {
	// P1 scored (slot 0) → ball launches toward P2 (top, VY < 0)
	m := newTestMatch()
	result := match.RallyReset(m, 0)
	if result.Ball.VY >= 0 {
		t.Errorf("after P1 scores, ball should launch toward P2 (VY<0), got %v", result.Ball.VY)
	}
}

func TestRallyReset_Player2ScoredBallLaunchesTowardP1(t *testing.T) {
	// P2 scored (slot 1) → ball launches toward P1 (bottom, VY > 0)
	m := newTestMatch()
	result := match.RallyReset(m, 1)
	if result.Ball.VY <= 0 {
		t.Errorf("after P2 scores, ball should launch toward P1 (VY>0), got %v", result.Ball.VY)
	}
}

func TestSetPlayerTarget_UpdatesTargetX(t *testing.T) {
	m := newTestMatch()
	result := match.SetPlayerTarget(m, 0, 0.7, 1)
	if result.Players[0].TargetX != 0.7 {
		t.Errorf("TargetX should be 0.7, got %v", result.Players[0].TargetX)
	}
	if result.Players[0].LastInputSeq != 1 {
		t.Errorf("LastInputSeq should be 1, got %v", result.Players[0].LastInputSeq)
	}
}

func TestSetPlayerTarget_IgnoresOldSeq(t *testing.T) {
	m := newTestMatch()
	m.Players[0].TargetX = 0.6
	m.Players[0].LastInputSeq = 5
	result := match.SetPlayerTarget(m, 0, 0.9, 3)
	if result.Players[0].TargetX != 0.6 {
		t.Error("old seq input should be ignored")
	}
}

func TestSetPlayerTarget_ClampedToValidRange(t *testing.T) {
	m := newTestMatch()
	result := match.SetPlayerTarget(m, 0, 1.5, 1)
	if result.Players[0].TargetX > 1.0 {
		t.Errorf("targetX should be clamped to [0,1], got %v", result.Players[0].TargetX)
	}
}
