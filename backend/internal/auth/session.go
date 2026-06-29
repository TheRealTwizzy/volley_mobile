package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Session holds identity info for one connected client.
type Session struct {
	ID          string    // short hex ID (used as session identifier)
	PlayerID    string    // "player_<hex>" — sent to clients
	Token       string    // opaque 32-char hex token for reconnect
	DisplayName string
	CreatedAt   time.Time
}

// NewSession creates a Session with crypto-random token and player ID.
func NewSession(displayName string) (Session, error) {
	token, err := randomHex(16) // 32 hex chars
	if err != nil {
		return Session{}, err
	}
	idBytes, err := randomHex(8) // 16 hex chars
	if err != nil {
		return Session{}, err
	}
	return Session{
		ID:          idBytes,
		PlayerID:    "player_" + idBytes,
		Token:       token,
		DisplayName: displayName,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

// Store is a concurrency-safe in-memory session store keyed by token.
type Store struct {
	mu      sync.RWMutex
	byToken map[string]Session
}

// NewStore returns an initialized Store.
func NewStore() *Store {
	return &Store{byToken: make(map[string]Session)}
}

// Put adds or replaces a session.
func (s *Store) Put(session Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byToken[session.Token] = session
}

// GetByToken looks up a session by opaque token.
func (s *Store) GetByToken(token string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.byToken[token]
	return sess, ok
}

// Delete removes a session by token.
func (s *Store) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byToken, token)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
