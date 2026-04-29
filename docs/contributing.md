# Contributing To CodeDojo

CodeDojo is still an MVP, so contributions should stay small, testable, and aligned with the existing package boundaries. Prefer changes that improve the CLI practice loop, deterministic grading, sandbox safety, or mode extensibility.

## Development Setup

Prerequisites:

- Go 1.23+
- Git
- Docker, optional for Docker sandbox work
- `golangci-lint`, optional for `make lint`

Run the standard checks from the repository root:

```sh
GOCACHE=/tmp/codedojo-gocache go test ./...
GOCACHE=/tmp/codedojo-gocache make smoke
```

For mode-level end-to-end checks:

```sh
make e2e-reviewer
make e2e-newcomer
make e2e
```

Use build-tagged integration tests only when the test needs an external service such as Docker, Anthropic, or Ollama.

## Coding Guidelines

- Keep internal implementation under `internal/` unless there is a real external consumer.
- Prefer existing package patterns over new abstractions.
- Pass `context.Context` as the first argument for long-running operations.
- Wrap errors with `%w` when returning underlying errors.
- Keep generated or user-provided repositories isolated from the original path; use the `internal/repo` helpers.
- Add focused tests next to the package being changed.
- Do not add dependencies without a clear reason and an update to the implementation plan if the choice affects project direction.

## Adding A Reviewer Mutator

Reviewer mutators are Go AST transforms used by reviewer mode. They live in:

```text
internal/modes/reviewer/mutate/op/
```

Each mutator implements:

```go
type Mutator interface {
	Name() string
	Difficulty() int
	Candidates(*ast.File) []Site
	Apply(*ast.File, Site) (Mutation, error)
}
```

### 1. Create The Operator

Add a file such as `timeout.go`:

```go
package op

import (
	"fmt"
	"go/ast"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
)

type Timeout struct{}

func (Timeout) Name() string    { return "timeout" }
func (Timeout) Difficulty() int { return 3 }

func (Timeout) Candidates(file *ast.File) []mutate.Site {
	var sites []mutate.Site
	ast.Inspect(file, func(node ast.Node) bool {
		// Find precise AST nodes here.
		return true
	})
	return sites
}

func (t Timeout) Apply(_ *ast.File, site mutate.Site) (mutate.Mutation, error) {
	if site.Node == nil {
		return mutate.Mutation{}, fmt.Errorf("timeout site has nil node")
	}
	// Mutate the AST node in place.
	return mutate.Mutation{
		Operator:    t.Name(),
		Difficulty:  t.Difficulty(),
		Description: "changed timeout behavior",
		AppliedAt:   time.Now().UTC(),
	}, nil
}
```

`Candidates` should:

- use `ast.Inspect` or targeted AST traversal
- return only sites that `Apply` can safely mutate
- set `Site.Node`
- set `Description` and useful `Metadata`

The mutation engine fills `FilePath`, line, and column data when they are missing.

`Apply` should:

- type-check `site.Node` before mutating
- mutate the AST in place
- return a useful `mutate.Mutation`
- avoid formatting source manually; the engine formats the final AST with `go/printer`

### 2. Register The Operator

Add the mutator to `All()` in:

```text
internal/modes/reviewer/mutate/op/registry.go
```

Example:

```go
func All() []mutate.Mutator {
	return []mutate.Mutator{
		Boundary{},
		Conditional{},
		ErrorDrop{},
		SliceBounds{},
		Timeout{},
	}
}
```

If tests assert registry names or difficulty buckets, update those expectations.

### 3. Add Mutator Tests

Add tests in `internal/modes/reviewer/mutate/op`. Existing tests use the `assertMutator` helper in `operators_test.go`.

Cover:

- the number of candidates found in representative source
- the exact mutated source after formatting
- registry inclusion
- unsupported node shapes if `Apply` has meaningful error handling

Run:

```sh
GOCACHE=/tmp/codedojo-gocache go test ./internal/modes/reviewer/mutate/op
GOCACHE=/tmp/codedojo-gocache go test ./internal/modes/reviewer/mutate ./internal/modes/reviewer
```

### 4. Check Task Quality

A mutator should create a plausible bug, not just any syntactic change. Before marking it done, verify:

- mutated code usually compiles
- at least some tests still pass, so the hunt remains realistic
- the line reported in `MutationLog` points near the actual changed expression
- the diagnosis can be described without leaking the exact implementation

If the operator frequently breaks compilation or all tests, adjust the candidate filter or mutation gates.

## Adding A Coach Backend

Coach backends live under:

```text
internal/coach/<backend>/
```

They implement:

```go
type Coach interface {
	Hint(ctx context.Context, req HintRequest) (Hint, error)
	Grade(ctx context.Context, req GradeRequest) (Grade, error)
}
```

### 1. Implement The Backend

Create a package such as `internal/coach/example`.

Backend requirements:

- return short Socratic hints from `Hint`
- return numeric scores and concise feedback from `Grade`
- respect `ctx`
- avoid printing secrets or raw provider responses
- keep provider-specific request/response types private unless tests need them
- expose usage or cost methods only when the CLI has a real consumer

Hints are wrapped by `coach.RetryWithStricterPrompt` in CLI wiring, but the backend should still be prompted to avoid code leaks.

### 2. Add Tests

For HTTP providers, prefer `httptest.Server` unit tests. Cover:

- successful hint request
- successful grade parsing
- non-2xx or malformed response errors
- usage/cost accounting if implemented
- interaction with `coach.RetryWithStricterPrompt` when the first response leaks code

External-service tests should be optional and build-tagged, following the Anthropic and Ollama integration test pattern.

### 3. Wire CLI Selection

Update:

```text
internal/cli/coach.go
internal/cli/init.go
```

`newBackendCoach` should construct the raw backend. `buildCoach` will wrap it with retry and validation for hints.

If the backend needs configuration, prefer environment variables or the existing config file shape until the implementation plan decides on a broader config model.

### 4. Update Docs

Update `README.md` if users can select the backend directly. Update this file if the backend introduces a new contribution pattern.

## Prompt Templates

Prompt templates are embedded from:

```text
internal/coach/prompts/templates/
```

Editable source copies also exist under:

```text
configs/prompts/
```

Keep both copies synchronized when changing prompt wording. Prompt tests live in `internal/coach/prompts`.

## Adding A Sandbox Driver

Sandbox drivers live under:

```text
internal/sandbox/<driver>/
```

A driver must implement `sandbox.Driver` and return a `sandbox.Session`.

Minimum behavior:

- `Exec` captures stdout, stderr, and exit code
- `Exec` respects context cancellation
- `WriteFile` and `ReadFile` operate relative to the workspace
- `Diff` returns the current git diff against the starting state
- `Close` cleans up runtime resources

Wire driver selection in:

```text
internal/cli/sandbox_driver.go
```

Add unit tests for deterministic behavior and build-tagged integration tests for runtime dependencies.

## Documentation Checklist

When changing user-facing behavior, update:

- `README.md` for install, configuration, and quickstart changes
- `docs/architecture.md` for package boundaries, interfaces, or flow changes
- `docs/security.md` for sandbox or threat-model changes
- `docs/IMPLEMENTATION_PLAN.md` only when marking completed tasks or changing the source-of-truth plan

Do not edit `docs/plan.md` unless the product plan itself changes.
