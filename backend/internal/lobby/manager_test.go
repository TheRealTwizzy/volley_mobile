package lobby_test

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/config"
	"github.com/pong-mobile/backend/internal/lobby"
)

// mockSender records SendBytes calls and implements lobby.Sender.
type mockSender struct {
	mu       sync.Mutex
	id       string
	messages [][]byte
}

func (m *mockSender) SendBytes(data []byte) {
	m.mu.Lock()
	m.messages = append(m.messages, data)
	m.mu.Unlock()
}
func (m *mockSender) ID() string { return m.id }
func (m *mockSender) received() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]map[string]any, len(m.messages))
	for i, b := range m.messages {
		_ = json.Unmarshal(b, &out[i])
	}
	return out
}
func newMock(id string) *mockSender { return &mockSender{id: id} }

func newSess(playerID string) *auth.Session {
	return &auth.Session{
		ID:          "sess_" + playerID,
		PlayerID:    playerID,
		Token:       "tok_" + playerID,
		DisplayName: playerID,
	}
}

func TestCreateRoom_Success(t *testing.T) {
	mgr := lobby.NewManager(func(r *lobby.Room) {})
	conn := newMock("conn1")
	room, err := mgr.CreateRoom(newSess("p1"), conn, config.Default)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if len(room.Code) != 6 {
		t.Errorf("room code should be 6 chars, got %q", room.Code)
	}
	if room.Players[0] == nil {
		t.Error("slot 0 should be filled")
	}
	if room.Players[1] != nil {
		t.Error("slot 1 should be empty")
	}
}

func TestJoinRoom_Success(t *testing.T) {
	mgr := lobby.NewManager(func(r *lobby.Room) {})
	c1 := newMock("conn1")
	room, _ := mgr.CreateRoom(newSess("p1"), c1, config.Default)
	c2 := newMock("conn2")
	joined, err := mgr.JoinRoom(room.Code, newSess("p2"), c2)
	if err != nil {
		t.Fatalf("JoinRoom: %v", err)
	}
	if joined.Players[1] == nil {
		t.Error("slot 1 should be filled after join")
	}
}

func TestJoinRoom_RoomNotFound(t *testing.T) {
	mgr := lobby.NewManager(func(r *lobby.Room) {})
	c := newMock("conn1")
	_, err := mgr.JoinRoom("000000", newSess("p1"), c)
	if err != lobby.ErrRoomNotFound {
		t.Errorf("expected ErrRoomNotFound, got %v", err)
	}
}

func TestJoinRoom_RoomFull(t *testing.T) {
	mgr := lobby.NewManager(func(r *lobby.Room) {})
	c1, c2, c3 := newMock("conn1"), newMock("conn2"), newMock("conn3")
	room, _ := mgr.CreateRoom(newSess("p1"), c1, config.Default)
	mgr.JoinRoom(room.Code, newSess("p2"), c2)
	_, err := mgr.JoinRoom(room.Code, newSess("p3"), c3)
	if err != lobby.ErrRoomFull {
		t.Errorf("expected ErrRoomFull, got %v", err)
	}
}

func TestJoinRoom_AlreadyInRoom(t *testing.T) {
	mgr := lobby.NewManager(func(r *lobby.Room) {})
	c1 := newMock("conn1")
	room, _ := mgr.CreateRoom(newSess("p1"), c1, config.Default)
	// c1 tries to join another room (same code in this test)
	_, err := mgr.JoinRoom(room.Code, newSess("p1b"), c1)
	if err != lobby.ErrAlreadyInRoom {
		t.Errorf("expected ErrAlreadyInRoom, got %v", err)
	}
}

func TestSetReady_BothReady_CallsOnStart(t *testing.T) {
	started := false
	mgr := lobby.NewManager(func(r *lobby.Room) { started = true })
	c1, c2 := newMock("conn1"), newMock("conn2")
	room, _ := mgr.CreateRoom(newSess("p1"), c1, config.Default)
	mgr.JoinRoom(room.Code, newSess("p2"), c2)

	mgr.SetReady("conn1", true)
	if started {
		t.Error("onStart should not fire with only one player ready")
	}
	mgr.SetReady("conn2", true)
	if !started {
		t.Error("onStart should fire when both players are ready")
	}
}

func TestSetReady_OneReady_NoStart(t *testing.T) {
	started := false
	mgr := lobby.NewManager(func(r *lobby.Room) { started = true })
	c1, c2 := newMock("conn1"), newMock("conn2")
	room, _ := mgr.CreateRoom(newSess("p1"), c1, config.Default)
	mgr.JoinRoom(room.Code, newSess("p2"), c2)
	mgr.SetReady("conn1", true)
	if started {
		t.Error("onStart must not fire with only one player ready")
	}
}

func TestLeaveRoom_HostLeaves_ClosesRoom(t *testing.T) {
	mgr := lobby.NewManager(func(r *lobby.Room) {})
	c1, c2 := newMock("conn1"), newMock("conn2")
	room, _ := mgr.CreateRoom(newSess("p1"), c1, config.Default)
	mgr.JoinRoom(room.Code, newSess("p2"), c2)

	mgr.LeaveRoom("conn1")

	// Guest should have received an error message
	msgs := c2.received()
	if len(msgs) == 0 {
		t.Fatal("guest should receive a message when host leaves")
	}
	last := msgs[len(msgs)-1]
	if last["type"] != "error" {
		t.Errorf("guest should receive error, got %v", last["type"])
	}
}

func TestLeaveRoom_GuestLeaves_RoomOpen(t *testing.T) {
	mgr := lobby.NewManager(func(r *lobby.Room) {})
	c1, c2 := newMock("conn1"), newMock("conn2")
	room, _ := mgr.CreateRoom(newSess("p1"), c1, config.Default)
	mgr.JoinRoom(room.Code, newSess("p2"), c2)

	// Count messages before guest leaves (join triggers room.updated)
	priorCount := len(c1.received())

	mgr.LeaveRoom("conn2")

	// Host should receive room.updated showing only 1 player
	msgs := c1.received()
	if len(msgs) <= priorCount {
		t.Fatal("host should receive room.updated when guest leaves")
	}
	last := msgs[len(msgs)-1]
	if last["type"] != "room.updated" {
		t.Errorf("host should receive room.updated, got %v", last["type"])
	}
}
