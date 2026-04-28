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
	wantNames := []string{"boundary", "conditional", "errordrop", "slicebounds"}
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
