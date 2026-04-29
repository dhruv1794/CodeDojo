package cli

import (
	"fmt"
	"os"

	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/coach/anthropic"
	"github.com/dhruvmishra/codedojo/internal/coach/mock"
	"github.com/dhruvmishra/codedojo/internal/coach/ollama"
	"github.com/dhruvmishra/codedojo/internal/config"
)

// buildCoach turns a Config into a wrapped coach.Coach. The validator+retry
// pipeline is applied to whichever backend the user picked. Banned identifier
// lists are passed in by callers that know the per-task list (e.g. the
// reviewer mutation identifier or the newcomer reference identifiers).
func buildCoach(cfg config.Config, banned []string) (coach.Coach, error) {
	inner, err := newBackendCoach(cfg)
	if err != nil {
		return nil, err
	}
	return coach.RetryWithStricterPrompt(inner, banned), nil
}

// newBackendCoach returns the raw backend coach, without retry/validator
// wrapping. Used by graders that call the coach directly with their own
// banned-identifier list, or for token/cost reporting.
func newBackendCoach(cfg config.Config) (coach.Coach, error) {
	switch cfg.Coach.Backend {
	case "", "mock":
		return mock.Coach{}, nil
	case "anthropic":
		key := cfg.Coach.APIKey
		if key == "" {
			key = os.Getenv("ANTHROPIC_API_KEY")
		}
		if key == "" {
			return nil, fmt.Errorf("anthropic backend selected but no API key (set ANTHROPIC_API_KEY or run codedojo init)")
		}
		return anthropic.New(key), nil
	case "ollama":
		c := ollama.New(os.Getenv("OLLAMA_MODEL"))
		if baseURL := os.Getenv("OLLAMA_BASE_URL"); baseURL != "" {
			c.BaseURL = baseURL
		}
		return c, nil
	default:
		return nil, fmt.Errorf("unknown coach backend %q", cfg.Coach.Backend)
	}
}
