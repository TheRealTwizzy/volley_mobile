package lobby

import (
	"github.com/pong-mobile/backend/internal/protocol"
)

type roomUpdatedPayload struct {
	RoomID   string        `json:"roomId"`
	RoomCode string        `json:"roomCode"`
	Status   string        `json:"status"`
	Players  []playerEntry `json:"players"`
	Settings settingsEntry `json:"settings"`
}

type playerEntry struct {
	PlayerID    string `json:"playerId"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	Ready       bool   `json:"ready"`
	Connected   bool   `json:"connected"`
}

type settingsEntry struct {
	PointsToWin int     `json:"pointsToWin"`
	PaddleSpeed float64 `json:"paddleSpeed"`
	BallSpeed   float64 `json:"ballSpeed"`
}

type roomCreatedPayload struct {
	RoomID       string        `json:"roomId"`
	RoomCode     string        `json:"roomCode"`
	HostPlayerID string        `json:"hostPlayerId"`
	Settings     settingsEntry `json:"settings"`
}

func buildRoomUpdated(room *Room) ([]byte, error) {
	var players []playerEntry
	for i, slot := range room.Players {
		if slot == nil {
			continue
		}
		role := "host"
		if i == 1 {
			role = "guest"
		}
		players = append(players, playerEntry{
			PlayerID:    slot.Session.PlayerID,
			DisplayName: slot.Session.DisplayName,
			Role:        role,
			Ready:       slot.Ready,
			Connected:   slot.Connected,
		})
	}
	return protocol.MarshalServer(protocol.ServerEnvelope{
		Type: protocol.TypeRoomUpdated,
		Payload: roomUpdatedPayload{
			RoomID:   room.ID,
			RoomCode: room.Code,
			Status:   string(room.Status),
			Players:  players,
			Settings: settingsEntry{
				PointsToWin: room.Settings.PointsToWin,
				PaddleSpeed: room.Settings.PaddleSpeed,
				BallSpeed:   room.Settings.BallSpeed,
			},
		},
	})
}

// BroadcastRoomUpdated sends room.updated to all connected players.
func BroadcastRoomUpdated(room *Room) {
	data, err := buildRoomUpdated(room)
	if err != nil {
		return
	}
	for _, slot := range room.Players {
		if slot != nil && slot.Conn != nil && slot.Connected {
			slot.Conn.SendBytes(data)
		}
	}
}

// SendRoomCreated sends the room.created response to the creator.
func SendRoomCreated(room *Room) {
	data, err := protocol.MarshalServer(protocol.ServerEnvelope{
		Type: protocol.TypeRoomCreated,
		Payload: roomCreatedPayload{
			RoomID:       room.ID,
			RoomCode:     room.Code,
			HostPlayerID: room.Players[0].Session.PlayerID,
			Settings: settingsEntry{
				PointsToWin: room.Settings.PointsToWin,
				PaddleSpeed: room.Settings.PaddleSpeed,
				BallSpeed:   room.Settings.BallSpeed,
			},
		},
	})
	if err != nil {
		return
	}
	room.Players[0].Conn.SendBytes(data)
}

// SendRoomError sends an error message to a specific connection.
func SendRoomError(conn Sender, code, message, requestID string) {
	data, err := protocol.MakeError(code, message, requestID, false, 0)
	if err != nil {
		return
	}
	conn.SendBytes(data)
}
