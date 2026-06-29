package config

import "math"

// Settings holds all tunable game constants.
type Settings struct {
	PointsToWin          int
	PaddleWidth          float64
	PaddleHeight         float64
	PaddleSpeed          float64 // normalized units/second
	BallRadius           float64
	BallSpeed            float64 // normalized units/second
	CountdownMs          int
	RallyResetMs         int
	ReconnectWindowMs    int
	MaxBounceAngle       float64 // radians
	Player1PaddleY       float64
	Player2PaddleY       float64
	TickRate             int
	SnapshotRate         int
	DT                   float64 // seconds per tick
	InterpolationDelayMs int
	MaxExtrapolationMs   int
}

// Default is the starting configuration per GAME_LOOP_SPEC.md.
var Default = Settings{
	PointsToWin:          5,
	PaddleWidth:          0.22,
	PaddleHeight:         0.025,
	PaddleSpeed:          0.9,
	BallRadius:           0.018,
	BallSpeed:            0.55,
	CountdownMs:          3000,
	RallyResetMs:         1500,
	ReconnectWindowMs:    10000,
	MaxBounceAngle:       60.0 * math.Pi / 180.0,
	Player1PaddleY:       0.93,
	Player2PaddleY:       0.07,
	TickRate:             30,
	SnapshotRate:         20,
	DT:                   1.0 / 30.0,
	InterpolationDelayMs: 100,
	MaxExtrapolationMs:   150,
}
