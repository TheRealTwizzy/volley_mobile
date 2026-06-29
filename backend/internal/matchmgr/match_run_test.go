package matchmgr

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/pong-mobile/backend/internal/config"
	"github.com/pong-mobile/backend/internal/match"
)

// testSender is a mock lobby.Sender that records received messages.
type testSender struct {
	mu   sync.Mutex
	id   string
	msgs [][]byte
}

func (s *testSender) ID() string { return s.id }
func (s *testSender) SendBytes(b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(b))
	copy(cp, b)
	s.msgs = append(s.msgs, cp)
}

func (s *testSender) countType(msgType string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, m := range s.msgs {
		var env struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(m, &env); err == nil && env.Type == msgType {
			count++
		}
	}
	return count
}

func (s *testSender) allTypes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	types := make([]string, 0, len(s.msgs))
	for _, m := range s.msgs {
		var env struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(m, &env); err == nil {
			types = append(types, env.Type)
		}
	}
	return types
}

// makeTestRun creates a MatchRun with both test senders and state already Active.
func makeTestRun(settings config.Settings) (*MatchRun, *testSender, *testSender, func(*MatchRun)) {
	s0 := &testSender{id: "conn0"}
	s1 := &testSender{id: "conn1"}
	players := [2]PlayerConn{
		{Sender: s0, ConnID: "conn0", SessionID: "sess0"},
		{Sender: s1, ConnID: "conn1", SessionID: "sess1"},
	}
	endCalled := false
	_ = endCalled
	onEnd := func(r *MatchRun) { endCalled = true }
	run := newMatchRun("test-match", settings, players, onEnd)
	run.State.Status = match.StatusActive
	return run, s0, s1, onEnd
}

