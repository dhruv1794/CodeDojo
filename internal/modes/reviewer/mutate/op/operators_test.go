// SPDX-License-Identifier: MIT

package op

import (
	"bytes"
	"go/parser"
	"go/printer"
	"go/token"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
)

func TestBoundaryCandidatesAndApply(t *testing.T) {
	source := `package p

func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	if value == 0 {
		return 0
	}
	return value
}
`
	want := `package p

func clamp(value, min, max int) int {
	if value <= min {
		return min
	}
	if value > max {
		return max
	}
	if value == 0 {
		return 0
	}
	return value
}
`
	assertMutator(t, Boundary{}, source, want, 2)
}

func TestConditionalCandidatesAndApply(t *testing.T) {
	source := `package p

func load() error {
	if err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	if value == 0 {
		return nil
	}
	return nil
}
`
	want := `package p

func load() error {
	if err == nil {
		return err
	}
	if result == nil {
		return nil
	}
	if value == 0 {
		return nil
	}
	return nil
}
`
	assertMutator(t, Conditional{}, source, want, 2)
}

func TestErrorDropCandidatesAndApply(t *testing.T) {
	source := `package p

func save() error {
	if err != nil {
		return err
	}
	if other != nil {
		return nil
	}
	return nil
}
`
	want := `package p

func save() error {

	if other != nil {
		return nil
	}
	return nil
}
`
	assertMutator(t, ErrorDrop{}, source, want, 1)
}

func TestSliceBoundsCandidatesAndApply(t *testing.T) {
	source := `package p

func split(values []int, i int) ([]int, []int, []int) {
	left := values[:i]
	right := values[i:]
	window := values[i : i+1]
	return left, right, window
}
`
	want := `package p

func split(values []int, i int) ([]int, []int, []int) {
	left := values[i:]
	right := values[i:]
	window := values[i : i+1]
	return left, right, window
}
`
	assertMutator(t, SliceBounds{}, source, want, 2)
}

func TestPaginationWindowCandidatesAndApply(t *testing.T) {
	source := `package p

func page(values []int, offset, limit int) []int {
	window := values[offset : offset+limit]
	all := values[0:len(values)]
	tail := values[offset:]
	return append(window, all[:len(tail)]...)
}
`
	want := `package p

func page(values []int, offset, limit int) []int {
	window := values[offset : (offset+limit)-1]
	all := values[0:len(values)]
	tail := values[offset:]
	return append(window, all[:len(tail)]...)
}
`
	assertMutator(t, PaginationWindow{}, source, want, 1)
}

func TestRaceLockDropCandidatesAndApply(t *testing.T) {
	source := `package p

type Counter struct {
	mu    Mutex
	count int
}

func (c *Counter) Inc() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++
}

func (c *Counter) Read() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.count
}
`
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "input.go", source, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse input: %v", err)
	}
	sites := RaceLockDrop{}.Candidates(file)
	if len(sites) != 2 {
		t.Fatalf("Candidates length = %d, want 2", len(sites))
	}
	mutation, err := (RaceLockDrop{}).Apply(file, sites[0])
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if mutation.Operator != "race-lock-drop" {
		t.Fatalf("operator = %q, want race-lock-drop", mutation.Operator)
	}
	got := formatFile(t, fileSet, file)
	if strings.Contains(got, "c.mu.Lock()") || strings.Contains(got, "defer c.mu.Unlock()") {
		t.Fatalf("Inc lock pair was not removed:\n%s", got)
	}
	if !strings.Contains(got, "c.count++") || !strings.Contains(got, "c.mu.RLock()") || !strings.Contains(got, "defer c.mu.RUnlock()") {
		t.Fatalf("mutation removed unrelated statements:\n%s", got)
	}
}

func TestRegistry(t *testing.T) {
	all := All()
	gotNames := make([]string, 0, len(all))
	for _, mutator := range all {
		gotNames = append(gotNames, mutator.Name())
		if mutator.Difficulty() < 1 {
			t.Fatalf("%s difficulty = %d, want positive", mutator.Name(), mutator.Difficulty())
		}
	}
	slices.Sort(gotNames)
	wantNames := []string{"boundary", "conditional", "errordrop", "pagination-window", "race-lock-drop", "slicebounds"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("All names = %#v, want %#v", gotNames, wantNames)
	}

	if mutator, ok := ByName("conditional"); !ok || mutator.Name() != "conditional" {
		t.Fatalf("ByName conditional = (%v, %v), want conditional", mutator, ok)
	}
	if _, ok := ByName("missing"); ok {
		t.Fatalf("ByName missing returned ok")
	}

	gotDifficultyTwo := names(ByDifficulty(2))
	wantDifficultyTwo := []string{"conditional", "slicebounds"}
	if !reflect.DeepEqual(gotDifficultyTwo, wantDifficultyTwo) {
		t.Fatalf("ByDifficulty(2) = %#v, want %#v", gotDifficultyTwo, wantDifficultyTwo)
	}
	gotDifficultyThree := names(ByDifficulty(3))
	wantDifficultyThree := []string{"errordrop", "pagination-window"}
	if !reflect.DeepEqual(gotDifficultyThree, wantDifficultyThree) {
		t.Fatalf("ByDifficulty(3) = %#v, want %#v", gotDifficultyThree, wantDifficultyThree)
	}
	gotDifficultyFour := names(ByDifficulty(4))
	wantDifficultyFour := []string{"race-lock-drop"}
	if !reflect.DeepEqual(gotDifficultyFour, wantDifficultyFour) {
		t.Fatalf("ByDifficulty(4) = %#v, want %#v", gotDifficultyFour, wantDifficultyFour)
	}
}

func assertMutator(t *testing.T, mutator mutate.Mutator, source, want string, wantCandidates int) {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "input.go", source, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse input: %v", err)
	}
	sites := mutator.Candidates(file)
	if len(sites) != wantCandidates {
		t.Fatalf("Candidates length = %d, want %d", len(sites), wantCandidates)
	}
	if len(sites) == 0 {
		t.Fatalf("mutator returned no candidates")
	}
	if sites[0].Node == nil {
		t.Fatalf("first candidate node is nil")
	}
	mutation, err := mutator.Apply(file, sites[0])
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if mutation.Operator != mutator.Name() {
		t.Fatalf("mutation operator = %q, want %q", mutation.Operator, mutator.Name())
	}
	if mutation.Difficulty != mutator.Difficulty() {
		t.Fatalf("mutation difficulty = %d, want %d", mutation.Difficulty, mutator.Difficulty())
	}
	got := formatFile(t, fileSet, file)
	if strings.TrimSpace(got) != strings.TrimSpace(want) {
		t.Fatalf("mutated source mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func formatFile(t *testing.T, fileSet *token.FileSet, file any) string {
	t.Helper()
	var out bytes.Buffer
	if err := printer.Fprint(&out, fileSet, file); err != nil {
		t.Fatalf("format file: %v", err)
	}
	return out.String()
}

func names(mutators []mutate.Mutator) []string {
	out := make([]string, 0, len(mutators))
	for _, mutator := range mutators {
		out = append(out, mutator.Name())
	}
	return out
}
