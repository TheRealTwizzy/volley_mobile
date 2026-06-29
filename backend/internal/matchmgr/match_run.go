package matchmgr

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pong-mobile/backend/internal/config"
	"github.com/pong-mobile/backend/internal/lobby"
	"github.com/pong-mobile/backend/internal/match"
	"github.com/pong-mobile/backend/internal/protocol"
)

// InputMsg is one client paddle input to be applied on the next tick.
type InputMsg struct {
	Slot    int     // 0=P1, 1=P2
	TargetX float64
	Seq     int
}

// PlayerConn holds one player's connection state in a running match.
type PlayerConn struct {
	Sender            lobby.Sender  // nil if disconnected
	ConnID            string
	SessionID         string        // auth.Session.ID for reconnect matching
	ReconnectDeadline time.Time     // zero = connected
}

// MatchRun is the complete state for one running match.
type MatchRun struct {
	MatchID     string
	State       match.MatchState
	Players     [2]PlayerConn
	InputCh     chan InputMsg
	reconnectCh chan int // slot index signaling reconnect
	mu          sync.Mutex
	cancelFn    context.CancelFunc
	onEnd       func(*MatchRun)
}

// newMatchRun creates a MatchRun without starting the goroutine.
func newMatchRun(matchID string, settings config.Settings, players [2]PlayerConn, onEnd func(*MatchRun)) *MatchRun {
	return &MatchRun{
		MatchID:     matchID,
		State:       match.NewMatchState(matchID, settings),
		Players:     players,
		InputCh:     make(chan InputMsg, 64),
		reconnectCh: make(chan int, 4),
		onEnd:       onEnd,
	}
}

// send sends data to a single player by slot; no-op if the Sender is nil.
func (r *MatchRun) send(slot int, data []byte) {
	r.mu.Lock()
	s := r.Players[slot].Sender
	r.mu.Unlock()
	if s != nil {
		s.SendBytes(data)
	}
}

// broadcast sends data to both players.
func (r *MatchRun) broadcast(data []byte) {
	r.send(0, data)
	r.send(1, data)
}

// matchLoop is the main goroutine for a match.
// Phase 1: countdown → Phase 2: active loop.
func matchLoop(ctx context.Context, run *MatchRun) {
	defer run.onEnd(run)

	// Phase 1: countdown
	startsAt := time.Now().Add(3 * time.Second)
	countdown := BuildCountdown(run.MatchID, startsAt)
	run.broadcast(countdown)
	time.Sleep(time.Until(startsAt))

	// Set state to Active
	run.mu.Lock()
	run.State.Status = match.StatusActive
	run.mu.Unlock()

	// Send match.started individually per player
	run.mu.Lock()
	state := run.State
	run.mu.Unlock()
	for slot := 0; slot < 2; slot++ {
		run.send(slot, BuildStarted(state, run.MatchID, slot))
	}

	// Phase 2: active loop
	matchLoopActive(ctx, run)
}

