package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticAssetsEmbedReactBuild(t *testing.T) {
	index := readStaticAsset(t, "static/index.html")

	for _, want := range []string{
		`<meta name="viewport"`,
		`id="root"`,
		`type="module"`,
		`/assets/`,
	} {
		if !strings.Contains(index, want) {
			t.Fatalf("index.html missing %q", want)
		}
	}
}

func TestReactSourceKeepsDemoPolishHooks(t *testing.T) {
	styles := readRepoFile(t, "web", "src", "styles.css")
	app := readRepoFile(t, "web", "src", "main.jsx")

	for _, want := range []string{
		"@media (min-width: 981px) and (max-width: 1260px)",
		"@media (max-width: 980px)",
		"@media (max-width: 640px)",
		".setup-hero",
		".is-busy",
	} {
		if !strings.Contains(styles, want) {
			t.Fatalf("styles.css missing %q", want)
		}
	}
	for _, want := range []string{
		`id="hero-scan-state"`,
		`id="start-button"`,
		`id="timer-label"`,
		`id="progress-label"`,
		`@monaco-editor/react`,
	} {
		if !strings.Contains(app, want) {
			t.Fatalf("main.jsx missing %q", want)
		}
	}
}

func readStaticAsset(t *testing.T, name string) string {
	t.Helper()
	data, err := staticFS.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func readRepoFile(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{"..", ".."}, parts...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
