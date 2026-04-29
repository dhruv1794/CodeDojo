package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type cliTheme struct {
	color bool

	bannerStyle    lipgloss.Style
	separatorStyle lipgloss.Style
	promptStyle    lipgloss.Style
	labelStyle     lipgloss.Style
	successStyle   lipgloss.Style
	warnStyle      lipgloss.Style
	hintStyle      lipgloss.Style
	mutedStyle     lipgloss.Style
	scoreStyle     lipgloss.Style
}

func themeFor(cmd *cobra.Command) cliTheme {
	out := cmd.OutOrStdout()
	color := !noColor && isTerminalWriter(out)
	return newCLITheme(color)
}

func themeForWriter(out io.Writer) cliTheme {
	color := !noColor && isTerminalWriter(out)
	return newCLITheme(color)
}

func newCLITheme(color bool) cliTheme {
	t := cliTheme{color: color}
	if !color {
		return t
	}
	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(termenv.ANSI256)
	renderer.SetHasDarkBackground(true)
	t.bannerStyle = renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	t.separatorStyle = renderer.NewStyle().Foreground(lipgloss.Color("240"))
	t.promptStyle = renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	t.labelStyle = renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	t.successStyle = renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	t.warnStyle = renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	t.hintStyle = renderer.NewStyle().Foreground(lipgloss.Color("213"))
	t.mutedStyle = renderer.NewStyle().Foreground(lipgloss.Color("244"))
	t.scoreStyle = renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("118"))
	return t
}

func isTerminalWriter(out io.Writer) bool {
	file, ok := out.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func (t cliTheme) Banner(title string) string {
	text := "== " + title + " =="
	if !t.color {
		return text
	}
	return t.bannerStyle.Render(text)
}

func (t cliTheme) Prompt(mode string) string {
	text := fmt.Sprintf("codedojo(%s)> ", mode)
	if !t.color {
		return text
	}
	return t.promptStyle.Render(text)
}

func (t cliTheme) Box(title, body string) string {
	body = strings.TrimRight(body, "\n")
	if !t.color {
		if title == "" {
			return body + "\n"
		}
		return fmt.Sprintf("--- %s ---\n%s\n--- end %s ---\n", title, body, title)
	}
	separator := t.separatorStyle.Render(strings.Repeat("-", 72))
	if title != "" {
		return fmt.Sprintf("%s\n%s\n%s\n%s\n", t.Label(title), separator, body, separator)
	}
	return fmt.Sprintf("%s\n%s\n%s\n", separator, body, separator)
}

func (t cliTheme) Label(text string) string {
	text += ":"
	if !t.color {
		return text
	}
	return t.labelStyle.Render(text)
}

func (t cliTheme) Success(text string) string {
	if !t.color {
		return text
	}
	return t.successStyle.Render(text)
}

func (t cliTheme) Warning(text string) string {
	if !t.color {
		return text
	}
	return t.warnStyle.Render(text)
}

func (t cliTheme) Hint(text string) string {
	if !t.color {
		return text
	}
	return t.hintStyle.Render(text)
}

func (t cliTheme) Muted(text string) string {
	if !t.color {
		return text
	}
	return t.mutedStyle.Render(text)
}

func (t cliTheme) Score(text string) string {
	if !t.color {
		return text
	}
	return t.scoreStyle.Render(text)
}

func (t cliTheme) CommandList(lines ...string) string {
	var b strings.Builder
	b.WriteString(t.Label("commands"))
	b.WriteByte('\n')
	for _, line := range lines {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
