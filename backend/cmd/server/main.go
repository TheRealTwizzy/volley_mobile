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

			// Build sessions array from auth store by SessionID lookup
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
