package main

import (
	"log"
	"net/http"

	"github.com/pong-mobile/backend/internal/auth"
	"github.com/pong-mobile/backend/internal/wsconn"
)

func main() {
	sessions := auth.NewStore()

	mux := http.NewServeMux()
	mux.Handle("/ws", wsconn.Handler(sessions))

	addr := ":8080"
	log.Printf("volley server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
