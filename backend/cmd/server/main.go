package main

import (
	"log"
	"net/http"
	"os"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/lobby"
	"github.com/pong-mobile/backend/internal/matchmgr"
	"github.com/pong-mobile/backend/internal/wsconn"
)

func main() {
	sessions := auth.NewStore()
	matchMgr := matchmgr.NewManager(nil)
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
