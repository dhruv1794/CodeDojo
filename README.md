# CodeDojo

CodeDojo is an open-source training ground for developers in the AI era.

It has two practice modes:

- **Reviewer mode**: CodeDojo injects one plausible bug into a real repository. You inspect the code, run tests, ask for limited Socratic hints, and submit the file, line range, and diagnosis.
- **Newcomer mode**: CodeDojo reverts a real feature commit. You rebuild the behavior from a stripped feature description, then CodeDojo grades the implementation against the original tests and reference diff.

The MVP is a CLI-first tool. It uses deterministic grading wherever possible, validator-gated coach hints, sqlite session history, and a sandbox driver that prefers Docker when available and falls back to a local working copy for development.

## Status

This repository is in active MVP development. The current build includes:

- Cobra CLI for `init`, `review`, `learn`, `author`, `benchmark`, `on-pr`, `status`, `stats`, `replay`, and `version`
- Reviewer and newcomer end-to-end flows for Go, Python, JavaScript, TypeScript, and Rust repositories
- AST and text mutation operators for reviewer mode
- Language-aware commit ranking, revert, restore, and grading for newcomer mode
- Mock, Anthropic, and Ollama coach backends
- Docker and local sandbox drivers
- SQLite persistence for sessions, replay events, scores, streaks, mutation logs, and stats

## Install

Prerequisites:

- Go 1.23+
- Git
- Docker, optional but recommended for stronger sandboxing
- Python 3.12+ with `pytest`, for Python repositories
- Node.js 20+ for JavaScript repositories; Node.js 22+ for TypeScript fixtures that use native type stripping
- Rust 1.76+ with Cargo, for Rust repositories
- `ANTHROPIC_API_KEY`, optional when using the Anthropic coach backend
- Ollama, optional when using the Ollama coach backend

Build the CLI:

```sh
make build
```

Run it from the repository:

```sh
./bin/codedojo version
```

You can also run commands without building first:

```sh
go run ./cmd/codedojo version
```

## Configure

The default configuration uses the deterministic mock coach and stores state under `~/.codedojo`.

```sh
./bin/codedojo init
```

The wizard writes `~/.codedojo/config.yaml`. Choose `mock` for local development, `anthropic` for the Anthropic Messages API, or `ollama` for a local Ollama server.

Anthropic can read the API key from either the config file or `ANTHROPIC_API_KEY`:

```sh
export ANTHROPIC_API_KEY=...
```

The default Anthropic API model is pinned to `claude-sonnet-4-20250514`. You can override it in `~/.codedojo/config.yaml`:

```yaml
coach:
  backend: anthropic
  model: claude-sonnet-4-20250514
```

Ollama uses these optional environment variables:

```sh
export OLLAMA_MODEL=llama3.1
export OLLAMA_BASE_URL=http://localhost:11434
```

## Quickstart: Reviewer Mode

Reviewer mode mutates one source file and asks you to find the bug.

```sh
./bin/codedojo review --repo testdata/sample-go-repo --difficulty 1 --budget 3
```

For reviewer v2 practice, ask CodeDojo to present a candidate set so the bug is
hidden among several plausible files:

```sh
./bin/codedojo review --repo ./path/to/repo --difficulty 3 --candidates 5
```

You can also request a compound kata with more than one injected bug. Grading
accepts any one mutation you can correctly localize and diagnose:

```sh
./bin/codedojo review --repo ./path/to/repo --difficulty 3 --candidates 5 --mutations 2
```

For interacting same-flow compounds in one Go function, add:

```sh
./bin/codedojo review --repo ./path/to/repo --difficulty 3 --mutations 2 --compound same-flow
```

Reviewer mode includes working-but-wrong bug classes such as pagination window
off-by-one mutations, JavaScript/TypeScript strict-equality weakening, and
race-friendly lock removal where code still builds while edge-case behavior
changes.

Inside the REPL:

```text
help
tests
cat calculator/calculator.go
diff
hint nudge
submit calculator/calculator.go:13 boundary comparison changed at the lower clamp check
```

The submission format is:

```text
submit <file>:<lineRange> <diagnosis>
```

Scoring gives partial credit for the correct file, nearby line range, operator class, and diagnosis quality. Diagnosis grading extracts file, line, operator, and mechanism evidence deterministically; the coach can only add a bounded mechanism-quality judgment, which is cached by mutation and diagnosis text. Hints subtract from the final score.

`submit` prints a progress line and runs grading and session cleanup under a time limit. Use `--submit-timeout` (default `3m`, accepted by both `review` and `sensei play`) to fail with a clear message instead of hanging if a kata's test command leaves a background process running.

## Author Mode

Educators can export curated reviewer katas as a versioned JSON mutation pack:

```sh
./bin/codedojo author pack --repo testdata/sample-go-repo --title "10 idiomatic Go bugs" --count 1 --output never_commit/idiomatic-go-bugs.json
```

The pack records the detected language, source repo head, task metadata, and the full mutation log with original/mutated snapshots so a curated kata set can be reviewed, shared, or imported by future tooling.

