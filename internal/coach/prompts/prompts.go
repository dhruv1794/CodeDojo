package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"path"
	"strings"
	"text/template"
)

//go:embed templates/**/*.tmpl
var files embed.FS

func Render(name string, vars any) (string, error) {
	name = strings.TrimPrefix(path.Clean(name), "/")
	templatePath := path.Join("templates", name)
	data, err := files.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("read prompt template %q: %w", name, err)
	}
	tmpl, err := template.New(path.Base(name)).Option("missingkey=error").Parse(string(data))
	if err != nil {
		return "", fmt.Errorf("parse prompt template %q: %w", name, err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, vars); err != nil {
		return "", fmt.Errorf("render prompt template %q: %w", name, err)
	}
	return out.String(), nil
}
