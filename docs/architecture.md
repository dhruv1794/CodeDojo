# CodeDojo Architecture

CodeDojo is a CLI-first practice tool built around a small shared spine:

- repository loading and language detection
- sandboxed command execution
- session state and persistence
- validator-gated coaching
- mode-specific task generation and grading

Reviewer mode and newcomer mode share the same infrastructure but keep their domain logic separate.

## Package Map

```text
cmd/codedojo
  main.go
      |
      v
internal/cli
  root command, config wiring, REPLs, sandbox/coach selection
      |
      +-------------------+-------------------+--------------------+
      |                   |                   |                    |
      v                   v                   v                    v
internal/repo       internal/session    internal/sandbox      internal/coach
clone/open/detect   state + events      Driver interface      Coach interface
                                        local/docker          mock/anthropic/ollama
      |                   |                   |                    |
      +-------------------+-------------------+--------------------+
                          |
                          v
                    internal/store
                    sqlite/memory

internal/modes/reviewer
  mutation engine, mutators, reviewer task generation, reviewer grading

internal/modes/newcomer
  commit history scan/rank, revert/restore, newcomer task generation, grading
```

The CLI layer owns process-level wiring. Mode packages expose task generation and grading functions. Infrastructure packages avoid depending on CLI code.

## Core Interfaces

### Sandbox

`internal/sandbox/types.go` defines the sandbox contract:

```go
type Driver interface {
	Start(ctx context.Context, spec Spec) (Session, error)
}

type Session interface {
	Exec(ctx context.Context, cmd []string) (ExecResult, error)
	WriteFile(path string, data []byte) error
	ReadFile(path string) ([]byte, error)
	Diff() (string, error)
	Close() error
}
```

Implementations:

- `internal/sandbox/docker`: starts a Docker container, mounts the workspace at `/workspace`, applies resource limits, disables networking when requested, and enforces a timeout.
- `internal/sandbox/local`: copies work into a temporary directory and runs commands on the host. This is useful for development and tests, but it is not a security boundary.

The CLI uses Docker when the daemon is reachable and falls back to local with a warning.

### Coach

`internal/coach/types.go` defines:

```go
type Coach interface {
	Hint(ctx context.Context, req HintRequest) (Hint, error)
	Grade(ctx context.Context, req GradeRequest) (Grade, error)
}
```

Backends:

- `internal/coach/mock`: deterministic hints and grades for tests and local demos
- `internal/coach/anthropic`: Anthropic Messages API client
- `internal/coach/ollama`: Ollama HTTP API client

`coach.RetryWithStricterPrompt` wraps a backend with output validation. It rejects hints that contain large code blocks, function definitions, or task-specific banned identifiers. After three failed attempts it returns a generic safe nudge.

### Session

`internal/session` owns the state machine:

```text
created -> running -> submitted -> graded -> closed
```

`session.Manager` coordinates:

- creating a persisted session row
- starting a sandbox session
- recording hint events
- moving submissions through the state machine
- closing the sandbox and recording close events

The manager depends on a narrow `session.Store` interface. SQLite and memory stores satisfy the methods needed by the manager; mode-specific persistence, such as mutation logs and history caches, uses separate store methods/interfaces.

### Store

`internal/store/sqlite` is the production store. Migrations create tables for:

- sessions
- events
- scores
- mutation logs
- newcomer history cache
- streak and engagement data

`internal/store/memory` supports unit tests that do not need SQLite behavior.

## Reviewer Sequence

```text
user
  |
  v
codedojo review --repo <path-or-url> --difficulty <1..5>
  |
  v
internal/cli.runReview
  |
  +--> config.Load
  +--> repo.OpenLocal or repo.Clone
  +--> reviewer.GenerateTask
  |      |
  |      v
  |    mutate.Engine.SelectAndApply
  |      |
  |      +--> CandidateFiles via git history
  |      +--> parse Go files with go/parser
  |      +--> collect Mutator candidates
  |      +--> apply one AST mutation
  |      +--> format source with go/printer
  |      +--> write .codedojo/mutation.json
  |
  +--> hide mutation log from workspace
  +--> select docker or local sandbox
  +--> session.Manager.New
  |
  v
review REPL
  |
  +--> tests: sandbox.Exec(test command)
  +--> cat:   sandbox.ReadFile
  +--> diff:  sandbox.Diff
  +--> hint:  session.Manager.RequestHint -> coach
  +--> submit <file>:<range> <diagnosis>
          |
          v
        reviewer.Grade
          |
          +--> file score
          +--> line proximity score
          +--> operator class score
          +--> coach diagnosis grade
          +--> hint deduction, time bonus, streak multiplier
          |
          v
        persist score, update streak, close session
```

Reviewer mode currently supports Go AST mutators only.

## Newcomer Sequence

