package auth_test

import (
	"testing"

	"github.com/pong-mobile/backend/internal/auth"
)

func TestNewSession_GeneratesUniqueTokens(t *testing.T) {
	s1, err := auth.NewSession("Alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s2, err := auth.NewSession("Bob")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s1.Token == s2.Token {
		t.Error("tokens should be unique")
	}
	if s1.PlayerID == s2.PlayerID {
		t.Error("player IDs should be unique")
	}
}

func TestNewSession_Fields(t *testing.T) {
	s, err := auth.NewSession("Guest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Token == "" {
		t.Error("token must not be empty")
	}
	if s.PlayerID == "" {
		t.Error("playerID must not be empty")
	}
	if s.DisplayName != "Guest" {
		t.Errorf("displayName: got %q, want Guest", s.DisplayName)
	}
	if s.CreatedAt.IsZero() {
		t.Error("createdAt must be set")
	}
}

func TestStore_PutAndGetByToken(t *testing.T) {
	store := auth.NewStore()
	s, _ := auth.NewSession("Alice")
	store.Put(s)

	got, ok := store.GetByToken(s.Token)
	if !ok {
		t.Fatal("expected to find session by token")
	}
	if got.PlayerID != s.PlayerID {
		t.Errorf("playerID mismatch: got %q, want %q", got.PlayerID, s.PlayerID)
	}
}

func TestStore_GetByToken_Missing(t *testing.T) {
	store := auth.NewStore()
	_, ok := store.GetByToken("nonexistent-token")
	if ok {
		t.Error("should not find missing token")
	}
}

func TestStore_Delete(t *testing.T) {
	store := auth.NewStore()
	s, _ := auth.NewSession("Alice")
	store.Put(s)
	store.Delete(s.Token)
	_, ok := store.GetByToken(s.Token)
	if ok {
		t.Error("session should be gone after delete")
	}
}
