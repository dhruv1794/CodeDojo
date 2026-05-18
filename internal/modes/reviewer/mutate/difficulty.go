// SPDX-License-Identifier: MIT

package mutate

import "strings"

type Profile struct {
	Locality  ProfileAxis `json:"locality"`
	Subtlety  ProfileAxis `json:"subtlety"`
	Knowledge ProfileAxis `json:"knowledge"`
	Summary   string      `json:"summary"`
}

type ProfileAxis struct {
	Score  int    `json:"score"`
	Reason string `json:"reason"`
}

func ProfileDifficulty(m Mutation) Profile {
	locality := profileLocality(m)
	subtlety := profileSubtlety(m)
	knowledge := profileKnowledge(m)
	return Profile{
		Locality:  locality,
		Subtlety:  subtlety,
		Knowledge: knowledge,
		Summary:   profileSummary(locality.Score, subtlety.Score, knowledge.Score),
	}
}

func profileLocality(m Mutation) ProfileAxis {
	span := m.EndLine - m.StartLine + 1
	if span < 1 {
		span = 1
	}
	switch {
	case span == 1:
		return ProfileAxis{Score: 1, Reason: "single-line mutation"}
	case span <= 4:
		return ProfileAxis{Score: 2, Reason: "small local line range"}
	default:
		return ProfileAxis{Score: 3, Reason: "wider local line range"}
	}
}

func profileSubtlety(m Mutation) ProfileAxis {
	operator := strings.ToLower(m.Operator)
	description := strings.ToLower(m.Description)
	switch {
	case strings.Contains(operator, "race") || strings.Contains(operator, "lock"):
		return ProfileAxis{Score: 3, Reason: "concurrency bugs can stay quiet until operations interleave"}
	case strings.Contains(operator, "coerc") || strings.Contains(operator, "strict-equality"):
		return ProfileAxis{Score: 3, Reason: "coercion behavior can stay quiet until mixed-type inputs appear"}
	case strings.Contains(operator, "error") || strings.Contains(operator, "except") || strings.Contains(operator, "async"):
		return ProfileAxis{Score: 3, Reason: "error-path behavior can stay quiet until failure handling is exercised"}
	case strings.Contains(operator, "optional") || strings.Contains(operator, "guard") || strings.Contains(operator, "conditional") || strings.Contains(description, "condition"):
		return ProfileAxis{Score: 2, Reason: "control-flow behavior changes without changing surrounding structure"}
	default:
		return ProfileAxis{Score: 1, Reason: "localized operator change is usually visible in a focused diff"}
	}
}

func profileKnowledge(m Mutation) ProfileAxis {
	operator := strings.ToLower(m.Operator)
	switch {
	case strings.Contains(operator, "async"):
		return ProfileAxis{Score: 3, Reason: "requires async error-flow knowledge"}
	case strings.Contains(operator, "race") || strings.Contains(operator, "lock"):
		return ProfileAxis{Score: 3, Reason: "requires concurrency and synchronization knowledge"}
	case strings.Contains(operator, "coerc") || strings.Contains(operator, "strict-equality"):
		return ProfileAxis{Score: 3, Reason: "requires language coercion semantics"}
	case strings.Contains(operator, "error") || strings.Contains(operator, "except") || strings.Contains(operator, "err"):
		return ProfileAxis{Score: 3, Reason: "requires language error-handling knowledge"}
	case strings.Contains(operator, "optional") || strings.Contains(operator, "guard") || strings.Contains(operator, "option"):
		return ProfileAxis{Score: 2, Reason: "requires type or nilability semantics"}
	case strings.Contains(operator, "slice") || strings.Contains(operator, "array") || strings.Contains(operator, "range") || strings.Contains(operator, "pagination") || strings.Contains(operator, "window"):
		return ProfileAxis{Score: 2, Reason: "requires collection boundary semantics"}
	default:
		return ProfileAxis{Score: 1, Reason: "requires basic expression and comparison semantics"}
	}
}

func profileSummary(scores ...int) string {
	total := 0
	for _, score := range scores {
		total += score
	}
	avg := float64(total) / float64(len(scores))
	switch {
	case avg >= 2.6:
		return "black belt"
	case avg >= 2.1:
		return "brown belt"
	case avg >= 1.6:
		return "green belt"
	case avg >= 1.2:
		return "yellow belt"
	default:
		return "white belt"
	}
}
