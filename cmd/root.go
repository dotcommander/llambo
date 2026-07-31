package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/dotcommander/llambo/providers"
)

const rootDescription = "High-performance LLM gateway with parallel job processing."

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
	if isVersionFlag(args) {
		_, err := fmt.Fprintf(stdout, "llambo version %s\n", VersionString())
		return err
	}
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
			Tree:                false,
			Summary:             true,
			FlagsLast:           true,
			NoExpandSubcommands: true,
			WrapUpperBound:      88,
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
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	err = parsed.Run()
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
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
