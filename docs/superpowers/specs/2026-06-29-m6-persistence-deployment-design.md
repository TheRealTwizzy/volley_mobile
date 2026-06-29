# M6 Persistence + Fly.io Deployment Design

## Goal

Persist match outcomes to PostgreSQL and deploy the Go server to Fly.io so real Android/iOS devices can connect to it. After this milestone, a completed match is stored in the database and two people on different phones can play together.

## Part 1: Persistence

### Package: `backend/internal/storage`

```
DB struct:
  db *sql.DB

NewDB(databaseURL string) (*DB, error)
(*DB) SaveUser(ctx, sess *auth.Session) error
(*DB) SaveMatch(ctx, room *lobby.Room, matchID string) error
(*DB) SaveMatchResult(ctx, matchID string, results [2]SlotResult) error

SlotResult struct:
  PlayerID string
  Score    int
  Result   string  // "win" | "loss" | "forfeit"
```

Driver: `github.com/lib/pq` (pure Go, no CGO).

Connection string from environment variable `DATABASE_URL`. Fail-fast at startup if not set and `VOLLEY_ENV != "dev"` (in dev, skip DB entirely).

### Schema Migrations

File: `backend/migrations/001_initial.sql`

```sql
CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY,
  display_name TEXT NOT NULL,
  is_guest BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS matches (
  id UUID PRIMARY KEY,
  room_code TEXT,
  player_one_id UUID REFERENCES users(id),
  player_two_id UUID REFERENCES users(id),
  status TEXT NOT NULL,
  points_to_win INTEGER NOT NULL,
  winner_id UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ,
  ended_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS match_results (
  id UUID PRIMARY KEY,
  match_id UUID NOT NULL REFERENCES matches(id),
  player_id UUID NOT NULL REFERENCES users(id),
  score INTEGER NOT NULL,
  result TEXT NOT NULL
);
```

Migrations run at server startup via `storage.RunMigrations(db)` which executes all `*.sql` files in `backend/migrations/` in lexicographic order. Uses `IF NOT EXISTS` — idempotent, no migration framework needed at this scale.

### Integration with matchmgr

In `matchmgr/manager.go`, the `onEnd` callback (called when a match ends) receives the `MatchRun`. The Manager's `onEnd` is wired in `main.go`:

```go
storage := storage.NewDB(os.Getenv("DATABASE_URL"))
matchMgr := matchmgr.NewManager(func(run *matchmgr.MatchRun) {
    // fire-and-forget, don't block match loop
    go func() {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        storage.SaveMatchResult(ctx, run.MatchID, run.FinalResults())
    }()
})
```

If `storage` is nil (dev mode), the callback is a no-op.

### Testing

`storage/db_test.go` — integration tests using `DATABASE_URL` env var pointing at a test DB. Skipped if env var not set:
```go
func TestSaveMatchResult(t *testing.T) {
    url := os.Getenv("DATABASE_URL")
    if url == "" { t.Skip("DATABASE_URL not set") }
    ...
}
```

## Part 2: Fly.io Deployment

### `backend/Dockerfile`

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o server ./cmd/server

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/server .
COPY migrations/ migrations/
EXPOSE 8080
CMD ["./server"]
```

### `backend/fly.toml`

```toml
app = "volley-server"
primary_region = "ord"  # Chicago — change to region closest to users

[build]
  dockerfile = "Dockerfile"

[env]
  VOLLEY_ENV = "production"
  PORT = "8080"

[[services]]
  internal_port = 8080
  protocol = "tcp"

  [[services.ports]]
    port = 80
    handlers = ["http"]

  [[services.ports]]
    port = 443
    handlers = ["tls", "http"]
```

WebSocket connections work over TLS (wss://) on Fly.io automatically when using HTTP handlers.

### `backend/cmd/server/main.go` — port from env

```go
port := os.Getenv("PORT")
if port == "" {
    port = "8080"
}
log.Fatal(http.ListenAndServe(":"+port, mux))
```

### Deployment Steps (in plan)

1. `fly auth login`
2. `fly apps create volley-server` (in `backend/` directory)
3. `fly postgres create --name volley-db` → get connection string
4. `fly secrets set DATABASE_URL="postgres://..."` 
5. `fly deploy` from `backend/`
6. Verify: `fly logs`, `wscat wss://volley-server.fly.dev/ws`

### Flutter client URL update

After deploy, update `client/lib/network/config.dart`:
```dart
const kServerUrl = 'wss://volley-server.fly.dev/ws';
```

## File Summary

| File | Action |
|---|---|
| `backend/internal/storage/db.go` | Create |
| `backend/internal/storage/migrations.go` | Create |
| `backend/internal/storage/db_test.go` | Create |
| `backend/migrations/001_initial.sql` | Create |
| `backend/Dockerfile` | Create |
| `backend/fly.toml` | Create |
| `backend/cmd/server/main.go` | Modify (port from env, wire storage) |
| `backend/go.mod` | Add `github.com/lib/pq` |
| `client/lib/network/config.dart` | Modify (update server URL) |