```text
user
  |
  v
codedojo learn --repo <path-or-url> --difficulty <1..5>
  |
  v
internal/cli.runLearn
  |
  +--> config.Load
  +--> repo.OpenLocal or repo.Clone
  +--> newcomer.GenerateTask
  |      |
  |      +--> history.Scan last N commits
  |      +--> history.Rank candidates
  |      +--> select candidate by difficulty
  |      +--> revert.Revert to parent commit
  |      +--> compute reference diff
  |      +--> extract banned identifiers from added lines
  |      +--> summarize feature without implementation details
  |      +--> validate summary
  |
  +--> detect language and test command
  +--> select docker or local sandbox
  +--> session.Manager.New
  |
  v
learn REPL
  |
  +--> tests: sandbox.Exec(test command)
  +--> cat:   sandbox.ReadFile
  +--> write: sandbox.WriteFile
  +--> diff:  sandbox.Diff
  +--> hint:  session.Manager.RequestHint -> coach
  +--> submit
          |
          v
        newcomer.Grade
          |
          +--> run original tests in sandbox
          +--> coach approach grade using user diff and reference diff
          +--> count newly added test funcs
          +--> hint deduction and streak multiplier
          |
          v
        persist score, update streak, close session
```

Newcomer summaries are deliberately constrained. The task generator validates that introduced identifiers from the reference diff do not appear in the user-facing description.

## Repository Loading

`internal/repo` provides:

- `OpenLocal(srcPath)`: copies a local repository into a temporary working directory
- `Clone(ctx, url, dest)`: clones a remote repository
- `DetectLanguage(path)`: detects Go, JavaScript/TypeScript, or Python projects and returns build/test commands

CodeDojo does not mutate the user-provided local path directly. Work happens in a copied or cloned working tree.

## Prompt Templates

Prompt templates live in two places:

- `configs/prompts/...`: source prompt files tracked for editing
- `internal/coach/prompts/templates/...`: embedded copies used by the Go binary

When prompt behavior changes, keep both locations in sync unless the project is refactored to embed from a single source directory.

## Adding A Reviewer Mutator

Reviewer mutators live under `internal/modes/reviewer/mutate/op`.

1. Create a new file, for example `timeout.go`.
2. Implement `mutate.Mutator`:

```go
type Timeout struct{}

func (Timeout) Name() string { return "timeout" }
func (Timeout) Difficulty() int { return 3 }
func (Timeout) Candidates(file *ast.File) []mutate.Site { /* ... */ }
func (Timeout) Apply(file *ast.File, site mutate.Site) (mutate.Mutation, error) { /* ... */ }
```

3. `Candidates` should return precise AST sites. Set `Site.Node`; the engine fills file path and line/column data when absent.
4. `Apply` should mutate the parsed AST and return a `mutate.Mutation` with a useful `Description`, `Original`, or `Mutated` value when available.
5. Register the mutator in `internal/modes/reviewer/mutate/op/registry.go` by adding it to `All()`.
6. Add table-driven tests in `internal/modes/reviewer/mutate/op`, including:

- candidate discovery
- expected mutated source
- no-op or unsupported-shape behavior

7. If the operator creates code that may fail compilation often, add or adjust mutation gate coverage in `internal/modes/reviewer/mutate`.

The mutation engine chooses the candidate with the closest mutator difficulty to the requested task difficulty, so keep difficulty values stable and meaningful.

## Adding A Coach Backend

Coach backends live under `internal/coach/<backend>`.

1. Implement the `coach.Coach` interface.
2. Keep backend-specific configuration local to the backend package.
3. Add unit tests with fake HTTP servers or deterministic doubles.
4. Wire the backend in `internal/cli/coach.go`.
5. Add the backend to `codedojo init` choices in `internal/cli/init.go`.
6. Ensure hint responses are safe under `coach.RetryWithStricterPrompt`.
7. Add build-tagged integration tests if the backend needs an external service.

Backends should return short, direct hints and grades. The validator is a safety net, not the primary behavior.

## Adding A Sandbox Driver

Sandbox drivers live under `internal/sandbox/<driver>`.

1. Implement `sandbox.Driver`.
2. Return a `sandbox.Session` that honors context cancellation in `Exec`.
3. Keep all paths relative to the workspace for `ReadFile`, `WriteFile`, and `Diff`.
4. Enforce `Spec.Timeout`, `Spec.Network`, `Spec.CPULimit`, and `Spec.MemoryLimit` where the platform supports them.
5. Add unit tests for file round trips, command execution, diff behavior, cleanup, and cancellation.
6. Add integration tests behind a build tag for platform-specific runtime dependencies.
7. Wire selection in `internal/cli/sandbox_driver.go`.

## Testing Strategy

Use focused unit tests for mutators, validators, scorers, stores, and state transitions. Use CLI tests for scripted end-to-end behavior. Use build-tagged integration tests for Docker, Anthropic, and Ollama.

Common verification commands:

```sh
GOCACHE=/tmp/codedojo-gocache go test ./...
GOCACHE=/tmp/codedojo-gocache make smoke
make e2e
```

Run `make smoke` when a change affects CLI wiring, sandbox selection, session management, repository loading, coach wrapping, or the sample repo path.
