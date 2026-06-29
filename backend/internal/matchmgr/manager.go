package matchmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/lobby"
	"github.com/pong-mobile/backend/internal/protocol"
)

// Manager tracks all running matches and routes messages to them.
type Manager struct {
	runs   sync.Map // matchID → *MatchRun
	byConn sync.Map // connID  → *MatchRun
	onEnd  func(*MatchRun)
}

// NewManager creates a Manager. onEnd is called when a match finishes; may be nil.
func NewManager(onEnd func(*MatchRun)) *Manager {
	if onEnd == nil {
		onEnd = func(*MatchRun) {}
	}
	return &Manager{onEnd: onEnd}
}

// StartMatch creates a MatchRun for the given lobby room and starts its goroutine.
func (m *Manager) StartMatch(room *lobby.Room) {
	matchID := fmt.Sprintf("match_%s", room.Code)

	players := [2]PlayerConn{
		{
			Sender:    room.Players[0].Conn,
			ConnID:    room.Players[0].Conn.ID(),
			SessionID: room.Players[0].Session.ID,
		},
		{
			Sender:    room.Players[1].Conn,
			ConnID:    room.Players[1].Conn.ID(),
			SessionID: room.Players[1].Session.ID,
		},
	}

	run := newMatchRun(matchID, room.Settings, players, func(r *MatchRun) {
		m.runs.Delete(r.MatchID)
		for i := 0; i < 2; i++ {
			r.mu.Lock()
			connID := r.Players[i].ConnID
			r.mu.Unlock()
			if connID != "" {
				m.byConn.Delete(connID)
			}
		}
		if m.onEnd != nil {
			m.onEnd(r)
		}
	})

	m.runs.Store(matchID, run)
	m.byConn.Store(players[0].ConnID, run)
	m.byConn.Store(players[1].ConnID, run)

	ctx, cancel := context.WithCancel(context.Background())
	run.cancelFn = cancel

	go matchLoop(ctx, run)
	log.Printf("matchmgr: started match %s", matchID)
}

// HandleInput routes an input.paddle_target message to the correct match.
func (m *Manager) HandleInput(connID string, env protocol.ClientEnvelope) {
	v, ok := m.byConn.Load(connID)
	if !ok {
		return
	}
	run := v.(*MatchRun)

	_, targetX, seq, ok := ParseInputPaddleTarget(env.Payload)
	if !ok {
		return
	}

	// Determine slot by connID.
	slot := -1
	run.mu.Lock()
	for i := 0; i < 2; i++ {
		if run.Players[i].ConnID == connID {
			slot = i
			break
		}
	}
	run.mu.Unlock()
	if slot < 0 {
		return
	}

	msg := InputMsg{Slot: slot, TargetX: targetX, Seq: seq}

	// Non-blocking send: drop oldest if channel is full.
	select {
	case run.InputCh <- msg:
	default:
		select {
		case <-run.InputCh:
		default:
		}
		select {
		case run.InputCh <- msg:
		default:
		}
	}
}

// HandleReconnect re-attaches a returning player to their in-progress match.
func (m *Manager) HandleReconnect(connID string, sess *auth.Session, sender lobby.Sender, rawPayload json.RawMessage) {
	_, matchID, ok := ParseReconnectPayload(rawPayload)
	if !ok {
		return
	}

	v, ok := m.runs.Load(matchID)
	if !ok {
		return
	}
	run := v.(*MatchRun)

	// Find slot by session ID.
	slot := -1
	run.mu.Lock()
	for i := 0; i < 2; i++ {
		if run.Players[i].SessionID == sess.ID {
			slot = i
			break
		}
	}
	if slot < 0 {
		run.mu.Unlock()
		return
	}

	// Remove old connID mapping.
	oldConnID := run.Players[slot].ConnID
	run.Players[slot].Sender = sender
	run.Players[slot].ConnID = connID
	run.Players[slot].ReconnectDeadline = time.Time{}
	run.mu.Unlock()

	if oldConnID != "" && oldConnID != connID {
		m.byConn.Delete(oldConnID)
	}
	m.byConn.Store(connID, run)

	run.reconnectCh <- slot
	log.Printf("matchmgr: player slot %d reconnected to match %s", slot, matchID)
}

// OnDisconnect nullifies a player's sender so the tick loop can detect it.
func (m *Manager) OnDisconnect(connID string) {
	v, ok := m.byConn.Load(connID)
	if !ok {
		return
	}
	run := v.(*MatchRun)

	run.mu.Lock()
	for i := 0; i < 2; i++ {
		if run.Players[i].ConnID == connID {
			run.Players[i].Sender = nil
			break
		}
	}
	run.mu.Unlock()
}
