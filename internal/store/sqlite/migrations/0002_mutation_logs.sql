CREATE TABLE IF NOT EXISTS mutation_logs (
  id TEXT PRIMARY KEY,
  session_id TEXT,
  repo_path TEXT NOT NULL,
  head_sha TEXT NOT NULL DEFAULT '',
  difficulty INTEGER NOT NULL,
  operator TEXT NOT NULL,
  file_path TEXT NOT NULL,
  start_line INTEGER NOT NULL,
  end_line INTEGER NOT NULL,
  payload TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(session_id) REFERENCES sessions(id)
);

CREATE INDEX IF NOT EXISTS mutation_logs_session_id_idx ON mutation_logs(session_id, created_at);
