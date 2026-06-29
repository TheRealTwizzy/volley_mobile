package physics_test

import (
	"math"
	"testing"

	"github.com/pong-mobile/backend/internal/game"
	"github.com/pong-mobile/backend/internal/physics"
)

const eps = 1e-9

func TestMoveBall_MovesAlongVelocity(t *testing.T) {
	ball := game.BallState{X: 0.5, Y: 0.5, VX: 1.0, VY: 0.0}
	result := physics.MoveBall(ball, 0.55, 1.0/30.0)
	want := 0.5 + 0.55*(1.0/30.0)
	if math.Abs(result.X-want) > eps {
		t.Errorf("X: got %v, want %v", result.X, want)
	}
	if math.Abs(result.Y-0.5) > eps {
		t.Errorf("Y should not change: got %v", result.Y)
	}
}

func TestWallCollide_LeftWall(t *testing.T) {
	radius := 0.018
	ball := game.BallState{X: radius - 0.001, Y: 0.5, VX: -0.8, VY: 0.6}
	result := physics.WallCollide(ball, radius)
	if result.X < radius {
		t.Errorf("ball clipped left wall: X=%v, radius=%v", result.X, radius)
	}
	if result.VX <= 0 {
		t.Errorf("VX should be positive after left wall bounce, got %v", result.VX)
	}
}

func TestWallCollide_RightWall(t *testing.T) {
	radius := 0.018
	ball := game.BallState{X: 1.0 - radius + 0.001, Y: 0.5, VX: 0.8, VY: 0.6}
	result := physics.WallCollide(ball, radius)
	if result.X > 1.0-radius {
		t.Errorf("ball clipped right wall: X=%v", result.X)
	}
	if result.VX >= 0 {
		t.Errorf("VX should be negative after right wall bounce, got %v", result.VX)
	}
}

func TestWallCollide_NoCollision(t *testing.T) {
	ball := game.BallState{X: 0.5, Y: 0.5, VX: 0.8, VY: 0.6}
	result := physics.WallCollide(ball, 0.018)
	if result != ball {
		t.Errorf("ball should be unchanged when no wall collision")
	}
}

func TestMovePaddle_MovesTowardTarget(t *testing.T) {
	got := physics.MovePaddle(0.5, 0.7, 0.9, 1.0/30.0, 0.22)
	want := 0.5 + 0.03
	if math.Abs(got-want) > eps {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMovePaddle_ReachesTarget(t *testing.T) {
	got := physics.MovePaddle(0.5, 0.501, 0.9, 1.0/30.0, 0.22)
	if math.Abs(got-0.501) > eps {
		t.Errorf("should snap to target: got %v", got)
	}
}

func TestMovePaddle_ClampedAtBounds(t *testing.T) {
	paddleWidth := 0.22
	minX := paddleWidth / 2
	maxX := 1.0 - paddleWidth/2
	got := physics.MovePaddle(maxX, 1.0, 0.9, 1.0/30.0, paddleWidth)
	if got > maxX+eps {
		t.Errorf("paddle exceeded right bound: %v > %v", got, maxX)
	}
	got = physics.MovePaddle(minX, 0.0, 0.9, 1.0/30.0, paddleWidth)
	if got < minX-eps {
		t.Errorf("paddle exceeded left bound: %v < %v", got, minX)
	}
}

func TestPaddleCollide_Player1HitCenter(t *testing.T) {
	radius := 0.018
	paddleX := 0.5
	paddleY := 0.93
	paddleW := 0.22
	paddleH := 0.025
	maxAngle := 60.0 * math.Pi / 180.0

	ball := game.BallState{
		X: 0.5, Y: paddleY - paddleH/2 - radius + 0.001,
		VX: 0.0, VY: 1.0,
	}
	result, hit := physics.PaddleCollide(ball, paddleX, paddleY, paddleW, paddleH, radius, maxAngle, true)
	if !hit {
		t.Fatal("expected collision with player 1 paddle")
	}
	if result.VY >= 0 {
		t.Errorf("ball should bounce upward after P1 hit: VY=%v", result.VY)
	}
	length := math.Sqrt(result.VX*result.VX + result.VY*result.VY)
	if math.Abs(length-1.0) > 1e-6 {
		t.Errorf("velocity not normalized after collision: length=%v", length)
	}
}

func TestPaddleCollide_Player2HitCenter(t *testing.T) {
	radius := 0.018
	paddleX := 0.5
	paddleY := 0.07
	paddleW := 0.22
	paddleH := 0.025
	maxAngle := 60.0 * math.Pi / 180.0

	ball := game.BallState{
		X: 0.5, Y: paddleY + paddleH/2 + radius - 0.001,
		VX: 0.0, VY: -1.0,
	}
	result, hit := physics.PaddleCollide(ball, paddleX, paddleY, paddleW, paddleH, radius, maxAngle, false)
	if !hit {
		t.Fatal("expected collision with player 2 paddle")
	}
	if result.VY <= 0 {
		t.Errorf("ball should bounce downward after P2 hit: VY=%v", result.VY)
	}
}

func TestPaddleCollide_NoCollision_WrongDirection(t *testing.T) {
	radius := 0.018
	ball := game.BallState{X: 0.5, Y: 0.93, VX: 0.0, VY: -1.0}
	_, hit := physics.PaddleCollide(ball, 0.5, 0.93, 0.22, 0.025, radius, 60*math.Pi/180, true)
	if hit {
		t.Error("ball moving away from P1 should not trigger collision")
	}
}

func TestCheckScore_Player1Scores(t *testing.T) {
	ball := game.BallState{X: 0.5, Y: -0.019, VX: 0.0, VY: -1.0}
	scored, slot := physics.CheckScore(ball, 0.018)
	if !scored {
		t.Fatal("expected score")
	}
	if slot != 0 {
		t.Errorf("expected slot 0 (P1 scores), got %d", slot)
	}
}

func TestCheckScore_Player2Scores(t *testing.T) {
	ball := game.BallState{X: 0.5, Y: 1.019, VX: 0.0, VY: 1.0}
	scored, slot := physics.CheckScore(ball, 0.018)
	if !scored {
		t.Fatal("expected score")
	}
	if slot != 1 {
		t.Errorf("expected slot 1 (P2 scores), got %d", slot)
	}
}

func TestCheckScore_NoScore(t *testing.T) {
	ball := game.BallState{X: 0.5, Y: 0.5, VX: 0.0, VY: 1.0}
	scored, _ := physics.CheckScore(ball, 0.018)
	if scored {
		t.Error("should not score when ball is in play")
	}
}
