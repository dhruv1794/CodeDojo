CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  mode TEXT NOT NULL,
  repo TEXT NOT NULL,
  task TEXT NOT NULL,
  hint_budget INTEGER NOT NULL,
  hints_used INTEGER NOT NULL,
  score INTEGER NOT NULL,
  state TEXT NOT NULL,
  started_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  type TEXT NOT NULL,
  payload TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  FOREIGN KEY(session_id) REFERENCES sessions(id)
);

CREATE INDEX IF NOT EXISTS events_session_id_idx ON events(session_id, id);

CREATE TABLE IF NOT EXISTS scores (
  session_id TEXT PRIMARY KEY,
  score INTEGER NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(session_id) REFERENCES sessions(id)
);
