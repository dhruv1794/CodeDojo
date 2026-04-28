package session

import "time"

func TimeBonus(elapsed time.Duration, target time.Duration) int {
	if target <= 0 || elapsed >= target {
		return 0
	}
	remaining := target - elapsed
	return int((remaining * 25) / target)
}

func ApplyStreak(score, streak int) int {
	if streak <= 1 {
		return score
	}
	return score + (score * min(streak, 5) / 20)
}