`author pack` fails if it cannot find `--count` unique mutation tasks. Pass `--allow-partial` to keep the tasks it did find: it writes the partial pack and prints a warning to stderr instead of discarding the work.

Sensei mode is the single-kata publishing path for a senior engineer or team lead. It records the source commit, difficulty, author/team name, and a learner-facing brief:

```sh
./bin/codedojo sensei publish \
  --repo testdata/sample-go-repo \
  --title "Clamp review kata" \
  --description "A lower-bound cleanup changed calculator behavior. Find the review bug." \
  --vet \
  --difficulty 1 \
  --output never_commit/clamp-sensei-kata.json
```

`--vet` samples mutation candidates until it finds one whose clean baseline passes tests and whose mutation fails tests. Use `--max-attempts` to bound real-repo authoring time.

The local web server can start that authored kata from a `?kata=` link or through `POST /api/sessions/sensei` with `{"pack_path":"/absolute/path/to/clamp-sensei-kata.json"}`. The server opens the source repository, checks out the recorded commit when present, applies the saved mutation in a hidden baseline, and runs the normal reviewer grading flow.

```text
http://localhost:8080/?kata=%2Fabsolute%2Fpath%2Fto%2Fclamp-sensei-kata.json
```

You can also play the same authored kata entirely from the CLI:

```sh
./bin/codedojo sensei inspect --pack never_commit/clamp-sensei-kata.json
./bin/codedojo sensei vet --pack never_commit/clamp-sensei-kata.json
./bin/codedojo sensei play --pack never_commit/clamp-sensei-kata.json
```

`sensei vet` opens the source once in a temporary baseline copy, checks out the recorded commit when present, verifies baseline tests pass, then copies that validated baseline per task, applies each saved mutation snapshot, and confirms the mutation makes tests fail before you share the pack.

For a specific task in a multi-kata pack, use the same task ID with `vet` or `play`:

```sh
./bin/codedojo sensei vet --pack never_commit/clamp-sensei-kata.json --task kata-001
./bin/codedojo sensei play --pack never_commit/clamp-sensei-kata.json --task kata-001
```

Run a pack as a local benchmark and write a stable results artifact:

```sh
./bin/codedojo benchmark run --pack never_commit/idiomatic-go-bugs.json --output never_commit/benchmark-results.json
```

Benchmark results include per-kata operator, difficulty, file location, test command, exit code, duration, and pass/fail status. This is the local artifact format intended for fixed benchmark suites and future leaderboard ingestion.

## PR Spotter Challenges

Generate a Markdown spotter challenge from a local PR diff or commit range:

```sh
./bin/codedojo on-pr --repo . --base origin/main --head HEAD --output spotter-challenge.md
```

`on-pr` reads the changed files from `base...head`, applies one mutation only inside those changed files in a copied working tree, and writes a challenge artifact for CI comments or manual review without modifying the source repository.

## Local Web UI

Run the local browser app when you want the full setup flow, file browser, editor, tests, hints, and submission panels in one place:

```sh
./bin/codedojo serve --repo testdata/sample-go-repo --port 8080
```

Then open `http://localhost:8080`.

The setup screen can use the `--repo` value, a pasted path or Git URL, an `?repo=` link, or the built-in repository browser. Local paths are inspected as you type. Git URLs use the **Open in CodeDojo** action so the server clones and inspects the repository once before you choose Review or Learn. Kata commands run in the server-side sandbox; Docker is used when available, with `CODEDOJO_SANDBOX=local` as the explicit local fallback.

Example local "open in CodeDojo" link:

```text
http://localhost:8080/?repo=https%3A%2F%2Fgithub.com%2Fowner%2Fproject.git
```

The browser starts at your home directory when no path is provided, shows detected repositories first, and includes:

- breadcrumbs for moving up and across the current path
- a direct path field for jumping to a known folder
- a hidden-folder toggle
- localStorage-backed recent repositories

Symlinked directories are labeled as symlinks and are not traversed by the browser. Enter the real target path directly if you intentionally want to use that repository.

## Language Support

CodeDojo detects repositories from standard project markers and uses the detected language's normal test command.

| Language | Detection | Learn | Review | Default tests | Notes |
|---|---|---:|---:|---|---|
| Go | `go.mod` | yes | yes | `go test ./...` | Reviewer uses Go AST mutators. |
| Python | `pyproject.toml`, `requirements.txt`, or `setup.py` | yes | yes | `python -m pytest` | Requires `pytest` for default test runs. |
| JavaScript | `package.json` | yes | yes | `npm test` | The local fixtures use Node's built-in test runner. |
| TypeScript | `tsconfig.json` or TypeScript package metadata | yes | yes | `npm test` | Use `.codedojo.yaml` for non-standard runners. |
| Rust | `Cargo.toml` | yes | yes | `cargo test` | Requires Cargo for learn grading. |

For monorepos or non-standard package managers, add `.codedojo.yaml` at the repo root:

```yaml
language: typescript
test_cmd: npm test
build_cmd: npm run build
```

