package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"time"

	"github.com/pong-mobile/backend/internal/auth"
	_ "github.com/lib/pq"
)

// SlotResult holds the outcome for one player slot in a completed match.
type SlotResult struct {
	PlayerID string // auth.Session.PlayerID, e.g. "player_abc123"
	Score    int
	Result   string // "win" | "loss" | "forfeit"
}

// DB wraps *sql.DB with Volley-specific persistence methods.
type DB struct {
	db *sql.DB
}

// NewDB opens a PostgreSQL connection and pings it.
// Returns (nil, nil) if databaseURL is empty — callers must nil-check DB before use.
func NewDB(databaseURL string) (*DB, error) {
	if databaseURL == "" {
		return nil, nil
	}
	sqlDB, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("storage.NewDB open: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("storage.NewDB ping: %w", err)
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	return &DB{db: sqlDB}, nil
}

// newID generates a random hex string suitable as a unique row ID.
func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

// RawDB returns the underlying *sql.DB for migration use.
func (d *DB) RawDB() *sql.DB {
	if d == nil {
		return nil
	}
	return d.db
}

// SaveUser upserts a user row. Idempotent — safe to call multiple times.
func (d *DB) SaveUser(ctx context.Context, sess *auth.Session) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO users (id, display_name, is_guest, created_at)
		VALUES ($1, $2, true, $3)
		ON CONFLICT (id) DO UPDATE SET display_name = EXCLUDED.display_name`,
		sess.PlayerID, sess.DisplayName, sess.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("storage.SaveUser: %w", err)
	}
	return nil
}

// SaveMatch inserts a match record. Idempotent via ON CONFLICT DO NOTHING.
func (d *DB) SaveMatch(ctx context.Context, matchID, roomCode, p1PlayerID, p2PlayerID string,
	pointsToWin int, winnerPlayerID string, now time.Time) error {

	var winner interface{} = nil
	if winnerPlayerID != "" {
		winner = winnerPlayerID
	}
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO matches
			(id, room_code, player_one_id, player_two_id, status,
			 points_to_win, winner_id, created_at, started_at, ended_at)
		VALUES ($1, $2, $3, $4, 'ended', $5, $6, $7, $7, $7)
		ON CONFLICT (id) DO NOTHING`,
		matchID, roomCode, p1PlayerID, p2PlayerID,
		pointsToWin, winner, now)
	if err != nil {
		return fmt.Errorf("storage.SaveMatch: %w", err)
	}
	return nil
}

// SaveMatchResult is the top-level persistence call for a completed match.
// It writes users -> match -> match_results in dependency order.
// sessions[i] may be nil if that slot forfeited before having a valid session.
func (d *DB) SaveMatchResult(ctx context.Context, matchID string, results [2]SlotResult,
	sessions [2]*auth.Session, pointsToWin int) error {

	roomCode := matchID
	if len(matchID) > 6 && matchID[:6] == "match_" {
		roomCode = matchID[6:]
	}
	now := time.Now().UTC()

	// 1. Upsert users (must exist before match FK references them)
	for i, sess := range sessions {
		if sess != nil {
			if err := d.SaveUser(ctx, sess); err != nil {
				return fmt.Errorf("SaveMatchResult user[%d]: %w", i, err)
			}
		}
	}

	// 2. Determine winner player ID
	winnerPlayerID := ""
	for i, r := range results {
		if r.Result == "win" && sessions[i] != nil {
			winnerPlayerID = r.PlayerID
			break
		}
	}

	// 3. Insert match row
	p1ID, p2ID := "", ""
	if sessions[0] != nil {
		p1ID = results[0].PlayerID
	}
	if sessions[1] != nil {
		p2ID = results[1].PlayerID
	}
	if err := d.SaveMatch(ctx, matchID, roomCode, p1ID, p2ID,
		pointsToWin, winnerPlayerID, now); err != nil {
		return fmt.Errorf("SaveMatchResult match: %w", err)
	}

	// 4. Insert per-slot result rows
	for i, r := range results {
		id, err := newID()
		if err != nil {
			return fmt.Errorf("SaveMatchResult newID slot[%d]: %w", i, err)
		}
		_, err = d.db.ExecContext(ctx, `
			INSERT INTO match_results (id, match_id, player_id, score, result)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO NOTHING`,
			id, matchID, r.PlayerID, r.Score, r.Result)
		if err != nil {
			return fmt.Errorf("SaveMatchResult result[%d]: %w", i, err)
		}
	}
	return nil
}
