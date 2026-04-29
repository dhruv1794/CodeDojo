package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestServicePreflightDetectsModeAvailability(t *testing.T) {
	ctx := context.Background()
	service := newTestService(t)
	dir := t.TempDir()
	writeServiceFile(t, dir, "go.mod", "module example.com/preflight\n\ngo 1.23\n")
	writeServiceFile(t, dir, "calc/calc.go", "package calc\n\nfunc Clamp(n int) int {\n\tif n < 0 {\n\t\treturn 0\n\t}\n\treturn n\n}\n")
	writeServiceFile(t, dir, "calc/calc_test.go", "package calc\n\nimport \"testing\"\n\nfunc TestClamp(t *testing.T) {\n\tif Clamp(-1) != 0 {\n\t\tt.Fatal(\"bad clamp\")\n\t}\n}\n")
	runServiceGit(t, dir, "init")
	runServiceGit(t, dir, "add", ".")
	runServiceGit(t, dir, "commit", "-m", "initial")

	preflight, err := service.Preflight(ctx, dir)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if preflight.Language != "go" {
		t.Fatalf("Language = %q, want go", preflight.Language)
	}
	if got := strings.Join(preflight.TestCommand, " "); got != "go test ./..." {
		t.Fatalf("TestCommand = %q, want go test ./...", got)
	}
	if !preflight.Review.Available || preflight.Review.CandidateCount == 0 {
		t.Fatalf("Review availability = %+v, want available candidates", preflight.Review)
	}
	if preflight.RepoName != filepath.Base(dir) {
		t.Fatalf("RepoName = %q, want %q", preflight.RepoName, filepath.Base(dir))
	}
}

func TestServicePreflightResponsesAcrossProjectTypes(t *testing.T) {
	ctx := context.Background()
	service := newTestService(t)
	tests := []struct {
		name       string
		files      map[string]string
		wantLang   string
		wantTest   string
		wantReview string
	}{
		{
			name:     "python",
			files:    map[string]string{"pyproject.toml": "[project]\nname = \"demo\"\n"},
			wantLang: "python", wantTest: "python -m pytest", wantReview: "Go AST mutations",
		},
		{
			name:     "javascript",
			files:    map[string]string{"package.json": "{\"scripts\":{\"test\":\"node test.js\"}}\n"},
			wantLang: "javascript", wantTest: "npm test", wantReview: "Go AST mutations",
		},
		{
			name:     "typescript",
			files:    map[string]string{"package.json": "{\"devDependencies\":{\"typescript\":\"latest\"}}\n", "tsconfig.json": "{}\n"},
			wantLang: "typescript", wantTest: "npm test", wantReview: "Go AST mutations",
		},
		{
			name:     "rust",
			files:    map[string]string{"Cargo.toml": "[package]\nname = \"demo\"\nversion = \"0.1.0\"\nedition = \"2021\"\n"},
			wantLang: "rust", wantTest: "cargo test", wantReview: "Go AST mutations",
		},
		{
			name:     "unknown",
			files:    map[string]string{"README.md": "# demo\n"},
			wantLang: "unknown", wantTest: "", wantReview: "supported project marker",
		},
		{
			name:     "override",
			files:    map[string]string{".codedojo.yaml": "language: rust\ntest_cmd: cargo test --all\nbuild_cmd:\n  - cargo\n  - build\n"},
			wantLang: "rust", wantTest: "cargo test --all", wantReview: "Go AST mutations",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for path, data := range tt.files {
				writeServiceFile(t, dir, path, data)
			}
			runServiceGit(t, dir, "init")
			runServiceGit(t, dir, "add", ".")
			runServiceGit(t, dir, "commit", "-m", "initial")

			preflight, err := service.Preflight(ctx, dir)
			if err != nil {
				t.Fatalf("preflight: %v", err)
			}
			if preflight.Language != tt.wantLang {
				t.Fatalf("Language = %q, want %q", preflight.Language, tt.wantLang)
			}
			if got := strings.Join(preflight.TestCommand, " "); got != tt.wantTest {
				t.Fatalf("TestCommand = %q, want %q", got, tt.wantTest)
			}
			if preflight.Review.Available {
				t.Fatalf("Review = %+v, want unavailable", preflight.Review)
			}
			if !strings.Contains(preflight.Review.Reason, tt.wantReview) {
				t.Fatalf("Review reason = %q, want to contain %q", preflight.Review.Reason, tt.wantReview)
			}
			if !preflight.Review.Available && len(preflight.Review.Actions) == 0 {
				t.Fatalf("Review actions are empty for unavailable mode: %+v", preflight.Review)
			}
		})
	}
}

func TestServicePreflightIncludesActionableUnavailableReasons(t *testing.T) {
	ctx := context.Background()
	service := newTestService(t)
	dir := t.TempDir()
	writeServiceFile(t, dir, "go.mod", "module example.com/preflight\n\ngo 1.23\n")
	runServiceGit(t, dir, "init")
	runServiceGit(t, dir, "add", ".")
	runServiceGit(t, dir, "commit", "-m", "initial")

	preflight, err := service.Preflight(ctx, dir)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if preflight.Learn.Available {
		t.Fatalf("Learn = %+v, want unavailable", preflight.Learn)
	}
	if len(preflight.Learn.Actions) == 0 {
		t.Fatalf("Learn actions are empty: %+v", preflight.Learn)
	}
	if preflight.Review.Available {
		t.Fatalf("Review = %+v, want unavailable", preflight.Review)
	}
	if len(preflight.Review.Actions) == 0 {
		t.Fatalf("Review actions are empty: %+v", preflight.Review)
	}
}