## Quickstart: Newcomer Mode

Newcomer mode picks a real feature commit, checks out its parent, and gives you a stripped feature description.

```sh
./bin/codedojo learn --repo testdata/sample-go-repo --difficulty 2 --budget 3
```

Limit task selection to a commit range when you want a guided trail through recent work:

```sh
./bin/codedojo learn --repo testdata/sample-go-repo --range main~30..HEAD
```

Inside the REPL:

```text
help
cat calculator/calculator.go
tests
write calculator/calculator.go
package calculator

func Add(a, b int) int {
	return a + b
}

func Multiply(a, b int) int {
	return a * b
}
EOF
submit
```

Use `diff` before submitting to inspect your changes. Newcomer scoring combines correctness, coach-graded approach quality, added test coverage, hint deductions, and streak.

## Session History

Show recent sessions:

```sh
./bin/codedojo status
```

Show aggregate stats:

```sh
./bin/codedojo stats
```

Stats include aggregate scores, streaks, a mistake index by mutation operator, and engagement signals that combine solve rate, average score, hints used, time spent, and difficulty-profile axes. The `Next practice` line points at the weakest operator or profile bucket to target in the next kata. The cost dashboard reports coach calls, tokens, Anthropic estimated spend, cost per session, tokens per hint, and projected monthly spend.

Replay a kata timeline:

```sh
./bin/codedojo replay <session-id>
```

Replay shows the hidden reviewer mutation with original and mutated code snapshots, a deterministic difficulty profile for locality, subtlety, and required knowledge, post-grade coach commentary, a reasoning trace for how to investigate the bug, then the recorded timeline of session lifecycle, file opens, writes, test runs, diff views, hints, submit, grade, commentary, trace, and close events. Add `--step` for numbered playback frames with elapsed and gap timing, or `--step --delay 750ms` for paced terminal playback. Use `--format json` to export the same replay data as a stable JSON artifact for later visual playback or sharing tools.

## Architecture

```text
                +----------------------+
                |      cmd/codedojo    |
                +----------+-----------+
                           |
                           v
                +----------------------+
                |     internal/cli     |
                | review / learn REPLs |
                +----+------------+----+
                     |            |
        +------------+            +-------------+
        v                                       v
+---------------+                       +---------------+
| internal/repo |                       | internal/store |
| clone/open    |                       | sqlite/memory  |
| detect        |                       +-------+-------+
+-------+-------+                               |
        |                                       |
        v                                       v
+----------------------+              +------------------+
| internal/sandbox     |              | internal/session |
| Driver interface     |              | state/scoring    |
| local / docker       |              +--------+---------+
+----------+-----------+                       |
           |                                   |
           v                                   v
+----------------------+              +------------------+
| internal/modes       |              | internal/coach   |
| reviewer/newcomer    |<------------>| mock/anthropic/  |
| tasks and graders    |              | ollama + retry   |
+----------------------+              +------------------+
```

Important interfaces:

- `sandbox.Driver` starts an isolated or local session, executes commands, reads and writes files, and reports diffs.
- `coach.Coach` returns Socratic hints and grading feedback.
- `store.Store` persists sessions, events, scores, mutation logs, streaks, and cached history scans.
- Reviewer mode owns mutation selection and localization grading.
- Newcomer mode owns commit ranking, revert/restore, summary generation, and reimplementation grading.

## Sandbox Model

CodeDojo tries Docker first. When Docker is reachable, the CLI uses the `codedojo/go:1.23` image, disables networking, mounts only the workspace read-write, applies CPU and memory limits, and sets a hard timeout.

Build the sandbox images with:

```sh
make images
```

If Docker is unavailable, CodeDojo falls back to the local driver and prints a warning. The local driver is for development only. It runs commands on the host in a temporary working copy and is not a security boundary.

## Development

Run the core checks:

```sh
make test
make smoke
make e2e
npm --prefix web run test:e2e
```

Equivalent direct test command:

```sh
GOCACHE=/tmp/codedojo-gocache go test ./...
```

Smoke test:

```sh
GOCACHE=/tmp/codedojo-gocache make smoke
```

Linting requires `golangci-lint`:

```sh
make lint
```

## Project Layout

```text
cmd/codedojo/                  CLI entrypoint
internal/cli/                  Cobra commands and REPL flows
internal/coach/                Coach interface, validators, mock, Anthropic, Ollama
internal/config/               Config load/save
internal/modes/reviewer/       Reviewer task generation, mutators, grading
internal/modes/newcomer/       History scan, revert/restore, task generation, grading
internal/repo/                 Repository clone/open/detection helpers
internal/sandbox/              Sandbox interface, local driver, Docker driver
internal/session/              Session state machine and scoring helpers
internal/store/                SQLite and memory stores
configs/images/                Docker image definitions
configs/prompts/               Prompt templates
testdata/sample-go-repo/       Deterministic Go fixture repository
docs/                          Product and implementation plans
```

## License

MIT. See [LICENSE](LICENSE).
