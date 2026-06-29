package storage_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/storage"
)

func TestNewDB_EmptyURL(t *testing.T) {
	db, err := storage.NewDB("")
	if err != nil {
		t.Fatalf("expected nil error for empty URL, got: %v", err)
	}
	if db != nil {
		t.Fatal("expected nil DB for empty URL")
	}
}

func TestSaveMatchResult_SkipIfNoDB(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set — skipping storage integration test")
	}

	db, err := storage.NewDB(url)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	if db == nil {
		t.Fatal("NewDB returned nil with non-empty URL")
	}

	sess0 := &auth.Session{
		ID:          "sess0",
		PlayerID:    "player_test0000",
		Token:       "tok0",
		DisplayName: "Alice",
		CreatedAt:   time.Now().UTC(),
	}
	sess1 := &auth.Session{
		ID:          "sess1",
		PlayerID:    "player_test0001",
		Token:       "tok1",
		DisplayName: "Bob",
		CreatedAt:   time.Now().UTC(),
	}

	results := [2]storage.SlotResult{
		{PlayerID: sess0.PlayerID, Score: 5, Result: "win"},
		{PlayerID: sess1.PlayerID, Score: 3, Result: "loss"},
	}
	sessions := [2]*auth.Session{sess0, sess1}

	ctx := context.Background()
	if err := db.SaveMatchResult(ctx, "match_TESTROOM", results, sessions, 5); err != nil {
		t.Fatalf("SaveMatchResult: %v", err)
	}
}
