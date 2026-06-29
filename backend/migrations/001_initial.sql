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
