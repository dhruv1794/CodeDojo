package op

import (
	"fmt"
	"sort"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
)

func All() []mutate.Mutator {
	return []mutate.Mutator{
		Boundary{},
		Conditional{},
		ErrorDrop{},
		SliceBounds{},
	}
}

func ByName(name string) (mutate.Mutator, bool) {
	for _, mutator := range All() {
		if mutator.Name() == name {
			return mutator, true
		}
	}
	return nil, false
}

func MustByName(name string) mutate.Mutator {
	mutator, ok := ByName(name)
	if !ok {
		panic(fmt.Sprintf("unknown reviewer mutator %q", name))
	}
	return mutator
}

func ByDifficulty(difficulty int) []mutate.Mutator {
	var out []mutate.Mutator
	for _, mutator := range All() {
		if mutator.Difficulty() == difficulty {
			out = append(out, mutator)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name() < out[j].Name()
	})
	return out
}