// matchLoopActive is a package-level var to allow test overrides.
var matchLoopActive = func(ctx context.Context, run *MatchRun) {
	ticker := time.NewTicker(time.Second / 30)
	defer ticker.Stop()

	tickCount := 0
	var paused atomic.Bool
	var pausedScoringSlot int

	for {
		select {
		case <-ctx.Done():
			return

		case slot := <-run.reconnectCh:
			if ctx.Err() != nil {
				return
			}
			// Unpause and handle reconnect
			paused.Store(false)
			run.mu.Lock()
			run.Players[slot].ReconnectDeadline = time.Time{}
			state := run.State
			run.mu.Unlock()

			// Send match.reconnected to reconnecting player
			run.send(slot, buildReconnected(run.MatchID, state, slot))
			// Notify other player
			run.send(1-slot, buildPlayerReconnected(run.MatchID, slot))

		case <-ticker.C:
			if paused.Load() {
				continue
			}

			// Drain all pending inputs
			run.mu.Lock()
		drainLoop:
			for {
				select {
				case inp := <-run.InputCh:
					run.State = match.SetPlayerTarget(run.State, inp.Slot, inp.TargetX, inp.Seq)
				default:
					break drainLoop
				}
			}

			// Check for newly disconnected players (Sender nil but no deadline set yet)
			disconnected := false
			for slot := 0; slot < 2; slot++ {
				if run.Players[slot].Sender == nil && run.Players[slot].ReconnectDeadline.IsZero() {
					reconnectWindowMs := run.State.Settings.ReconnectWindowMs
					deadline := time.Now().Add(time.Duration(reconnectWindowMs) * time.Millisecond)
					run.Players[slot].ReconnectDeadline = deadline
					matchID := run.MatchID
					disconnSlot := slot
					run.mu.Unlock()

					// Notify other player of disconnect
					run.send(1-disconnSlot, buildPlayerDisconnected(matchID, disconnSlot, deadline))

					// Schedule forfeit
					time.AfterFunc(time.Duration(reconnectWindowMs)*time.Millisecond, func() {
						run.mu.Lock()
						stillDisconnected := run.Players[disconnSlot].Sender == nil
						run.mu.Unlock()
						if stillDisconnected {
							winnerSlot := 1 - disconnSlot
							run.broadcast(buildMatchEnded(matchID, winnerSlot, "forfeit"))
							if run.cancelFn != nil {
								run.cancelFn()
							}
						}
					})

					paused.Store(true)
					disconnected = true
					break
				}
			}
			if disconnected {
				continue // skip simulation this tick; lock already released
			}

			// Advance simulation (lock still held from drain loop above)
			newState, result := match.Tick(run.State)
			run.State = newState
			run.mu.Unlock()

			tickCount++

			if result.Scored {
				scoringSlot := result.ScoringSlot
				run.mu.Lock()
				state := run.State
				run.mu.Unlock()
				run.broadcast(buildScoreMsg(run.MatchID, state, scoringSlot))

				if result.MatchEnded {
					run.broadcast(buildMatchEnded(run.MatchID, result.WinnerSlot, "score"))
					return
				}

				// Pause for rally reset
				paused.Store(true)
				pausedScoringSlot = scoringSlot
				resetDelay := time.Duration(run.State.Settings.RallyResetMs) * time.Millisecond
				go func(slot int) {
					time.Sleep(resetDelay)
					run.mu.Lock()
					run.State = match.RallyReset(run.State, slot)
					state := run.State
					run.mu.Unlock()
					run.broadcast(buildRallyReset(run.MatchID, state))
					tickCount = 0
					paused.Store(false)
				}(pausedScoringSlot)
				continue
			}

			// Broadcast snapshot every 3rd tick
			if tickCount%3 == 0 {
				run.mu.Lock()
				state := run.State
				run.mu.Unlock()
				run.broadcast(BuildSnapshot(state, run.MatchID))
			}
		}
	}
}

// ---- Parser helpers (used by manager.go in Task 3) ----

type inputPaddleTargetPayload struct {
	MatchID string  `json:"matchId"`
	TargetX float64 `json:"targetX"`
	Seq     int     `json:"clientSeq"`
}

// ParseInputPaddleTarget extracts matchID, targetX, seq from a raw input.paddle_target payload.
func ParseInputPaddleTarget(rawPayload json.RawMessage) (matchID string, targetX float64, seq int, ok bool) {
	var p inputPaddleTargetPayload
	if err := json.Unmarshal(rawPayload, &p); err != nil {
		return "", 0, 0, false
	}
	if p.MatchID == "" {
		return "", 0, 0, false
	}
	return p.MatchID, p.TargetX, p.Seq, true
}

type reconnectPayload struct {
	SessionToken string `json:"sessionToken"`
	MatchID      string `json:"matchId"`
}

// ParseReconnectPayload extracts sessionToken, matchID from a raw match.reconnect payload.
func ParseReconnectPayload(rawPayload json.RawMessage) (sessionToken, matchID string, ok bool) {
	var p reconnectPayload
	if err := json.Unmarshal(rawPayload, &p); err != nil {
		return "", "", false
	}
	if p.SessionToken == "" || p.MatchID == "" {
		return "", "", false
	}
	return p.SessionToken, p.MatchID, true
}

// ---- Private message builders ----

type scorePayload struct {
	MatchID     string `json:"matchId"`
	ScoringSlot int    `json:"scoringSlot"`
	Scores      struct {
		P1 int `json:"p1"`
		P2 int `json:"p2"`
	} `json:"scores"`
}

