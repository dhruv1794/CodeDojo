CREATE TABLE IF NOT EXISTS newcomer_history_cache (
  repo_url TEXT NOT NULL,
  head_sha TEXT NOT NULL,
  payload TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(repo_url, head_sha)
);
