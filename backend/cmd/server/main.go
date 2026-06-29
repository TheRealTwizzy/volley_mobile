package main

import (
	"log"
	"net/http"
	"os"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/lobby"
	"github.com/pong-mobile/backend/internal/wsconn"
)

func main() {
	sessions := auth.NewStore()
	lobbyMgr := lobby.NewManager(func(room *lobby.Room) {
		// M3 stub: match loop implemented in M4.
		log.Printf("match start triggered for room %s (stub)", room.Code)
	})

	mux := http.NewServeMux()
	mux.Handle("/ws", wsconn.Handler(sessions, lobbyMgr))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	log.Printf("volley server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
