package lobby

import (
	"errors"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/config"
)

// Sender is implemented by wsconn.conn via duck typing (no import needed).
type Sender interface {
	SendBytes(data []byte)
	ID() string
}

type RoomStatus string

const (
	RoomStatusWaiting  RoomStatus = "waiting"
	RoomStatusStarting RoomStatus = "starting"
)

var (
	ErrRoomNotFound  = errors.New("room_not_found")
	ErrRoomFull      = errors.New("room_full")
	ErrAlreadyInRoom = errors.New("already_in_room")
	ErrNotInRoom     = errors.New("not_in_room")
)

// Slot holds one player's connection and state within a room.
type Slot struct {
	Session   *auth.Session
	Conn      Sender
	Ready     bool
	Connected bool
}

// Room is the authoritative room state.
type Room struct {
	ID       string
	Code     string
	Players  [2]*Slot
	Settings config.Settings
	Status   RoomStatus
}