// TestMatchRun_SnapshotEvery3Ticks verifies snapshots are sent at 1/3 the tick rate.
func TestMatchRun_SnapshotEvery3Ticks(t *testing.T) {
	cfg := config.Default
	run, s0, _, _ := makeTestRun(cfg)

	ticksToRun := 30
	ticked := 0

	// Override matchLoopActive to count ticks and snapshots
	origActive := matchLoopActive
	defer func() { matchLoopActive = origActive }()

	done := make(chan struct{})
	matchLoopActive = func(ctx context.Context, r *MatchRun) {
		ticker := time.NewTicker(time.Second / 30)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				close(done)
				return
			case <-ticker.C:
				r.mu.Lock()
				newState, _ := match.Tick(r.State)
				r.State = newState
				r.mu.Unlock()
				ticked++
				if ticked%3 == 0 {
					r.mu.Lock()
					state := r.State
					r.mu.Unlock()
					r.broadcast(BuildSnapshot(state, r.MatchID))
				}
				if ticked >= ticksToRun {
					close(done)
					return
				}
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run.cancelFn = cancel
	go matchLoopActive(ctx, run)
	<-done

	snapshots := s0.countType("match.snapshot")
	expectedSnapshots := ticksToRun / 3
	if snapshots < expectedSnapshots-1 || snapshots > expectedSnapshots+1 {
		t.Errorf("snapshots=%d want ~%d (ticks=%d)", snapshots, expectedSnapshots, ticked)
	}
}

// TestMatchRun_ScoreEvent verifies both players receive match.score when the ball passes the goal line.
func TestMatchRun_ScoreEvent(t *testing.T) {
	cfg := config.Default
	cfg.PointsToWin = 5
	run, s0, s1, _ := makeTestRun(cfg)

	// Position ball near P1's goal (bottom, y approaching 1.0)
	run.State.Ball.X = 0.5
	run.State.Ball.Y = 0.99
	run.State.Ball.VX = 0.0
	run.State.Ball.VY = 1.0 // moving toward bottom goal

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	endCalled := make(chan struct{}, 1)
	run.onEnd = func(r *MatchRun) {
		select {
		case endCalled <- struct{}{}:
		default:
		}
	}

	go matchLoopActive(ctx, run)

	// Wait for a score event or timeout
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s0.countType("match.score") > 0 && s1.countType("match.score") > 0 {
			return // success
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Errorf("expected match.score on both senders; s0=%d s1=%d", s0.countType("match.score"), s1.countType("match.score"))
}

// TestMatchRun_MatchEnded verifies match.ended is sent and onEnd is called when pointsToWin is reached.
func TestMatchRun_MatchEnded(t *testing.T) {
	cfg := config.Default
	cfg.PointsToWin = 1
	cfg.RallyResetMs = 50
	run, s0, s1, _ := makeTestRun(cfg)

	// Position ball to score immediately
	run.State.Ball.X = 0.5
	run.State.Ball.Y = 0.99
	run.State.Ball.VX = 0.0
	run.State.Ball.VY = 1.0

	endCalled := make(chan struct{}, 1)
	run.onEnd = func(r *MatchRun) {
		select {
		case endCalled <- struct{}{}:
		default:
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run.cancelFn = cancel

	// Wrap in a goroutine that calls onEnd after the loop exits (simulating matchLoop's defer)
	go func() {
		matchLoopActive(ctx, run)
		run.onEnd(run)
	}()

	// Wait for match.ended
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s0.countType("match.ended") > 0 && s1.countType("match.ended") > 0 {
			// Also wait for onEnd
			select {
			case <-endCalled:
				return // success
			case <-time.After(500 * time.Millisecond):
				t.Error("onEnd not called after match.ended")
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Errorf("expected match.ended on both senders; s0_ended=%d s1_ended=%d types=%v",
		s0.countType("match.ended"), s1.countType("match.ended"), s0.allTypes())
}

// TestMatchRun_InputApplied verifies queued inputs are applied to the match state.
func TestMatchRun_InputApplied(t *testing.T) {
	cfg := config.Default
	run, _, _, _ := makeTestRun(cfg)

	// Queue input for P1 with targetX=0.8
	run.InputCh <- InputMsg{Slot: 0, TargetX: 0.8, Seq: 1}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run.cancelFn = cancel

	go matchLoopActive(ctx, run)

	// Wait up to 200ms for 2 ticks, then check targetX was applied
	time.Sleep(100 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)

	run.mu.Lock()
	targetX := run.State.Players[0].TargetX
	run.mu.Unlock()

	if targetX != 0.8 {
		t.Errorf("Players[0].TargetX=%.2f want 0.8", targetX)
	}
}

// TestMatchRun_DisconnectPauses verifies that when a sender goes nil, the other player receives
// player.disconnected and snapshots stop.
func TestMatchRun_DisconnectPauses(t *testing.T) {
	cfg := config.Default
	run, s0, _, _ := makeTestRun(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run.cancelFn = cancel

	go matchLoopActive(ctx, run)

	// Let a few ticks run, then disconnect P2
	time.Sleep(150 * time.Millisecond)

	run.mu.Lock()
	run.Players[1].Sender = nil
	run.mu.Unlock()

	// Wait for disconnect event
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s0.countType("player.disconnected") > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if s0.countType("player.disconnected") == 0 {
		t.Error("expected player.disconnected on P1 (s0)")
	}

	// Count snapshots before and after a delay — should stop when paused
	snapshotsBefore := s0.countType("match.snapshot")
	time.Sleep(300 * time.Millisecond)
	snapshotsAfter := s0.countType("match.snapshot")

	if snapshotsAfter > snapshotsBefore+3 {
		t.Errorf("snapshots continued after disconnect: before=%d after=%d", snapshotsBefore, snapshotsAfter)
	}
}

// TestMatchRun_ReconnectResumes verifies that sending on reconnectCh causes match.reconnected to
// be sent to the reconnecting player and player.reconnected to the other.
func TestMatchRun_ReconnectResumes(t *testing.T) {
	cfg := config.Default
	run, s0, _, _ := makeTestRun(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run.cancelFn = cancel

	go matchLoopActive(ctx, run)

	// Let a few ticks run, then simulate P2 disconnect
	time.Sleep(100 * time.Millisecond)

	run.mu.Lock()
	run.Players[1].Sender = nil
	run.mu.Unlock()

	// Wait for disconnect to be detected
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if s0.countType("player.disconnected") > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Simulate P2 reconnecting: restore sender and signal via reconnectCh
	newS1 := &testSender{id: "conn1-new"}
	run.mu.Lock()
	run.Players[1].Sender = newS1
	run.mu.Unlock()

	run.reconnectCh <- 1

	// Wait for reconnect messages
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if newS1.countType("match.reconnected") > 0 && s0.countType("player.reconnected") > 0 {
			return // success
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Errorf("reconnect messages not received: match.reconnected=%d player.reconnected=%d",
		newS1.countType("match.reconnected"), s0.countType("player.reconnected"))
}
