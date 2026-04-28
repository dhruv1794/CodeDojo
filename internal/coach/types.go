package coach

import "context"

type HintLevel int

const (
	LevelNudge HintLevel = iota
	LevelQuestion
	LevelPointer
	LevelConcept
)

type Hint struct {
	Level   HintLevel
	Content string
	Cost    int
}

type HintRequest struct {
	SessionID string
	Level     HintLevel
	Context   string
	Strict    bool
}

type GradeRequest struct {
	SessionID string
	Rubric    string
	Answer    string
}

type Grade struct {
	Score    int
	Feedback string
}

type Coach interface {
	Hint(ctx context.Context, req HintRequest) (Hint, error)
	Grade(ctx context.Context, req GradeRequest) (Grade, error)
}

func HintCost(level HintLevel) int {
	switch level {
	case LevelPointer:
		return 20
	case LevelConcept:
		return 30
	default:
		return 10
	}
}
