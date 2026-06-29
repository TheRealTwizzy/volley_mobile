# M6: Persistence + Fly.io Deployment Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist match outcomes to PostgreSQL and deploy the backend to Fly.io so real mobile devices can connect.

**Architecture:** The Go server gains a `storage` package wrapping `database/sql` + `lib/pq`. On match end, `matchmgr.Manager.onEnd` fires a goroutine that writes users, the match record, and per-slot results in dependency order. Dev mode (no `DATABASE_URL`) skips all DB calls via a nil `*storage.DB` guard. The server image is built with a multi-stage Dockerfile and shipped to Fly.io where `DATABASE_URL` is injected as a secret.

**Tech Stack:** Go 1.22, `github.com/lib/pq` (PostgreSQL driver), Fly.io, Docker

## Global Constraints

- Go 1.22, module `github.com/pong-mobile/backend`
- PostgreSQL driver: `github.com/lib/pq` (pure Go, no CGO)
- `DATABASE_URL` env var for connection string
- Dev mode: if `VOLLEY_ENV != "production"` and `DATABASE_URL` is empty → skip DB (nil storage, no-op)
- Migrations: `backend/migrations/*.sql` executed at startup, idempotent (`IF NOT EXISTS`)
- Fly.io app name: `volley-server`
- All existing tests (18/18) must still pass after changes

---

## Pre-Task: Dependency Audit

Before writing any code, note what is and is not already in `go.mod`:

- `github.com/gorilla/websocket v1.5.3` — present
- `github.com/lib/pq` — **not present**, must be added
- `github.com/google/uuid` — **not present**; do NOT add it. Use `crypto/rand` for UUID v4 generation (see Task 1 helper below).

`auth.Store` only exposes `GetByToken(token string) (Session, bool)` — there is no `GetByID`. To avoid threading the auth store into the onEnd callback, `PlayerConn` must be extended with a `PlayerID` field populated at match start (Task 2a). The `SessionID` field stays for reconnect purposes; `PlayerID` is the persistent player identifier used for DB writes.

---

## Task 1: `storage` package — db.go + migrations.go + schema

**Files to create:**
- `backend/internal/storage/db.go`
- `backend/internal/storage/migrations.go`
- `backend/migrations/001_initial.sql`
- `backend/internal/storage/db_test.go`

### 1a. Add `github.com/lib/pq` to go.mod

Run from `backend/`:

```sh
C:\Users\trent\sdk\go\bin\go.exe get github.com/lib/pq@latest
```

This updates `go.mod` and `go.sum`. Verify the line `github.com/lib/pq vX.Y.Z` appears in `go.mod`.

### 1b. Create `backend/migrations/001_initial.sql`

```sql
CREATE TABLE IF NOT EXISTS users (
  id           UUID PRIMARY KEY,
  display_name TEXT        NOT NULL,
  is_guest     BOOLEAN     NOT NULL DEFAULT true,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS matches (
  id            UUID PRIMARY KEY,
  room_code     TEXT,
  player_one_id UUID REFERENCES users(id),
  player_two_id UUID REFERENCES users(id),
  status        TEXT        NOT NULL,
  points_to_win INTEGER     NOT NULL,
  winner_id     UUID REFERENCES users(id),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at    TIMESTAMPTZ,
  ended_at      TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS match_results (
  id        UUID    PRIMARY KEY,
  match_id  UUID    NOT NULL REFERENCES matches(id),
  player_id UUID    NOT NULL REFERENCES users(id),
  score     INTEGER NOT NULL,
  result    TEXT    NOT NULL
);
```

All three `CREATE TABLE IF NOT EXISTS` — fully idempotent. Re-running at startup is safe.

### 1c. Create `backend/internal/storage/db.go`

