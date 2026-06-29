package lobby

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/config"
)

// Manager owns all room state. All exported methods are safe for concurrent use.
type Manager struct {
	mu      sync.Mutex
	rooms   map[string]*Room // code → *Room
	byConn  map[string]*Room // connID → *Room
	onStart func(*Room)
}

// NewManager returns a Manager. onStart is called (in a goroutine) when both
// players in a room set ready=true.
func NewManager(onStart func(*Room)) *Manager {
	return &Manager{
		rooms:   make(map[string]*Room),
		byConn:  make(map[string]*Room),
		onStart: onStart,
	}
}

func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func generateCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// CreateRoom creates a new room with the host in slot 0.
// Returns ErrAlreadyInRoom if conn is already in a room.
func (m *Manager) CreateRoom(sess *auth.Session, conn Sender, settings config.Settings) (*Room, error) {
	// Clamp user-supplied settings onto defaults.
	if settings.PointsToWin <= 0 {
		settings.PointsToWin = config.Default.PointsToWin
	} else {
		settings.PointsToWin = clampInt(settings.PointsToWin, 1, 21)
	}
	if settings.PaddleSpeed <= 0 {
		settings.PaddleSpeed = config.Default.PaddleSpeed
	} else {
		settings.PaddleSpeed = clampFloat(settings.PaddleSpeed, 0.1, 2.0)
	}
	if settings.BallSpeed <= 0 {
		settings.BallSpeed = config.Default.BallSpeed
	} else {
		settings.BallSpeed = clampFloat(settings.BallSpeed, 0.1, 2.0)
	}
	// Carry over all physics/timing constants from Default.
	full := config.Default
	full.PointsToWin = settings.PointsToWin
	full.PaddleSpeed = settings.PaddleSpeed
	full.BallSpeed = settings.BallSpeed

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.byConn[conn.ID()]; exists {
		return nil, ErrAlreadyInRoom
	}

	var code string
	for {
		var err error
		code, err = generateCode()
		if err != nil {
			return nil, err
		}
		if _, taken := m.rooms[code]; !taken {
			break
		}
	}

	room := &Room{
		ID:       generateID(),
		Code:     code,
		Settings: full,
		Status:   RoomStatusWaiting,
		Players: [2]*Slot{
			{Session: sess, Conn: conn, Connected: true},
			nil,
		},
	}
	m.rooms[code] = room
	m.byConn[conn.ID()] = room

	SendRoomCreated(room)
	return room, nil
}

// JoinRoom adds a second player to the room with the given code.
// Returns ErrRoomNotFound, ErrRoomFull, or ErrAlreadyInRoom on failure.
func (m *Manager) JoinRoom(code string, sess *auth.Session, conn Sender) (*Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	room, ok := m.rooms[code]
	if !ok {
		return nil, ErrRoomNotFound
	}
	if room.Players[1] != nil {
		return nil, ErrRoomFull
	}
	if _, exists := m.byConn[conn.ID()]; exists {
		return nil, ErrAlreadyInRoom
	}

	room.Players[1] = &Slot{Session: sess, Conn: conn, Connected: true}
	m.byConn[conn.ID()] = room

	BroadcastRoomUpdated(room)
	return room, nil
}

// SetReady toggles a player's ready flag and fires onStart when both are ready.
// Returns ErrNotInRoom if connID is not in any room.
func (m *Manager) SetReady(connID string, ready bool) error {
	m.mu.Lock()

	room, ok := m.byConn[connID]
	if !ok {
		m.mu.Unlock()
		return ErrNotInRoom
	}

	for _, slot := range room.Players {
		if slot != nil && slot.Conn.ID() == connID {
			slot.Ready = ready
			break
		}
	}

	BroadcastRoomUpdated(room)

	shouldStart := room.Status == RoomStatusWaiting &&
		room.Players[0] != nil && room.Players[0].Ready &&
		room.Players[1] != nil && room.Players[1].Ready
	if shouldStart {
		room.Status = RoomStatusStarting
	}

	m.mu.Unlock() // release lock before calling onStart to avoid deadlock

	if shouldStart {
		m.onStart(room)
	}

	return nil
}

// LeaveRoom removes the player with connID from their room.
// If the host leaves, the room is closed and the guest is notified.
// If the guest leaves, the host receives room.updated.
func (m *Manager) LeaveRoom(connID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.leaveRoomLocked(connID)
}

// OnDisconnect is called when a WebSocket connection closes.
// In M3 this is identical to LeaveRoom; match-phase handling is added in M4.
func (m *Manager) OnDisconnect(connID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.leaveRoomLocked(connID)
}

func (m *Manager) leaveRoomLocked(connID string) {
	room, ok := m.byConn[connID]
	if !ok {
		return
	}
	delete(m.byConn, connID)

	if room.Players[0] != nil && room.Players[0].Conn.ID() == connID {
		// Host left — close room, notify guest.
		if room.Players[1] != nil {
			SendRoomError(room.Players[1].Conn, "room_closed", "Room was closed by the host.", "")
			delete(m.byConn, room.Players[1].Conn.ID())
		}
		delete(m.rooms, room.Code)
	} else if room.Players[1] != nil && room.Players[1].Conn.ID() == connID {
		// Guest left — clear slot, notify host.
		room.Players[1] = nil
		BroadcastRoomUpdated(room)
	}
}