func buildScoreMsg(matchID string, state match.MatchState, scoringSlot int) []byte {
	p := scorePayload{MatchID: matchID, ScoringSlot: scoringSlot}
	p.Scores.P1 = state.Players[0].Score
	p.Scores.P2 = state.Players[1].Score
	data, err := protocol.MarshalServer(protocol.ServerEnvelope{
		Type:       protocol.TypeMatchScore,
		ServerTick: state.ServerTick,
		Payload:    p,
	})
	if err != nil {
		panic(err)
	}
	return data
}

type rallyResetPayload struct {
	MatchID string             `json:"matchId"`
	Ball    snapshotBallPayload `json:"ball"`
}

func buildRallyReset(matchID string, state match.MatchState) []byte {
	p := rallyResetPayload{
		MatchID: matchID,
		Ball:    snapshotBallPayload{X: state.Ball.X, Y: state.Ball.Y, VX: state.Ball.VX, VY: state.Ball.VY},
	}
	data, err := protocol.MarshalServer(protocol.ServerEnvelope{
		Type:       protocol.TypeMatchRallyReset,
		ServerTick: state.ServerTick,
		Payload:    p,
	})
	if err != nil {
		panic(err)
	}
	return data
}

type matchEndedPayload struct {
	MatchID    string `json:"matchId"`
	WinnerSlot int    `json:"winnerSlot"`
	Reason     string `json:"reason"`
}

func buildMatchEnded(matchID string, winnerSlot int, reason string) []byte {
	p := matchEndedPayload{MatchID: matchID, WinnerSlot: winnerSlot, Reason: reason}
	data, err := protocol.MarshalServer(protocol.ServerEnvelope{
		Type:    protocol.TypeMatchEnded,
		Payload: p,
	})
	if err != nil {
		panic(err)
	}
	return data
}

type playerDisconnectedPayload struct {
	MatchID          string `json:"matchId"`
	Slot             int    `json:"slot"`
	ReconnectDeadline int64 `json:"reconnectDeadline"` // unix ms
}

func buildPlayerDisconnected(matchID string, slot int, deadline time.Time) []byte {
	p := playerDisconnectedPayload{
		MatchID:           matchID,
		Slot:              slot,
		ReconnectDeadline: deadline.UnixMilli(),
	}
	data, err := protocol.MarshalServer(protocol.ServerEnvelope{
		Type:    protocol.TypePlayerDisconnected,
		Payload: p,
	})
	if err != nil {
		panic(err)
	}
	return data
}

type playerReconnectedPayload struct {
	MatchID string `json:"matchId"`
	Slot    int    `json:"slot"`
}

func buildPlayerReconnected(matchID string, slot int) []byte {
	p := playerReconnectedPayload{MatchID: matchID, Slot: slot}
	data, err := protocol.MarshalServer(protocol.ServerEnvelope{
		Type:    protocol.TypePlayerReconnected,
		Payload: p,
	})
	if err != nil {
		panic(err)
	}
	return data
}

type reconnectedPayload struct {
	MatchID      string             `json:"matchId"`
	PlayerSlot   int                `json:"playerSlot"`
	ServerTick   int64              `json:"serverTick"`
	Ball         snapshotBallPayload `json:"ball"`
	Players      struct {
		P1 snapshotPlayerEntry `json:"p1"`
		P2 snapshotPlayerEntry `json:"p2"`
	} `json:"players"`
}

func buildReconnected(matchID string, state match.MatchState, slot int) []byte {
	p := reconnectedPayload{
		MatchID:    matchID,
		PlayerSlot: slot,
		ServerTick: state.ServerTick,
		Ball:       snapshotBallPayload{X: state.Ball.X, Y: state.Ball.Y, VX: state.Ball.VX, VY: state.Ball.VY},
	}
	p.Players.P1 = snapshotPlayerEntry{PaddleX: state.Players[0].PaddleX, Score: state.Players[0].Score}
	p.Players.P2 = snapshotPlayerEntry{PaddleX: state.Players[1].PaddleX, Score: state.Players[1].Score}
	data, err := protocol.MarshalServer(protocol.ServerEnvelope{
		Type:       protocol.TypeMatchReconnected,
		ServerTick: state.ServerTick,
		Payload:    p,
	})
	if err != nil {
		panic(err)
	}
	return data
}
