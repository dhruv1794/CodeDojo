package history

import "slices"

func Rank(candidates []CommitCandidate) []CommitCandidate {
	ranked := make([]CommitCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Filtered {
			continue
		}
		candidate.Score = scoreCandidate(candidate)
		if candidate.Score > 0 {
			ranked = append(ranked, candidate)
		}
	}
	slices.SortFunc(ranked, func(a, b CommitCandidate) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		return b.Additions - a.Additions
	})
	return ranked
}

func scoreCandidate(candidate CommitCandidate) int {
	if !candidate.IsRevertable || !candidate.HasTests || len(candidate.Files) == 0 {
		return 0
	}
	score := 100
	switch {
	case len(candidate.Files) == 1:
		score += 25
	case len(candidate.Files) <= 3:
		score += 15
	default:
		return 0
	}
	switch {
	case candidate.Additions <= 40:
		score += 25
	case candidate.Additions <= 100:
		score += 15
	case candidate.Additions <= 200:
		score += 5
	default:
		return 0
	}
	if candidate.Deletions <= 80 {
		score += 10
	}
	for _, file := range candidate.Files {
		if file.Test {
			score += 10
			break
		}
	}
	return score
}
