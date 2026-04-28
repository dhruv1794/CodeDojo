package session

import "fmt"

var transitions = map[State][]State{
	StateCreated:   {StateRunning, StateClosed},
	StateRunning:   {StateSubmitted, StateClosed},
	StateSubmitted: {StateGraded, StateClosed},
	StateGraded:    {StateClosed},
	StateClosed:    {},
}

func CanTransition(from, to State) bool {
	for _, allowed := range transitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

func Transition(from, to State) error {
	if !CanTransition(from, to) {
		return fmt.Errorf("invalid session transition %s -> %s", from, to)
	}
	return nil
}
