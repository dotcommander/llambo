package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/dotcommander/llambo/providers"
)

const rootDescription = `Llambo is a high-performance LLM gateway with parallel job processing.
(LLM + Lamborghini = fast, multi-provider AI routing)

Start the gateway:
  llambo serve [--port 8080]

Features:
  - OpenAI-compatible API (/v1/chat/completions, /v1/embeddings)
  - Real-time parallel job processing (/v1/jobs)
  - Multi-provider load balancing
  - Per-backend circuit breakers for resilience
  - Automatic failover on backend failures

Backends configured in ~/.config/llambo/config.json`

type commandIO struct {
	ctx     context.Context
	stdout  io.Writer
	stderr  io.Writer
	changed map[string]bool
}

type commandPrinter struct{ io.Writer }

func (p commandPrinter) Println(values ...any) (int, error) { return fmt.Fprintln(p.Writer, values...) }
func (p commandPrinter) Printf(format string, values ...any) (int, error) {
	return fmt.Fprintf(p.Writer, format, values...)
}
func (p commandPrinter) Sprintf(format string, values ...any) string {
	return fmt.Sprintf(format, values...)
}

func (c *commandIO) Context() context.Context { return c.ctx }
func (c *commandIO) OutOrStdout() io.Writer {
	if c == nil || c.stdout == nil {
		return io.Discard
	}
	return c.stdout
}
func (c *commandIO) ErrOrStderr() io.Writer {
	if c == nil || c.stderr == nil {
		return io.Discard
	}
	return c.stderr
}
func (c *commandIO) FlagChanged(name string) bool { return c.changed[name] }

func optionalString(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}

func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return execute(ctx, args, stdout, stderr)
}

func execute(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		args = []string{"--help"}
	}
	args = normalizeBranchPositionals(args)
	var tree cli
	ioctx := &commandIO{ctx: ctx, stdout: stdout, stderr: stderr, changed: presentFlags(args)}
	parser, err := kong.New(
		&tree,
		kong.Name("llambo"),
		kong.Description(rootDescription),
		kong.Writers(stdout, stderr),
		kong.BindTo(ctx, (*context.Context)(nil)),
		kong.Bind(ioctx),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact:             true,
			Tree:                true,
			Summary:             true,
			FlagsLast:           true,
			NoExpandSubcommands: true,
		}),
		kong.Help(llamboHelp),
	)
	if err != nil {
		return err
	}
	exited := false
	parser.Exit = func(int) { exited = true }
	parsed, err := parser.Parse(args)
	if exited {
		return nil
	}
	if err != nil {
		return err
	}
	if tree.ConfigPath != "" {
		providers.SetConfigFile(tree.ConfigPath)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err = parsed.Run()
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func llamboHelp(options kong.HelpOptions, ctx *kong.Context) error {
	var rendered bytes.Buffer
	stdout := ctx.Stdout
	ctx.Stdout = &rendered
	err := kong.DefaultHelpPrinter(options, ctx)
	ctx.Stdout = stdout
	if err != nil {
		return err
	}
	help := strings.ReplaceAll(rendered.String(), "llambo models list", "llambo models")
	help = strings.ReplaceAll(help, "llambo models catalog list", "llambo models catalog [provider]")
	help = strings.ReplaceAll(help, "llambo providers list", "llambo providers")
	help = strings.ReplaceAll(help, "llambo route query", "llambo route <query>")
	help = strings.ReplaceAll(help, " [<ignored> ...]", "")
	help = strings.ReplaceAll(help, "Arguments:\n  [<ignored> ...]\n\n", "")
	switch ctx.Command() {
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
	case "evals":
		help = "Transform cached LLM Stats and Artificial Analysis data into transparent,\nexternal-only percentile scores and task-specific rankings. No local evaluation\ntargets are used. Cached external snapshots are the default; live local models\nare discovered from loopback OMLX unless --offline or --no-omlx is set. Use\n--refresh only for external source updates. Artificial Analysis refreshes require\nAA_API_KEY.\n\nExamples:\n  llambo evals\n  llambo evals --rank-by coding\n  llambo evals --rank-by writing\n  llambo evals --min-overall -1 --max-output-price -1\n  llambo evals --no-omlx\n  llambo evals --projections ./eval-projections.json\n  llambo evals --refresh\n\n" + help
	}
	_, err = fmt.Fprint(stdout, help)
	return err
}

func normalizeBranchPositionals(args []string) []string {
	root := firstPositional(args, 0, map[string]bool{"--config": true})
	if root < 0 {
		return args
	}
	if args[root] == "models" {
		branch := firstPositional(args, root+1, map[string]bool{"--config": true})
		if branch < 0 {
			return insertArg(args, root+1, "list")
		}
		if args[branch] == "catalog" {
			leaf := firstPositional(args, branch+1, map[string]bool{"--config": true, "--tag": true})
			if leaf < 0 {
				return insertArg(args, branch+1, "list")
			}
			switch args[leaf] {
			case "pin", "unpin", "avoid", "unavoid", "tag", "untag", "import-quality":
				return args
			default:
				args = append([]string(nil), args...)
				args[leaf] = "--provider-compat=" + args[leaf]
				return insertArg(args, branch+1, "list")
			}
		}
		switch args[branch] {
		case "list", "discover-free", "sync-pricing":
			return args
		default:
			return insertArg(args, root+1, "list")
		}
	}
	if args[root] == "route" {
		branch := firstPositional(args, root+1, map[string]bool{"--config": true})
		if branch < 0 {
			return insertArg(args, root+1, "query")
		}
		switch args[branch] {
		case "query", "simulate", "canary":
			return args
		default:
			args = append([]string(nil), args...)
			args[branch] = "--query-compat=" + args[branch]
			return insertArg(args, root+1, "query")
		}
	}
	if args[root] == "providers" {
		branch := firstPositional(args, root+1, map[string]bool{"--config": true})
		if branch < 0 {
			return insertArg(args, root+1, "list")
		}
		switch args[branch] {
		case "list", "refresh":
			return args
		default:
			return insertArg(args, root+1, "list")
		}
	}
	return args
}

func firstPositional(args []string, start int, valueFlags map[string]bool) int {
	for i := start; i < len(args); i++ {
		if !strings.HasPrefix(args[i], "-") {
			return i
		}
		if valueFlags[args[i]] && i+1 < len(args) {
			i++
		}
	}
	return -1
}

func insertArg(args []string, index int, value string) []string {
	result := make([]string, 0, len(args)+1)
	result = append(result, args[:index]...)
	result = append(result, value)
	return append(result, args[index:]...)
}

func presentFlags(args []string) map[string]bool {
	out := map[string]bool{}
	for _, arg := range args {
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		name := strings.TrimPrefix(arg, "--")
		if i := strings.IndexByte(name, '='); i >= 0 {
			name = name[:i]
		}
		if strings.HasPrefix(name, "no-") {
			out[strings.TrimPrefix(name, "no-")] = true
		}
		out[name] = true
	}
	return out
}
