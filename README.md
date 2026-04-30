# CodeDojo

CodeDojo is an open-source training ground for developers in the AI era.

It has two practice modes:

- **Reviewer mode**: CodeDojo injects one plausible bug into a real Go repository. You inspect the code, run tests, ask for limited Socratic hints, and submit the file, line range, and diagnosis.
- **Newcomer mode**: CodeDojo reverts a real feature commit. You rebuild the behavior from a stripped feature description, then CodeDojo grades the implementation against the original tests and reference diff.

The MVP is a CLI-first tool. It uses deterministic grading wherever possible, validator-gated coach hints, sqlite session history, and a sandbox driver that prefers Docker when available and falls back to a local working copy for development.

## Status

This repository is in active MVP development. The current build includes:

- Cobra CLI for `init`, `review`, `learn`, `status`, `stats`, and `version`
- Reviewer and newcomer end-to-end flows for Go, Python, JavaScript, TypeScript, and Rust repositories
- AST and text mutation operators for reviewer mode
- Language-aware commit ranking, revert, restore, and grading for newcomer mode
- Mock, Anthropic, and Ollama coach backends
- Docker and local sandbox drivers
- SQLite persistence for sessions, scores, streaks, mutation logs, and stats

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

Scoring gives partial credit for the correct file, nearby line range, operator class, and diagnosis quality. Hints subtract from the final score.

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
