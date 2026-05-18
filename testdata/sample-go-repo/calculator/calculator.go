package calculator

import "fmt"

func Divide(total, parts int) (int, error) {
	if parts == 0 {
		return 0, fmt.Errorf("parts must not be zero")
	}
	return total / parts, nil
}

func Clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
