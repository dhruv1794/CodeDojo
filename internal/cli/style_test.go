package cli

import (
	"strings"
	"testing"
)

func TestCLIThemeColorAndNoColor(t *testing.T) {
	color := newCLITheme(true)
	if got := color.Banner("Reviewer mode"); !strings.Contains(got, "\x1b[") {
		t.Fatalf("colored banner does not contain ANSI sequence: %q", got)
	}
	if got := color.Prompt("review"); !strings.Contains(got, "\x1b[") || !strings.Contains(got, "codedojo(review)> ") {
		t.Fatalf("colored prompt = %q, want ANSI and prompt text", got)
	}

	plain := newCLITheme(false)
	if got := plain.Banner("Reviewer mode"); got != "== Reviewer mode ==" {
		t.Fatalf("plain banner = %q", got)
	}
	if got := plain.Prompt("review"); got != "codedojo(review)> " {
		t.Fatalf("plain prompt = %q", got)
	}
}

func TestCLIThemeCommandListKeepsPlainTextCommands(t *testing.T) {
	got := newCLITheme(false).CommandList("help", "tests", "quit")
	for _, want := range []string{
		"commands:",
		"  help",
		"  tests",
		"  quit",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("command list missing %q:\n%s", want, got)
		}
	}
}
