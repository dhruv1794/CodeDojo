CREATE TABLE IF NOT EXISTS streaks (
  id INTEGER PRIMARY KEY CHECK(id = 1),
  current_streak INTEGER NOT NULL,
  best_streak INTEGER NOT NULL,
  updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO streaks(id, current_streak, best_streak, updated_at)
VALUES(1, 0, 0, '');
