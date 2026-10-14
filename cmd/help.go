package cmd

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/charmbracelet/lipgloss"
)

// Repeated string literals consolidated by goconst.
const (
	commands1 = "commands"
	options1  = "options"
	usage1    = "usage"
)

type helpStyles struct {
	section  lipgloss.Style
	command  lipgloss.Style
	flag     lipgloss.Style
	syntax   lipgloss.Style
	hint     lipgloss.Style
	errorBox lipgloss.Style
}

func newHelpStyles(renderer *lipgloss.Renderer) helpStyles {
	return helpStyles{
		section: renderer.NewStyle().Foreground(lipgloss.Color("33")).Bold(true),
		command: renderer.NewStyle().Foreground(lipgloss.Color("33")).Bold(true),
		flag:    renderer.NewStyle().Foreground(lipgloss.Color("33")),
		syntax:  renderer.NewStyle().Faint(true),
		hint:    renderer.NewStyle().Faint(true),
		errorBox: renderer.NewStyle().
			Foreground(lipgloss.Color("#FF4B4B")).
			BorderForeground(lipgloss.Color("#FF4B4B")).
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1).
			Bold(true),
	}
}

func llamboHelp(options kong.HelpOptions, ctx *kong.Context) error {
	var rendered bytes.Buffer
	stdout := ctx.Stdout
	ctx.Stdout = &rendered
	if err := kong.DefaultHelpPrinter(options, ctx); err != nil {
		ctx.Stdout = stdout
		return err
	}
	ctx.Stdout = stdout

	help := detailedHelp(rendered.String(), ctx.Command())
	if ctx.Selected() == nil {
		help = polishRootHelp(help)
	}
	_, err := fmt.Fprint(stdout, styleHelp(help, ctx, newHelpStyles(lipgloss.NewRenderer(stdout))))
	return err
}

// WriteError prints one concise, terminal-aware error badge without usage text.
func WriteError(w io.Writer, err error) error {
	styles := newHelpStyles(lipgloss.NewRenderer(w))
	_, writeErr := fmt.Fprintln(w, styles.errorBox.Render("Error: "+err.Error()))
	return writeErr
}

// detailedHelp replaces the rendered Kong template with curated fragments for
// commands whose examples and contracts need hand-authored copy.
func detailedHelp(rendered, command string) string {
	help := normalizeRenderedHelp(rendered)
	override, ok := curatedCommandHelp[command]
	if !ok {
		return help
	}
	return override.prefix + help + override.appendix
}

func polishRootHelp(help string) string {
	lines := strings.Split(strings.TrimSuffix(help, "\n"), "\n")
	out := make([]string, 0, len(lines)+1)
	section := ""
	for _, line := range lines {
		switch line {
		case "Commands:":
			section = commands1
		case "Flags:":
			section = options1
			line = "Options:"
		}
		if line == `Run "llambo <command> --help" for more information on a command.` {
			line = `Run "llambo <command> --help" for command details.`
		}
		if section == options1 && strings.HasPrefix(strings.TrimSpace(line), "-h, --help") {
			out = append(out,
				"  -h, --help             Show context-sensitive help.",
				"  -v, --version          Print version information.",
			)
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n") + "\n"
}

func styleHelp(help string, ctx *kong.Context, styles helpStyles) string {
	section := ""
	lines := strings.Split(help, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if section == usage1 {
				section = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "Usage: ") {
			lines[i] = styleUsage(line, ctx, styles)
			section = usage1
			continue
		}
		if line == trimmed && strings.HasSuffix(trimmed, ":") {
			heading := strings.TrimSuffix(strings.ToLower(trimmed), ":")
			switch heading {
			case usage1, commands1, "flags", options1, "arguments":
				section = heading
			default:
				section = ""
			}
			lines[i] = leadingSpace(line) + styles.section.Render(trimmed)
			continue
		}
		if strings.HasPrefix(trimmed, "Run \"") {
			lines[i] = leadingSpace(line) + styles.hint.Render(trimmed)
			continue
		}
		switch section {
		case usage1:
			lines[i] = styleUsageSyntax(line, ctx, styles)
		case commands1:
			lines[i] = styleColumns(line, styles.command, styles.syntax)
		case "flags", options1:
			lines[i] = styleFlagColumns(line, styles)
		case "arguments":
			lines[i] = styleColumns(line, styles.syntax, styles.syntax)
		}
	}
	return strings.Join(lines, "\n")
}

func styleUsage(line string, ctx *kong.Context, styles helpStyles) string {
	indent := leadingSpace(line)
	trimmed := strings.TrimSpace(line)
	path := displayCommandPath(ctx)
	syntax := strings.TrimPrefix(strings.TrimPrefix(trimmed, "Usage: "), path)
	return indent + styles.section.Render("Usage:") + " " + styles.command.Render(path) + styles.syntax.Render(syntax)
}

func styleUsageSyntax(line string, ctx *kong.Context, styles helpStyles) string {
	indent := leadingSpace(line)
	trimmed := strings.TrimSpace(line)
	path := displayCommandPath(ctx)
	if strings.HasPrefix(trimmed, path) {
		return indent + styles.command.Render(path) + styles.syntax.Render(strings.TrimPrefix(trimmed, path))
	}
	return indent + styles.syntax.Render(line)
}

func displayCommandPath(ctx *kong.Context) string {
	path := ctx.Model.Name
	if selected := ctx.Selected(); selected != nil {
		path = selected.FullPath()
	}
	switch ctx.Command() {
	case "models list":
		return "llambo models"
	case "models catalog list":
		return "llambo models catalog [provider]"
	case "providers list":
		return "llambo providers"
	case "route query":
		return "llambo route <query>"
	default:
		return path
	}
}

func styleColumns(line string, leftStyle, syntaxStyle lipgloss.Style) string {
	indent := leadingSpace(line)
	left, right, ok := splitColumns(strings.TrimLeft(line, " "))
	if !ok {
		return line
	}
	gap := strings.Repeat(" ", len(left)-len(strings.TrimRight(left, " ")))
	name := strings.TrimRight(left, " ")
	if strings.HasPrefix(name, "<") || strings.HasPrefix(name, "[<") {
		leftStyle = syntaxStyle
	}
	return indent + leftStyle.Render(name) + gap + right
}

func styleFlagColumns(line string, styles helpStyles) string {
	indent := leadingSpace(line)
	left, right, ok := splitColumns(strings.TrimLeft(line, " "))
	if !ok {
		return line
	}
	gap := strings.Repeat(" ", len(left)-len(strings.TrimRight(left, " ")))
	flag := strings.TrimRight(left, " ")
	if name, placeholder, found := strings.Cut(flag, "="); found {
		flag = styles.flag.Render(name+"=") + styles.syntax.Render(placeholder)
	} else {
		flag = styles.flag.Render(flag)
	}
	return indent + flag + gap + right
}

func splitColumns(line string) (left, right string, ok bool) {
	for i := 0; i < len(line)-1; i++ {
		if line[i] != ' ' || line[i+1] != ' ' {
			continue
		}
		j := i + 2
		for j < len(line) && line[j] == ' ' {
			j++
		}
		if i > 0 && j < len(line) {
			return line[:j], line[j:], true
		}
	}
	return "", "", false
}

func leadingSpace(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " "))]
}