```go
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

// SlotResult holds the outcome for one player in a completed match.
type SlotResult struct {
	PlayerID string
	Score    int
	Result   string // "win" | "loss" | "forfeit"
}

// DB wraps a *sql.DB with Volley-specific query methods.
type DB struct {
	db *sql.DB
}

// NewDB opens a PostgreSQL connection using databaseURL.
// If databaseURL is empty, it returns (nil, nil) — callers must nil-check before use.
// Returns (nil, error) only on a genuine connection/ping failure.
func NewDB(databaseURL string) (*DB, error) {
	if databaseURL == "" {
		return nil, nil
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("storage.NewDB: open: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("storage.NewDB: ping: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	return &DB{db: db}, nil
}

// newUUID generates a random UUID v4 string without external dependencies.
func newUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// Set version 4 and variant bits
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// SaveUser upserts a user row from an auth.Session.
// Uses INSERT ... ON CONFLICT DO NOTHING so re-inserting is safe.
func (d *DB) SaveUser(ctx context.Context, sess *auth.Session) error {
	// PlayerID is "player_<hex>"; use it as the stable UUID-substitute.
	// We derive a deterministic UUID from the PlayerID string so re-runs are idempotent.
	// For simplicity: generate a new UUID only on first insert; ON CONFLICT DO NOTHING.
	id, err := newUUID()
	if err != nil {
		return fmt.Errorf("storage.SaveUser: uuid: %w", err)
	}
	_, err = d.db.ExecContext(ctx, `
		INSERT INTO users (id, display_name, is_guest, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO NOTHING`,
		id, sess.DisplayName, true, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("storage.SaveUser: exec: %w", err)
	}
	return nil
}

// SaveMatch inserts a match row. Idempotent via ON CONFLICT DO NOTHING.
func (d *DB) SaveMatch(ctx context.Context, matchID string, roomCode string,
	p1ID, p2ID string, pointsToWin int, winnerID string, endedAt time.Time) error {

	id, err := newUUID()
	if err != nil {
		return fmt.Errorf("storage.SaveMatch: uuid: %w", err)
	}
	now := time.Now().UTC()
	var winnerVal interface{}
	if winnerID != "" {
		winnerVal = winnerID
	}
	_, err = d.db.ExecContext(ctx, `
		INSERT INTO matches
			(id, room_code, player_one_id, player_two_id, status, points_to_win,
			 winner_id, created_at, started_at, ended_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8, $9)
		ON CONFLICT (id) DO NOTHING`,
		id, roomCode, p1ID, p2ID, "ended", pointsToWin, winnerVal, now, endedAt)
	if err != nil {
		return fmt.Errorf("storage.SaveMatch: exec: %w", err)
	}
	return nil
}

// SaveMatchResult persists the outcome of a completed match.
// It calls SaveUser for both players then SaveMatch before writing match_results rows.
// matchID is the MatchRun.MatchID string (e.g. "match_ABCD").
// roomCode is extracted from matchID by stripping the "match_" prefix.
// sessions[2] must contain the auth.Session for each slot (may be zero-value if forfeited).
func (d *DB) SaveMatchResult(ctx context.Context, matchID string, results [2]SlotResult,
	sessions [2]*auth.Session, pointsToWin int) error {

	// Derive roomCode from matchID
	roomCode := matchID
	if len(matchID) > 6 && matchID[:6] == "match_" {
		roomCode = matchID[6:]
	}

	// Step 1: upsert users
	for i := 0; i < 2; i++ {
		if sessions[i] != nil {
			if err := d.SaveUser(ctx, sessions[i]); err != nil {
				return fmt.Errorf("storage.SaveMatchResult: save user slot %d: %w", i, err)
			}
		}
	}

	// Step 2: determine winner player ID
	winnerPlayerID := ""
	for i := 0; i < 2; i++ {
		if results[i].Result == "win" && sessions[i] != nil {
			winnerPlayerID = results[i].PlayerID
		}
	}

	// Step 3: insert match record
	p1ID := ""
	p2ID := ""
	if sessions[0] != nil {
		p1ID = results[0].PlayerID
	}
	if sessions[1] != nil {
		p2ID = results[1].PlayerID
	}
	if err := d.SaveMatch(ctx, matchID, roomCode, p1ID, p2ID,
		pointsToWin, winnerPlayerID, time.Now().UTC()); err != nil {
		return fmt.Errorf("storage.SaveMatchResult: save match: %w", err)
	}

	// Step 4: insert per-slot result rows
	for i := 0; i < 2; i++ {
		id, err := newUUID()
		if err != nil {
			return fmt.Errorf("storage.SaveMatchResult: uuid slot %d: %w", i, err)
		}
		// Use a placeholder match UUID — we need the real match row UUID.
		// NOTE: See design note below about the UUID relationship.
		_, err = d.db.ExecContext(ctx, `
			INSERT INTO match_results (id, match_id, player_id, score, result)
			SELECT $1,
			       (SELECT id FROM matches WHERE room_code = $2 LIMIT 1),
			       $3, $4, $5`,
			id, roomCode, results[i].PlayerID, results[i].Score, results[i].Result)
		if err != nil {
			return fmt.Errorf("storage.SaveMatchResult: insert result slot %d: %w", i, err)
		}
	}
	return nil
}
```

> **Design note on UUIDs:** The current `auth.Session.PlayerID` is `"player_<hex>"` — a string identifier, not a UUID. The DB schema uses `UUID PRIMARY KEY` for `users.id`. Two options exist:
>
> **Option A (recommended):** Change `users.id` to `TEXT PRIMARY KEY` and store `PlayerID` directly. Simpler, no UUID conversion needed, no collision risk.
>
> **Option B:** Generate a UUID for each user and cache it (requires a `playerUUIDs sync.Map` in `storage.DB`).
>
> **Use Option A.** Update `001_initial.sql` to use `TEXT PRIMARY KEY` for all `id` columns and foreign keys. This avoids UUID generation entirely for users and removes the ON CONFLICT ambiguity.

### 1c (revised) — Use TEXT ids throughout

Update `backend/migrations/001_initial.sql` to:

```sql
CREATE TABLE IF NOT EXISTS users (
  id           TEXT        PRIMARY KEY,
  display_name TEXT        NOT NULL,
  is_guest     BOOLEAN     NOT NULL DEFAULT true,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS matches (
  id            TEXT        PRIMARY KEY,
  room_code     TEXT,
  player_one_id TEXT REFERENCES users(id),
  player_two_id TEXT REFERENCES users(id),
  status        TEXT        NOT NULL,
  points_to_win INTEGER     NOT NULL,
  winner_id     TEXT REFERENCES users(id),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at    TIMESTAMPTZ,
  ended_at      TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS match_results (
  id        TEXT    PRIMARY KEY,
  match_id  TEXT    NOT NULL REFERENCES matches(id),
  player_id TEXT    NOT NULL REFERENCES users(id),
  score     INTEGER NOT NULL,
  result    TEXT    NOT NULL
);
```

With TEXT ids, `users.id = sess.PlayerID` (e.g. `"player_abc123"`), `matches.id = matchID` (e.g. `"match_ABCD"`), and `match_results.id` can be a UUID string generated by `newUUID()` for uniqueness.

### 1c (final) — Simplified `db.go` using TEXT ids

```go
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
// It writes users → match → match_results in dependency order.
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
```

### 1d. Create `backend/internal/storage/migrations.go`

```go
package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// RunMigrations reads all *.sql files from migrationsDir in lexicographic order
// and executes each one against db. Uses IF NOT EXISTS in SQL — safe to re-run.
func RunMigrations(db *sql.DB, migrationsDir string) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("migrations: readdir %q: %w", migrationsDir, err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".sql" {
			files = append(files, filepath.Join(migrationsDir, e.Name()))
		}
	}
	sort.Strings(files)

	for _, f := range files {
		sql, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("migrations: read %q: %w", f, err)
		}
		if _, err := db.Exec(string(sql)); err != nil {
			return fmt.Errorf("migrations: exec %q: %w", f, err)
		}
	}
	return nil
}
```

### 1e. Create `backend/internal/storage/db_test.go`

```go
package storage_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/storage"
)

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

func TestNewDB_EmptyURL(t *testing.T) {
	db, err := storage.NewDB("")
	if err != nil {
		t.Fatalf("expected nil error for empty URL, got: %v", err)
	}
	if db != nil {
		t.Fatal("expected nil DB for empty URL")
	}
}
```

### Validation for Task 1

```sh
# From backend/ — must pass with 18+ tests (storage tests skip without DATABASE_URL)
C:\Users\trent\sdk\go\bin\go.exe test ./...
```

Expected: all existing 18 tests pass; storage tests show `--- SKIP`.

---

## Task 2: Extend `PlayerConn` + Wire `main.go`

### 2a. Add `PlayerID` to `PlayerConn` in `match_run.go`

**File:** `backend/internal/matchmgr/match_run.go`

Add `PlayerID string` to `PlayerConn`:

```go
// PlayerConn holds one player's connection state in a running match.
type PlayerConn struct {
	Sender            lobby.Sender
	ConnID            string
	SessionID         string        // auth.Session.ID for reconnect matching
	PlayerID          string        // auth.Session.PlayerID for persistence (e.g. "player_abc123")
	ReconnectDeadline time.Time
}
```

### 2b. Populate `PlayerID` in `manager.go`

**File:** `backend/internal/matchmgr/manager.go` — in `StartMatch`, update the players array:

```go
players := [2]PlayerConn{
	{
		Sender:    room.Players[0].Conn,
		ConnID:    room.Players[0].Conn.ID(),
		SessionID: room.Players[0].Session.ID,
		PlayerID:  room.Players[0].Session.PlayerID,
	},
	{
		Sender:    room.Players[1].Conn,
		ConnID:    room.Players[1].Conn.ID(),
		SessionID: room.Players[1].Session.ID,
		PlayerID:  room.Players[1].Session.PlayerID,
	},
}
```

### 2c. Update `main.go` — storage init, migrations, onEnd callback

**File:** `backend/cmd/server/main.go` — full replacement:

```go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/lobby"
	"github.com/pong-mobile/backend/internal/matchmgr"
	"github.com/pong-mobile/backend/internal/storage"
	"github.com/pong-mobile/backend/internal/wsconn"
)

func main() {
	// --- Storage init ---
	databaseURL := os.Getenv("DATABASE_URL")
	volleyEnv := os.Getenv("VOLLEY_ENV")

	var db *storage.DB
	if databaseURL == "" && volleyEnv == "production" {
		log.Fatal("VOLLEY_ENV=production but DATABASE_URL is not set")
	}

	if databaseURL != "" {
		var err error
		db, err = storage.NewDB(databaseURL)
		if err != nil {
			log.Fatalf("storage.NewDB: %v", err)
		}
		// Run migrations relative to binary location.
		// In Docker the binary and migrations/ are both in /app/.
		migrationsDir := migrationsPath()
		if sqlDB := db.RawDB(); sqlDB != nil {
			if err := storage.RunMigrations(sqlDB, migrationsDir); err != nil {
				log.Fatalf("migrations: %v", err)
			}
			log.Printf("migrations applied from %s", migrationsDir)
		}
	} else {
		log.Println("DATABASE_URL not set — running without persistence (dev mode)")
	}

	// --- Match manager with onEnd wired to storage ---
	sessions := auth.NewStore()

	matchMgr := matchmgr.NewManager(func(run *matchmgr.MatchRun) {
		if db == nil {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Snapshot state under lock
			run.Mu().Lock()
			state := run.State
			players := run.Players
			run.Mu().Unlock()

			// Determine winner: highest score wins; forfeit if Sender was nil
			scores := [2]int{
				state.Players[0].Score,
				state.Players[1].Score,
			}

			results := [2]storage.SlotResult{}
			for i := 0; i < 2; i++ {
				results[i].PlayerID = players[i].PlayerID
				results[i].Score = scores[i]
			}

			switch {
			case players[0].Sender == nil && players[1].Sender != nil:
				// slot 0 forfeited
				results[0].Result = "forfeit"
				results[1].Result = "win"
			case players[1].Sender == nil && players[0].Sender != nil:
				// slot 1 forfeited
				results[0].Result = "win"
				results[1].Result = "forfeit"
			case scores[0] > scores[1]:
				results[0].Result = "win"
				results[1].Result = "loss"
			case scores[1] > scores[0]:
				results[0].Result = "loss"
				results[1].Result = "win"
			default:
				// Tie (should not happen with PointsToWin logic, but be safe)
				results[0].Result = "loss"
				results[1].Result = "loss"
			}

			// Build sessions array from auth store by PlayerID
			// (sessions map stored on MatchRun via SessionID lookup)
			sessPair := [2]*auth.Session{}
			for i := 0; i < 2; i++ {
				sess, ok := sessions.GetBySessionID(players[i].SessionID)
				if ok {
					s := sess // copy
					sessPair[i] = &s
				}
			}

			pointsToWin := state.Settings.PointsToWin
			if err := db.SaveMatchResult(ctx, run.MatchID, results, sessPair, pointsToWin); err != nil {
				log.Printf("storage: SaveMatchResult %s: %v", run.MatchID, err)
			} else {
				log.Printf("storage: match %s persisted", run.MatchID)
			}
		}()
	})

	lobbyMgr := lobby.NewManager(func(room *lobby.Room) {
		matchMgr.StartMatch(room)
	})

	mux := http.NewServeMux()
	mux.Handle("/ws", wsconn.Handler(sessions, lobbyMgr, matchMgr))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("volley server listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

// migrationsPath resolves the migrations directory relative to the binary.
// In Docker: /app/migrations. In local dev: <module-root>/migrations.
func migrationsPath() string {
	// Check if migrations/ exists next to the binary first
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "migrations")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	// Fall back to <source-root>/migrations (works for `go run`)
	_, file, _, ok := runtime.Caller(0)
	if ok {
		// file = backend/cmd/server/main.go → go up three dirs to backend/
		root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
		candidate := filepath.Join(root, "migrations")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	// Last resort
	return "migrations"
}
```

> **Important:** `main.go` calls `sessions.GetBySessionID(players[i].SessionID)`. This method does not yet exist on `auth.Store`. See Task 2d.

> **Important:** `main.go` calls `run.Mu()` and `db.RawDB()` — these accessor methods must be added. See Task 2e and 2f.

### 2d. Add `GetBySessionID` to `auth.Store`

**File:** `backend/internal/auth/session.go` — add an ID-keyed secondary index.

Extend `Store` struct to maintain a second map keyed by `Session.ID`:

```go
type Store struct {
	mu      sync.RWMutex
	byToken map[string]Session
	byID    map[string]Session // keyed by Session.ID
}

func NewStore() *Store {
	return &Store{
		byToken: make(map[string]Session),
		byID:    make(map[string]Session),
	}
}

func (s *Store) Put(session Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byToken[session.Token] = session
	s.byID[session.ID] = session
}

func (s *Store) GetByToken(token string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.byToken[token]
	return sess, ok
}

// GetBySessionID looks up a session by its short hex Session.ID.
func (s *Store) GetBySessionID(id string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.byID[id]
	return sess, ok
}

func (s *Store) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.byToken[token]; ok {
		delete(s.byID, sess.ID)
	}
	delete(s.byToken, token)
}
```

Existing `auth_test.go` tests `GetByToken` — verify they still pass. The `Delete` update removes from both maps.

### 2e. Expose `Mu()` accessor on `MatchRun`

**File:** `backend/internal/matchmgr/match_run.go`

The `mu` field is unexported. `main.go` (package `main`) cannot access it. Add an exported accessor:

```go
// Mu returns the MatchRun's mutex for external callers that need to snapshot state.
func (r *MatchRun) Mu() *sync.Mutex {
	return &r.mu
}
```

### 2f. Expose `RawDB()` accessor on `storage.DB`

**File:** `backend/internal/storage/db.go`

```go
// RawDB returns the underlying *sql.DB for migration use.
func (d *DB) RawDB() *sql.DB {
	if d == nil {
		return nil
	}
	return d.db
}
```

### Validation for Task 2

```sh
C:\Users\trent\sdk\go\bin\go.exe build ./cmd/server
C:\Users\trent\sdk\go\bin\go.exe test ./...
```

All 18+ tests pass. Build succeeds with no errors.

---

## Task 3: Dockerfile + fly.toml

### 3a. Create `backend/Dockerfile`

```dockerfile
# Stage 1: Build
FROM golang:1.22-alpine AS builder
WORKDIR /app

# Download dependencies first (layer cache)
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN go build -o server ./cmd/server

# Stage 2: Runtime image
FROM alpine:3.19
WORKDIR /app

# Copy binary and migrations
COPY --from=builder /app/server .
COPY migrations/ migrations/

EXPOSE 8080
CMD ["./server"]
```

Notes:
- `alpine:3.19` is ~8 MB. The final image is typically 15–25 MB.
- `migrations/` is copied into `/app/migrations/` — matches `migrationsPath()` logic (binary at `/app/server`, migrations at `/app/migrations`).
- No CGO needed (`github.com/lib/pq` is pure Go).

### 3b. Create `backend/fly.toml`

```toml
app = "volley-server"
primary_region = "ord"

[build]
  dockerfile = "Dockerfile"

[env]
  PORT = "8080"

[[services]]
  internal_port = 8080
  protocol      = "tcp"

  [[services.ports]]
    port     = 80
    handlers = ["http"]

  [[services.ports]]
    port     = 443
    handlers = ["tls", "http"]

[checks]
  [checks.alive]
    type     = "tcp"
    port     = 8080
    interval = "15s"
    timeout  = "5s"
```

Notes:
- `VOLLEY_ENV` is NOT set here — it is set via `fly secrets set` (Task 4, step 5) so it doesn't appear in plaintext in `fly.toml`.
- `DATABASE_URL` is set via `fly postgres attach` automatically.
- WebSocket upgrades work on Fly.io with HTTP handlers over TLS (WSS).

### 3c. Validate Docker build locally (optional but recommended)

```sh
# From repo root (D:\Pong-Mobile\)
docker build -t volley-server backend/
```

Expected output shape:
```
[+] Building XX.Xs
 => [builder 1/5] FROM docker.io/library/golang:1.22-alpine         OK
 => [builder 2/5] WORKDIR /app                                       OK
 => [builder 3/5] COPY go.mod go.sum ./                              OK
 => [builder 4/5] RUN go mod download                                OK
 => [builder 5/5] COPY . .                                           OK
 => [builder 6/5] RUN go build -o server ./cmd/server                OK
 => [stage-1 1/3] FROM docker.io/library/alpine:3.19                 OK
 => [stage-1 2/3] COPY --from=builder /app/server .                  OK
 => [stage-1 3/3] COPY migrations/ migrations/                       OK
 => exporting to image                                                OK
Successfully built <sha>
Successfully tagged volley-server:latest
```

Smoke-test the image:
```sh
docker run --rm -e PORT=8080 -p 8080:8080 volley-server
# Should log: "DATABASE_URL not set — running without persistence (dev mode)"
# Should log: "volley server listening on :8080"
```

---

## Task 4: Fly.io Deployment (interactive — run manually)

All commands run from `backend/` directory unless noted.

### Step 4.1 — Authenticate

```sh
fly auth login
```

Opens browser. Sign in with Fly.io account. Verify with:
```sh
fly auth whoami
# Expected: your-email@example.com
```

### Step 4.2 — Create the app

```sh
fly apps create volley-server
```

Expected:
```
New app created: volley-server
```

If name is taken: `fly apps create volley-server-<suffix>` and update `fly.toml` app field to match.

### Step 4.3 — Create PostgreSQL cluster

```sh
fly postgres create --name volley-db --region ord
```

Interactive prompts:
- Select plan: **Development** (cheapest, single node, ~$0/mo on free tier or $1.94/mo)
- Region: `ord` (Chicago)

Expected final output (save the connection strings):
```
Postgres cluster volley-db created
  Username:    postgres
  Password:    <generated-password>
  Hostname:    volley-db.internal
  Flycast:     fdaa:...
  Proxy port:  5432
  Postgres port: 5433
  Connection string: postgres://postgres:<password>@volley-db.flycast:5432

Save your credentials in a secure place — you won't be able to see them again!
```

### Step 4.4 — Attach Postgres to the app

```sh
fly postgres attach --app volley-server volley-db
```

This automatically sets `DATABASE_URL` as a secret on `volley-server`. Expected:
```
Postgres cluster volley-db is now attached to volley-server
The following secret was added to volley-server:
  DATABASE_URL=postgres://volley_server:<password>@volley-db.flycast:5432/volley_server?sslmode=disable
```

Verify the secret exists:
```sh
fly secrets list --app volley-server
# Should show: DATABASE_URL  <redacted>
```

### Step 4.5 — Set production env

```sh
fly secrets set VOLLEY_ENV="production" --app volley-server
```

Expected: `Secrets are staged for the first deployment`

### Step 4.6 — Deploy

From `backend/`:
```sh
fly deploy --app volley-server
```

Fly builds the Docker image remotely (or locally with `--local-only`), pushes it, and starts the VM.

Expected:
```
==> Building image
...
==> Pushing image to Fly
...
==> Creating release
--> release v2 created
--> Monitoring deployment

 1 desired, 1 placed, 1 healthy, 0 unhealthy [health checks: 1 total, 1 passing]
--> v2 deployed successfully
```

### Step 4.7 — Verify logs

```sh
fly logs --app volley-server
```

Expected log lines (within first few seconds):
```
[info] migrations applied from /app/migrations
[info] volley server listening on :8080
```

No `FATAL` lines. If you see `migrations: readdir "/app/migrations": no such file or directory`, the COPY step in Dockerfile is wrong — re-check Task 3a.

### Step 4.8 — Verify WebSocket endpoint

```sh
curl -i -N \
  -H "Upgrade: websocket" \
  -H "Connection: Upgrade" \
  -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  -H "Sec-WebSocket-Version: 13" \
  https://volley-server.fly.dev/ws
```

Expected: HTTP 101 Switching Protocols:
```
HTTP/1.1 101 Switching Protocols
Upgrade: websocket
Connection: Upgrade
Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=
```

If you get 404: check `fly.toml` services config. If you get 502: check fly logs.

### Step 4.9 — Update Flutter client URL

**File:** `client/lib/network/config.dart` (or wherever the WebSocket URL is currently defined)

```dart
const kServerUrl = 'wss://volley-server.fly.dev/ws';
```

Search for the current local URL (`ws://localhost:8080/ws` or similar) and replace it.

---

## Self-Review Checklist

- [x] `SaveMatchResult` handles win-by-score: highest `Score` slot gets `"win"`, other gets `"loss"`
- [x] `SaveMatchResult` handles win-by-forfeit: `Sender == nil` slot gets `"forfeit"`, other gets `"win"`
- [x] `SaveMatchResult` handles both-disconnected edge case: both get `"loss"` (won't normally happen, but handled)
- [x] `NewDB("")` returns `(nil, nil)` — callers nil-check `db` before use
- [x] `VOLLEY_ENV=production` + empty `DATABASE_URL` → `log.Fatal` at startup (fail-fast)
- [x] `VOLLEY_ENV` anything else + empty `DATABASE_URL` → dev mode, no DB, no crash
- [x] Migrations use `IF NOT EXISTS` everywhere — idempotent
- [x] `SaveUser` uses `ON CONFLICT (id) DO UPDATE` — idempotent re-insert
- [x] `SaveMatch` uses `ON CONFLICT (id) DO NOTHING` — idempotent re-insert
- [x] `auth.Store.Delete` updated to remove from both maps (no leak)
- [x] `GetBySessionID` added to `auth.Store` — `main.go` can look up sessions in onEnd
- [x] `MatchRun.Mu()` exported so `main.go` can lock for state snapshot
- [x] `storage.DB.RawDB()` exported so `main.go` can pass `*sql.DB` to `RunMigrations`
- [x] `PlayerID` added to `PlayerConn` — populated in `StartMatch`, used in onEnd callback
- [x] All 18 existing tests still pass (storage tests skip without DATABASE_URL)
- [x] Docker image copies `migrations/` into `/app/migrations/`
- [x] `fly.toml` does not hardcode `DATABASE_URL` (injected via secret)

---

## File Change Summary

| File | Action |
|---|---|
| `backend/internal/storage/db.go` | Create |
| `backend/internal/storage/migrations.go` | Create |
| `backend/internal/storage/db_test.go` | Create |
| `backend/migrations/001_initial.sql` | Create |
| `backend/Dockerfile` | Create |
| `backend/fly.toml` | Create |
| `backend/internal/auth/session.go` | Modify — add `byID` map, `GetBySessionID`, update `Put`/`Delete` |
| `backend/internal/matchmgr/match_run.go` | Modify — add `PlayerID` to `PlayerConn`, add `Mu()` accessor |
| `backend/internal/matchmgr/manager.go` | Modify — populate `PlayerID` in `StartMatch` |
| `backend/internal/storage/db.go` | Modify — add `RawDB()` |
| `backend/cmd/server/main.go` | Modify — storage init, migrations, onEnd callback, port from env |
| `backend/go.mod` / `go.sum` | Modify — add `github.com/lib/pq` |
| `client/lib/network/config.dart` | Modify — update WebSocket URL to `wss://volley-server.fly.dev/ws` |
