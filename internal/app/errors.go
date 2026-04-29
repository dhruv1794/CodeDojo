package app

import (
	"errors"
	"net/http"
	"strings"
)

type ProductError struct {
	Code    string   `json:"code"`
	Title   string   `json:"title"`
	Message string   `json:"message"`
	Actions []string `json:"actions,omitempty"`
	Status  int      `json:"-"`
	cause   error
}

func (e ProductError) Error() string {
	if e.Message == "" {
		return e.Title
	}
	return e.Title + ": " + e.Message
}

func (e ProductError) Unwrap() error {
	return e.cause
}

func ExplainError(err error) ProductError {
	if err == nil {
		return ProductError{
			Code:    "request_failed",
			Title:   "Request failed",
			Message: "No error detail was returned.",
			Actions: []string{"Try the action again.", "Check the terminal running codedojo serve for more detail."},
			Status:  http.StatusInternalServerError,
		}
	}
	var product ProductError
	if errors.As(err, &product) {
		return withDefaultStatus(product)
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "repo is required"):
		product = ProductError{
			Code:    "repo_required",
			Title:   "Choose a repository",
			Message: "CodeDojo needs a local repository path or a Git URL before it can create a practice session.",
			Actions: []string{"Enter an absolute path to a cloned repository.", "Start the server with codedojo serve --repo /path/to/repo."},
		}
	case strings.Contains(text, "no test command detected"):
		product = noTestCommandError(err)
	case strings.Contains(text, "no suitable newcomer commits found") || strings.Contains(text, "no suitable historical commits found"):
		product = noLearnCommitsError(err)
	case strings.Contains(text, "supports go repositories only") || strings.Contains(text, "review for ") && strings.Contains(text, "coming soon"):
		product = reviewUnsupportedError(err)
	case strings.Contains(text, "no mutation candidates found") || strings.Contains(text, "no go files with neighbouring tests found"):
		product = noReviewCandidatesError(err)
	case strings.Contains(text, "docker"):
		product = dockerUnavailableError(err)
	case strings.Contains(text, "session ") && strings.Contains(text, " not found"):
		product = ProductError{
			Code:    "session_not_found",
			Title:   "Session not found",
			Message: "That practice session is no longer active.",
			Actions: []string{"Start another session from the setup screen."},
			Status:  http.StatusNotFound,
		}
	case strings.Contains(text, "hint budget exhausted"):
		product = ProductError{
			Code:    "hint_budget_exhausted",
			Title:   "Hint budget exhausted",
			Message: "This session has used every available hint.",
			Actions: []string{"Keep investigating with tests and the diff.", "Start another session with a larger hint budget."},
		}
	case strings.Contains(text, "diagnosis is required"):
		product = ProductError{
			Code:    "diagnosis_required",
			Title:   "Add a diagnosis",
			Message: "Review submissions need a short explanation of what is wrong, not only a file and line.",
			Actions: []string{"Write one or two sentences describing the hidden bug.", "Include the behavior you expect to fail."},
		}
	default:
		product = ProductError{
			Code:    "request_failed",
			Title:   "Request failed",
			Message: err.Error(),
			Actions: []string{"Try the action again.", "Check the terminal running codedojo serve for more detail."},
		}
	}
	product.cause = err
	return withDefaultStatus(product)
}

func withDefaultStatus(err ProductError) ProductError {
	if err.Status == 0 {
		err.Status = http.StatusBadRequest
	}
	return err
}

func noTestCommandError(cause error) ProductError {
	return ProductError{
		Code:    "no_test_command",
		Title:   "No test command detected",
		Message: "CodeDojo could not infer how to run this repository's tests.",
		Actions: []string{"Add .codedojo.yaml with a test_cmd, for example test_cmd: go test ./...", "Use a standard project marker such as go.mod, package.json, pyproject.toml, or Cargo.toml."},
		cause:   cause,
	}
}

func noLearnCommitsError(cause error) ProductError {
	return ProductError{
		Code:    "no_learn_commits",
		Title:   "No Learn tasks found",
		Message: "Learn mode needs recent, revertable commits that include tests and a small enough feature change to rebuild.",
		Actions: []string{"Try a repository with several feature commits and test changes.", "Use Review mode if this is a small Go repository without useful history."},
		cause:   cause,
	}
}

func reviewUnsupportedError(cause error) ProductError {
	return ProductError{
		Code:    "review_unsupported_language",
		Title:   "Review is Go-only right now",
		Message: "The current review engine uses Go AST mutations. Other detected languages can still use Learn when tests and commit history are available.",
		Actions: []string{"Choose Learn for this repository.", "Use a Go repository for Review practice."},
		cause:   cause,
	}
}

func noReviewCandidatesError(cause error) ProductError {
	return ProductError{
		Code:    "no_review_candidates",
		Title:   "No hidden-bug candidates found",
		Message: "Review mode looks for non-generated Go source files with nearby tests and mutation sites such as comparisons, conditionals, error checks, or slice bounds.",
		Actions: []string{"Try a Go repository with source files and matching _test.go files.", "Commit the source and tests before starting Review.", "Use Learn if this repo has useful tested feature commits."},
		cause:   cause,
	}
}

func dockerUnavailableError(cause error) ProductError {
	return ProductError{
		Code:    "docker_unavailable",
		Title:   "Docker sandbox unavailable",
		Message: "CodeDojo could not start the Docker sandbox. The CLI can fall back to the local driver, but local execution is for development and does not isolate untrusted code.",
		Actions: []string{"Start Docker Desktop and try again.", "Use only repositories you trust when running with the local fallback.", "Read docs/security.md for the sandbox tradeoff."},
		cause:   cause,
	}
}
