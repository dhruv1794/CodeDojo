package cli

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/dhruvmishra/codedojo/internal/config"
	"github.com/spf13/cobra"
)

func newInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Configure CodeDojo for first use",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := promptForConfig(cmd, config.Default())
			if err != nil {
				return err
			}
			if err := config.Save(cfgFile, cfg); err != nil {
				return err
			}
			path := cfgFile
			if path == "" {
				var err error
				path, err = config.DefaultPath()
				if err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", path)
			return nil
		},
	}
}

func promptForConfig(cmd *cobra.Command, cfg config.Config) (config.Config, error) {
	in := bufio.NewReader(cmd.InOrStdin())
	out := cmd.OutOrStdout()

	backend, err := promptChoice(in, out, "LLM backend", cfg.Coach.Backend, []string{"mock", "anthropic", "ollama"})
	if err != nil {
		return config.Config{}, err
	}
	cfg.Coach.Backend = backend

	apiKeyDefault := cfg.Coach.APIKey
	if backend == "anthropic" {
		apiKey, err := promptString(in, out, "Anthropic API key", apiKeyDefault)
		if err != nil {
			return config.Config{}, err
		}
		cfg.Coach.APIKey = apiKey
	} else {
		cfg.Coach.APIKey = ""
	}

	difficulty, err := promptInt(in, out, "Default difficulty", cfg.Defaults.Difficulty, 1, 5)
	if err != nil {
		return config.Config{}, err
	}
	cfg.Defaults.Difficulty = difficulty

	return cfg, nil
}

func promptChoice(in *bufio.Reader, out io.Writer, label, def string, allowed []string) (string, error) {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, value := range allowed {
		allowedSet[value] = struct{}{}
	}
	for {
		value, err := promptString(in, out, fmt.Sprintf("%s (%s)", label, strings.Join(allowed, "/")), def)
		if err != nil {
			return "", err
		}
		value = strings.ToLower(value)
		if _, ok := allowedSet[value]; ok {
			return value, nil
		}
		if _, err := fmt.Fprintf(out, "choose one of: %s\n", strings.Join(allowed, ", ")); err != nil {
			return "", err
		}
	}
}

func promptInt(in *bufio.Reader, out io.Writer, label string, def, minValue, maxValue int) (int, error) {
	for {
		value, err := promptString(in, out, label, strconv.Itoa(def))
		if err != nil {
			return 0, err
		}
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed >= minValue && parsed <= maxValue {
			return parsed, nil
		}
		if _, err := fmt.Fprintf(out, "enter a number from %d to %d\n", minValue, maxValue); err != nil {
			return 0, err
		}
	}
}

func promptString(in *bufio.Reader, out io.Writer, label, def string) (string, error) {
	if def == "" {
		if _, err := fmt.Fprintf(out, "%s: ", label); err != nil {
			return "", err
		}
	} else if _, err := fmt.Fprintf(out, "%s [%s]: ", label, def); err != nil {
		return "", err
	}
	line, err := in.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	value := strings.TrimSpace(line)
	if value == "" {
		return def, nil
	}
	return value, nil
}
