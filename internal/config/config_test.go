package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	want := Default()
	if cfg != want {
		t.Fatalf("Load() = %+v, want defaults %+v", cfg, want)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	want := Config{
		Coach: CoachConfig{
			Backend: "anthropic",
			APIKey:  "sk-test",
		},
		Defaults: Defaults{
			Difficulty: 5,
			HintBudget: 7,
		},
		StorePath: filepath.Join(t.TempDir(), "codedojo.db"),
	}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

func TestLoadMalformedFileReturnsClearError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("coach:\n  backend: [mock\n"), 0o644); err != nil {
		t.Fatalf("write malformed config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load returned nil error for malformed config")
	}

	msg := err.Error()
	if !strings.Contains(msg, "load config") || !strings.Contains(msg, path) {
		t.Fatalf("Load error = %q, want clear config path context", msg)
	}
}
