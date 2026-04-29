package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dhruvmishra/codedojo/internal/config"
	"github.com/spf13/cobra"
)

func TestPromptForConfigUsesDefaults(t *testing.T) {
	cmd := &cobra.Command{}
	in := strings.NewReader("\n\n")
	out := &bytes.Buffer{}
	cmd.SetIn(in)
	cmd.SetOut(out)

	cfg, err := promptForConfig(cmd, config.Default())
	if err != nil {
		t.Fatalf("promptForConfig returned error: %v", err)
	}

	if cfg.Coach.Backend != "mock" {
		t.Fatalf("backend = %q, want mock", cfg.Coach.Backend)
	}
	if cfg.Coach.APIKey != "" {
		t.Fatalf("api key = %q, want empty", cfg.Coach.APIKey)
	}
	if cfg.Defaults.Difficulty != config.DefaultDifficulty {
		t.Fatalf("difficulty = %d, want %d", cfg.Defaults.Difficulty, config.DefaultDifficulty)
	}
}

func TestPromptForConfigAnthropic(t *testing.T) {
	cmd := &cobra.Command{}
	in := strings.NewReader("anthropic\nsk-test\n5\n")
	out := &bytes.Buffer{}
	cmd.SetIn(in)
	cmd.SetOut(out)

	cfg, err := promptForConfig(cmd, config.Default())
	if err != nil {
		t.Fatalf("promptForConfig returned error: %v", err)
	}

	if cfg.Coach.Backend != "anthropic" {
		t.Fatalf("backend = %q, want anthropic", cfg.Coach.Backend)
	}
	if cfg.Coach.APIKey != "sk-test" {
		t.Fatalf("api key = %q, want sk-test", cfg.Coach.APIKey)
	}
	if cfg.Defaults.Difficulty != 5 {
		t.Fatalf("difficulty = %d, want 5", cfg.Defaults.Difficulty)
	}
}

func TestPromptForConfigRepromptsInvalidValues(t *testing.T) {
	cmd := &cobra.Command{}
	in := strings.NewReader("bogus\nmock\n8\n2\n")
	out := &bytes.Buffer{}
	cmd.SetIn(in)
	cmd.SetOut(out)

	cfg, err := promptForConfig(cmd, config.Default())
	if err != nil {
		t.Fatalf("promptForConfig returned error: %v", err)
	}

	if cfg.Coach.Backend != "mock" {
		t.Fatalf("backend = %q, want mock", cfg.Coach.Backend)
	}
	if cfg.Defaults.Difficulty != 2 {
		t.Fatalf("difficulty = %d, want 2", cfg.Defaults.Difficulty)
	}
	if !strings.Contains(out.String(), "choose one of") {
		t.Fatalf("expected invalid backend warning in output:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "enter a number from 1 to 5") {
		t.Fatalf("expected invalid difficulty warning in output:\n%s", out.String())
	}
}

func TestPromptForConfigOllama(t *testing.T) {
	cmd := &cobra.Command{}
	in := strings.NewReader("ollama\n4\n")
	out := &bytes.Buffer{}
	cmd.SetIn(in)
	cmd.SetOut(out)

	cfg, err := promptForConfig(cmd, config.Default())
	if err != nil {
		t.Fatalf("promptForConfig returned error: %v", err)
	}

	if cfg.Coach.Backend != "ollama" {
		t.Fatalf("backend = %q, want ollama", cfg.Coach.Backend)
	}
	if cfg.Coach.APIKey != "" {
		t.Fatalf("api key = %q, want empty", cfg.Coach.APIKey)
	}
	if cfg.Defaults.Difficulty != 4 {
		t.Fatalf("difficulty = %d, want 4", cfg.Defaults.Difficulty)
	}
}

func TestInitCommandWritesConfig(t *testing.T) {
	t.Cleanup(func() { cfgFile = "" })
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfgFile = path

	cmd := newInitCommand()
	cmd.SetIn(strings.NewReader("anthropic\nsk-test\n4\n"))
	out := &bytes.Buffer{}
	cmd.SetOut(out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init command returned error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config file at %s: %v", path, err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	if cfg.Coach.Backend != "anthropic" || cfg.Coach.APIKey != "sk-test" || cfg.Defaults.Difficulty != 4 {
		t.Fatalf("written config mismatch: %+v", cfg)
	}
}
