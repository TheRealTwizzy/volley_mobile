package physics

import (
	"math"

	"github.com/pong-mobile/backend/internal/game"
)

// MoveBall advances ball position one tick.
func MoveBall(ball game.BallState, speed, dt float64) game.BallState {
	ball.X += ball.VX * speed * dt
	ball.Y += ball.VY * speed * dt
	return ball
}

// WallCollide bounces the ball off left and right walls.
func WallCollide(ball game.BallState, radius float64) game.BallState {
	if ball.X-radius <= 0 {
		ball.X = radius
		ball.VX = math.Abs(ball.VX)
	}
	if ball.X+radius >= 1.0 {
		ball.X = 1.0 - radius
		ball.VX = -math.Abs(ball.VX)
	}
	return ball
}

// MovePaddle moves paddleX toward targetX capped at paddleSpeed*dt per tick.
// Result is clamped within paddle bounds.
func MovePaddle(paddleX, targetX, paddleSpeed, dt, paddleWidth float64) float64 {
	minX := paddleWidth / 2
	maxX := 1.0 - paddleWidth/2

	delta := targetX - paddleX
	maxStep := paddleSpeed * dt
	if math.Abs(delta) <= maxStep {
		paddleX = targetX
	} else {
		paddleX += math.Copysign(maxStep, delta)
	}
	return math.Max(minX, math.Min(maxX, paddleX))
}

// PaddleCollide checks whether the ball intersects the paddle and computes bounce.
//
// isPlayer1=true: paddle at bottom (paddleY≈0.93), ball must move down (VY>0).
// isPlayer1=false: paddle at top (paddleY≈0.07), ball must move up (VY<0).
func PaddleCollide(
	ball game.BallState,
	paddleX, paddleY, paddleWidth, paddleHeight, radius, maxBounceAngle float64,
	isPlayer1 bool,
) (game.BallState, bool) {
	if isPlayer1 && ball.VY <= 0 {
		return ball, false
	}
	if !isPlayer1 && ball.VY >= 0 {
		return ball, false
	}

	left := paddleX - paddleWidth/2
	right := paddleX + paddleWidth/2
	top := paddleY - paddleHeight/2
	bottom := paddleY + paddleHeight/2

	if ball.X+radius < left || ball.X-radius > right {
		return ball, false
	}
	if ball.Y+radius < top || ball.Y-radius > bottom {
		return ball, false
	}

	rel := (ball.X - paddleX) / (paddleWidth / 2)
	rel = math.Max(-1.0, math.Min(1.0, rel))

	var angle float64
	if isPlayer1 {
		angle = -math.Pi/2 + rel*maxBounceAngle
	} else {
		angle = math.Pi/2 - rel*maxBounceAngle
	}

	ball.VX = math.Cos(angle)
	ball.VY = math.Sin(angle)

	length := math.Sqrt(ball.VX*ball.VX + ball.VY*ball.VY)
	ball.VX /= length
	ball.VY /= length

	return ball, true
}

// CheckScore returns whether the ball crossed a scoring boundary and which slot scored.
// Slot 0 = P1 scored (ball exits top, P2 missed).
// Slot 1 = P2 scored (ball exits bottom, P1 missed).
func CheckScore(ball game.BallState, radius float64) (bool, int) {
	if ball.Y+radius < 0 {
		return true, 0
	}
	if ball.Y-radius > 1.0 {
		return true, 1
	}
	return false, -1
}
