package prompts

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRenderReviewerSystemPrompt(t *testing.T) {
	t.Parallel()

	got, err := Render("reviewer/system.tmpl", map[string]any{
		"Difficulty": 3,
		"HintBudget": 3,
		"Level":      "pointer",
		"Strict":     true,
		"Context":    "one failing test",
		"Language":   "Go",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	for _, want := range []string{"CodeDojo reviewer coach", "Difficulty: 3", "Strict: true", "one failing test"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered prompt %q does not contain %q", got, want)
		}
	}
}

func TestRenderMissingVariableReturnsClearError(t *testing.T) {
	t.Parallel()

	_, err := Render("reviewer/grade_diagnosis.tmpl", map[string]any{"MaxScore": 50})
	if err == nil {
		t.Fatal("Render() error = nil, want missing key error")
	}
	if !strings.Contains(err.Error(), "render prompt template") {
		t.Fatalf("Render() error = %v, want render context", err)
	}
}

func TestEmbeddedReviewerPromptsMatchConfigPrompts(t *testing.T) {
	t.Parallel()

	for _, prompt := range []struct {
		dir  string
		name string
	}{
		{dir: "reviewer", name: "system.tmpl"},
		{dir: "reviewer", name: "grade_diagnosis.tmpl"},
		{dir: "newcomer", name: "summarize.tmpl"},
	} {
		configPrompt := readRepoFile(t, "configs", "prompts", prompt.dir, prompt.name)
		embeddedPrompt := readRepoFile(t, "internal", "coach", "prompts", "templates", prompt.dir, prompt.name)
		if configPrompt != embeddedPrompt {
			t.Fatalf("%s/%s differs between configs/prompts and embedded prompt copy", prompt.dir, prompt.name)
		}
	}
}

func readRepoFile(t *testing.T, elems ...string) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", ".."))
	path := filepath.Join(append([]string{root}, elems...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
