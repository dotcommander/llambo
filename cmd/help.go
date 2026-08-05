package cmd

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/charmbracelet/lipgloss"
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

func detailedHelp(rendered, command string) string {
	help := strings.ReplaceAll(rendered, "llambo models list", "llambo models")
	help = strings.ReplaceAll(help, "llambo models catalog list", "llambo models catalog [provider]")
	help = strings.ReplaceAll(help, "llambo providers list", "llambo providers")
	help = strings.ReplaceAll(help, "llambo route query", "llambo route <query>")
	help = strings.ReplaceAll(help, " [<ignored> ...]", "")
	help = strings.ReplaceAll(help, "Arguments:\n  [<ignored> ...]\n\n", "")
	help = strings.ReplaceAll(help, "Arguments:\n \n\n", "")
	switch command {
	case "models list":
		help = "List providers and model names from ~/.config/llambo/config.json.\n\nBy default, shows enabled providers only.\nUse --all to include disabled providers.\n\n" + help + "\nCommands:\n  catalog       List models from the local catalog\n  discover-free Discover and health-check zero-price catalog models\n  sync-pricing  Sync model pricing from models.dev into the local pricing cache\n"
	case "models catalog list":
		help = "List models tracked in ~/.config/llambo/catalog.json.\n\nOptionally filter to a single provider. Use --pinned, --avoid, --new, --free, --tag, --metadata, or --json to filter or format results.\n\n" + help + "\nCommands:\n  pin, unpin, avoid, unavoid, tag, untag, import-quality\n"
	case "models catalog import-quality":
		help = "Import task-specific quality evidence into ~/.config/llambo/catalog.json.\n\nThe input file must contain a JSON array of records:\n[\n  {\"provider\":\"openrouter\",\"model\":\"qwen/qwen3-30b-a3b-instruct-2507\",\"task\":\"extraction\",\"score\":1.0,\"source\":\"distill 86-fact benchmark\"}\n]\n\nScores must be between 0 and 1. Imported tasks are also added as catalog tags.\n\n" + help
	case "providers list":
		help += "\nCommands:\n  refresh [provider ...]    Refresh model catalog from upstream APIs\n"
	case "route query":
		help += "\nCommands:\n  simulate    Replay routing events against one or more routing modes\n  canary      Canary routing management\n"
	case "prompt":
		help = "Send the same prompt to multiple configured LLM providers and compare responses.\n\nExamples:\n  llambo prompt \"What is the capital of France?\"\n  llambo prompt --smart \"Compare these options and give me the best answer\"\n  llambo prompt \"Explain quantum computing in simple terms\"\n  llambo prompt --system \"Be concise\" \"What is the capital of France?\"\n\nThe prompt text can be provided as a positional argument or via stdin.\n\nFor lower-friction defaults, set LLAMBO_PROMPT_MODELS, LLAMBO_PROMPT_FUSE,\nor LLAMBO_PROMPT_FUSE_MODELS in your shell.\n\n" + help
	case "ping":
		help = "Test each enabled provider with a standard prompt and report timing/results.\n\nExample:\n  llambo ping\n  llambo ping --prompt \"What is 2+2?\"\n  llambo ping --output results.json\n  llambo ping --provider nvidia\n  llambo ping -P nvidia,groq\n\n" + help
	case "jobs":
		help = "Submit and monitor batch jobs to test the gateway's job processing.\n\nExamples:\n  llambo jobs run --count 10\n  llambo jobs stress --jobs 5 --requests-per-job 20\n\n" + help
	case "jobs run":
		help = "Submit a batch of requests and monitor progress until completion.\n\nExamples:\n  llambo jobs run --count 5\n  llambo jobs run -n 20 -p \"Explain {n} in one sentence\"\n  llambo jobs run --count 10 --no-wait\n\n" + help
	case "jobs stress":
		help = "Submit multiple jobs concurrently to stress test the gateway.\n\nExamples:\n  llambo jobs stress --jobs 5 --requests-per-job 20\n  llambo jobs stress -j 10 -r 50 -p \"Calculate {n}*2\"\n\n" + help
	case "serve":
		help = "Start llambo as an HTTP server providing OpenAI-compatible API\nwith parallel job processing capabilities.\n\nEndpoints:\n  POST /v1/chat/completions  - Chat completion (single request with failover)\n  POST /v1/messages          - Anthropic-compatible messages (translated to internal chat flow)\n  POST /v1/embeddings        - Generate embeddings\n  GET  /v1/models            - List available models\n  POST /v1/jobs              - Submit batch job for parallel processing\n  GET  /v1/jobs/{id}         - Get job status and results\n  POST /v1/jobs/{id}/cancel  - Cancel a running job\n  GET  /health               - Health check with backend status\n  GET  /providers            - List configured providers\n  GET  /stats                - Token usage and cost statistics\n\nJob Queue Features:\n  - Real-time parallel processing across all backends\n  - Per-backend circuit breakers for resilience\n  - Weighted round-robin load balancing\n  - Results streamed as they complete\n  - Automatic failover on backend failures\n\n" + help
	case "evals writing":
		help = "List writing benchmarks, scrape the primary public leaderboard, and track current open-weight model candidates. Scores remain source-native and are not mixed into the LES-1 report.\n\nExamples:\n  llambo evals writing\n  llambo evals writing --refresh\n  llambo evals writing --refresh --format json --output /tmp/llambo-writing.json\n\n" + help
	case "evals":
		help = "Transform cached LLM Stats and Artificial Analysis data into transparent,\nexternal-only percentile scores and task-specific rankings. No local evaluation\ntargets are used. Cached external snapshots are the default; live local models\nare discovered from loopback OMLX unless --offline or --no-omlx is set. Use\n--refresh only for external source updates. Artificial Analysis refreshes require\nAA_API_KEY. Use --format html with --output to save a standalone browser report.\n\nExamples:\n  llambo evals\n  llambo evals --rank-by coding\n  llambo evals --rank-by writing\n  llambo evals writing\n  llambo evals writing --refresh --format json --output /tmp/llambo-writing.json\n  llambo evals --rank-by coding --format html --output /tmp/llambo-evals-coding.html\n  llambo evals --min-overall -1 --max-output-price -1\n  llambo evals --no-omlx\n  llambo evals --projections ./eval-projections.json\n  llambo evals --refresh\n\n" + help
	}
	return help
}

func polishRootHelp(help string) string {
	lines := strings.Split(strings.TrimSuffix(help, "\n"), "\n")
	out := make([]string, 0, len(lines)+1)
	section := ""
	for _, line := range lines {
		switch line {
		case "Commands:":
			section = "commands"
		case "Flags:":
			section = "options"
			line = "Options:"
		}
		if line == `Run "llambo <command> --help" for more information on a command.` {
			line = `Run "llambo <command> --help" for command details.`
		}
		if section == "options" && strings.HasPrefix(strings.TrimSpace(line), "-h, --help") {
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
			if section == "usage" {
				section = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "Usage: ") {
			lines[i] = styleUsage(line, ctx, styles)
			section = "usage"
			continue
		}
		if line == trimmed && strings.HasSuffix(trimmed, ":") {
			heading := strings.TrimSuffix(strings.ToLower(trimmed), ":")
			switch heading {
			case "usage", "commands", "flags", "options", "arguments":
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
		case "usage":
			lines[i] = styleUsageSyntax(line, ctx, styles)
		case "commands":
			lines[i] = styleColumns(line, styles.command, styles.syntax)
		case "flags", "options":
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
