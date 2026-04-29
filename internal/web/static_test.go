package web

import (
	"strings"
	"testing"
)

func TestStaticAssetsKeepDemoPolishHooks(t *testing.T) {
	index := readStaticAsset(t, "static/index.html")
	styles := readStaticAsset(t, "static/styles.css")
	app := readStaticAsset(t, "static/app.js")

	for _, want := range []string{
		`<meta name="viewport"`,
		`id="hero-scan-state"`,
		`id="start-button"`,
		`id="timer-label"`,
		`id="progress-label"`,
	} {
		if !strings.Contains(index, want) {
			t.Fatalf("index.html missing %q", want)
		}
	}
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
		"function setBusy(",
		"function startTimer(",
		`event.altKey && ["1", "2", "3", "4", "5"]`,
		`event.key === "Enter"`,
	} {
		if !strings.Contains(app, want) {
			t.Fatalf("app.js missing %q", want)
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
