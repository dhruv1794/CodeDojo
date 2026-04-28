package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

const (
	DefaultBackend    = "mock"
	DefaultDifficulty = 3
)

type Config struct {
	Coach     CoachConfig `mapstructure:"coach"`
	Defaults  Defaults    `mapstructure:"defaults"`
	StorePath string      `mapstructure:"store_path"`
}

type CoachConfig struct {
	Backend string `mapstructure:"backend"`
	APIKey  string `mapstructure:"api_key"`
}

type Defaults struct {
	Difficulty int `mapstructure:"difficulty"`
	HintBudget int `mapstructure:"hint_budget"`
}

func codedojoHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".codedojo"), nil
}

func Default() Config {
	root, err := codedojoHome()
	if err != nil {
		root = ".codedojo"
	}
	return Config{
		Coach: CoachConfig{Backend: DefaultBackend},
		Defaults: Defaults{
			Difficulty: DefaultDifficulty,
			HintBudget: 3,
		},
		StorePath: filepath.Join(root, "codedojo.db"),
	}
}

func DefaultPath() (string, error) {
	root, err := codedojoHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "config.yaml"), nil
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		defaultPath, err := DefaultPath()
		if err != nil {
			return Config{}, err
		}
		path = defaultPath
	}

	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	setDefaults(v, cfg)
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) || errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return Config{}, fmt.Errorf("load config %q: %w", path, err)
	}
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	if path == "" {
		defaultPath, err := DefaultPath()
		if err != nil {
			return err
		}
		path = defaultPath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	v := viper.New()
	v.SetConfigFile(path)
	v.Set("coach.backend", cfg.Coach.Backend)
	v.Set("coach.api_key", cfg.Coach.APIKey)
	v.Set("defaults.difficulty", cfg.Defaults.Difficulty)
	v.Set("defaults.hint_budget", cfg.Defaults.HintBudget)
	v.Set("store_path", cfg.StorePath)
	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("write config %q: %w", path, err)
	}
	return nil
}

func setDefaults(v *viper.Viper, cfg Config) {
	v.SetDefault("coach.backend", cfg.Coach.Backend)
	v.SetDefault("coach.api_key", cfg.Coach.APIKey)
	v.SetDefault("defaults.difficulty", cfg.Defaults.Difficulty)
	v.SetDefault("defaults.hint_budget", cfg.Defaults.HintBudget)
	v.SetDefault("store_path", cfg.StorePath)
}
