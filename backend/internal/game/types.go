package game

// BallState holds authoritative ball position and velocity in normalized [0,1] coords.
// VX and VY form a unit vector; speed is stored separately in config.Settings.
type BallState struct {
	X  float64
	Y  float64
	VX float64
	VY float64
}

// PlayerState holds per-player paddle state.
// Slot 0 = Player 1 (bottom, paddleY=0.93), Slot 1 = Player 2 (top, paddleY=0.07).
type PlayerState struct {
	PaddleX      float64
	TargetX      float64
	Score        int
	Connected    bool
	LastInputSeq int
}
