#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${CODEDOJO_DEMO_PORT:-18080}"
REPO="${1:-$ROOT/testdata/sample-go-repo}"
HOME_DIR="$(mktemp -d "${TMPDIR:-/tmp}/codedojo-demo-home.XXXXXX")"
LOG="$HOME_DIR/server.log"
GO_MODCACHE="${GOMODCACHE:-$(go env GOMODCACHE)}"

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]]; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" >/dev/null 2>&1 || true
  fi
  rm -rf "$HOME_DIR"
}
trap cleanup EXIT

cd "$ROOT"
HOME="$HOME_DIR" GOCACHE="${GOCACHE:-/tmp/codedojo-gocache}" GOMODCACHE="$GO_MODCACHE" go run ./cmd/codedojo serve --repo "$REPO" --port "$PORT" >"$LOG" 2>&1 &
SERVER_PID=$!

ready=0
for _ in $(seq 1 60); do
  if curl -fsS "http://127.0.0.1:$PORT/api/health" >/dev/null 2>&1; then
    ready=1
    break
  fi
  if ! kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    cat "$LOG"
    exit 1
  fi
  sleep 0.25
done
if [[ "$ready" != "1" ]]; then
  cat "$LOG"
  echo "server did not become ready on port $PORT" >&2
  exit 1
fi

base="http://127.0.0.1:$PORT"
echo "CodeDojo demo server: $base"

echo
echo "1. Repo scan"
curl -fsS "$base/api/preflight" \
  -H "content-type: application/json" \
  -d "{\"repo\":\"$REPO\"}"
echo

echo
echo "2. Start Review session"
session_json="$(curl -fsS "$base/api/sessions/review" \
  -H "content-type: application/json" \
  -d "{\"repo\":\"$REPO\",\"difficulty\":1,\"hint_budget\":1}")"
echo "$session_json"
session_id="$(printf '%s' "$session_json" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')"
if [[ -z "$session_id" ]]; then
  echo "could not parse session id" >&2
  exit 1
fi

echo
echo "3. Run tests"
curl -fsS "$base/api/sessions/$session_id/tests" \
  -H "content-type: application/json" \
  -d '{}'
echo

echo
echo "4. Select a line and submit diagnosis"
curl -fsS "$base/api/sessions/$session_id/submit" \
  -H "content-type: application/json" \
  -d '{"file_path":"calculator/calculator.go","start_line":13,"end_line":13,"operator_class":"boundary","diagnosis":"The lower-bound comparison was changed, so one edge case now falls through instead of clamping."}'
echo
