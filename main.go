package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/dotcommander/llambo/cmd"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cmd.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_ = cmd.WriteError(os.Stderr, err)
		return 1
	}
	return 0
}
