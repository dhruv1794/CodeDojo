package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Language struct {
	Name     string
	TestCmd  []string
	BuildCmd []string
}

type languageOverride struct {
	Language string      `yaml:"language"`
	Name     string      `yaml:"name"`
	TestCmd  commandList `yaml:"test_cmd"`
	BuildCmd commandList `yaml:"build_cmd"`
}

type commandList []string

func DetectLanguage(path string) (Language, error) {
	if override, ok, err := detectOverride(path); ok || err != nil {
		return override, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return Language{}, fmt.Errorf("read repo dir: %w", err)
	}
	names := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		names[entry.Name()] = struct{}{}
	}
	if _, ok := names["go.mod"]; ok {
		return Language{Name: "go", TestCmd: []string{"go", "test", "./..."}, BuildCmd: []string{"go", "build", "./..."}}, nil
	}
	if _, ok := names["Cargo.toml"]; ok {
		return Language{Name: "rust", TestCmd: []string{"cargo", "test"}, BuildCmd: []string{"cargo", "build"}}, nil
	}
	if _, ok := names["package.json"]; ok {
		name := "javascript"
		if hasTypeScriptProject(path, names) {
			name = "typescript"
		}
		return Language{Name: name, TestCmd: []string{"npm", "test"}, BuildCmd: []string{"npm", "run", "build"}}, nil
	}
	if _, ok := names["pyproject.toml"]; ok {
		return Language{Name: "python", TestCmd: []string{"python", "-m", "pytest"}, BuildCmd: []string{"python", "-m", "compileall", "."}}, nil
	}
	if _, ok := names["requirements.txt"]; ok {
		return Language{Name: "python", TestCmd: []string{"python", "-m", "pytest"}, BuildCmd: []string{"python", "-m", "compileall", "."}}, nil
	}
	if _, ok := names["setup.py"]; ok {
		return Language{Name: "python", TestCmd: []string{"python", "-m", "pytest"}, BuildCmd: []string{"python", "-m", "compileall", "."}}, nil
	}
	return Language{Name: "unknown"}, nil
}

func hasTypeScriptProject(path string, names map[string]struct{}) bool {
	if _, ok := names["tsconfig.json"]; ok {
		return true
	}
	data, err := os.ReadFile(filepath.Join(path, "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		Dependencies    map[string]any `json:"dependencies"`
		DevDependencies map[string]any `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}
	_, dep := pkg.Dependencies["typescript"]
	_, devDep := pkg.DevDependencies["typescript"]
	return dep || devDep
}

func detectOverride(path string) (Language, bool, error) {
	data, err := os.ReadFile(filepath.Join(path, ".codedojo.yaml"))
	if os.IsNotExist(err) {
		return Language{}, false, nil
	}
	if err != nil {
		return Language{}, true, fmt.Errorf("read .codedojo.yaml: %w", err)
	}
	var override languageOverride
	if err := yaml.Unmarshal(data, &override); err != nil {
		return Language{}, true, fmt.Errorf("parse .codedojo.yaml: %w", err)
	}
	name := override.Language
	if name == "" {
		name = override.Name
	}
	if name == "" {
		return Language{}, true, fmt.Errorf(".codedojo.yaml must set language or name")
	}
	return Language{Name: name, TestCmd: []string(override.TestCmd), BuildCmd: []string(override.BuildCmd)}, true, nil
}

func (c *commandList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.SequenceNode:
		return value.Decode((*[]string)(c))
	case yaml.ScalarNode:
		*c = strings.Fields(value.Value)
		return nil
	default:
		return fmt.Errorf("command must be a string or string list")
	}
}
