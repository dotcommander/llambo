package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/dotcommander/llambo/internal/gateway"
	"github.com/dotcommander/llambo/internal/styles"
	"github.com/dotcommander/llambo/providers"
)

func (cliOpts *invocationOptions) runServe(cmd *commandIO, args []string) error {
	if err := validateServeBind(cliOpts.serveHost, cliOpts.serveAllowRemote); err != nil {
		return err
	}

	// Load config
	cfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	cfg.Gateway, err = cfg.Gateway.Normalize()
	if err != nil {
		return fmt.Errorf("gateway settings: %w", err)
	}

	// Filter to enabled providers, optionally expanding pinned catalog models.
	enabledConfigs, err := routingProviderConfigs(cfg.Providers, cfg.Routing)
	if err != nil {
		return fmt.Errorf("prepare routing providers: %w", err)
	}

	if len(enabledConfigs) == 0 {
		return fmt.Errorf("no providers enabled - ensure at least one provider in ~/.config/llambo/config.json has \"enabled\": true")
	}

	// Create gateway server
	server, err := gateway.New(cmd.Context(), enabledConfigs, cfg.Routing, cfg.Gateway)
	if err != nil {
		return fmt.Errorf("init gateway: %w", err)
	}

	addr := fmt.Sprintf("%s:%s", cliOpts.serveHost, cliOpts.servePort)

	// Print startup banner
	printBannerTo(cmd.OutOrStdout(), addr, server.BackendNames())

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)
	serveDone := make(chan struct{})
	defer close(serveDone)

	go func() {
		select {
		case <-serveDone:
			return
		case <-sigChan:
		case <-cmd.Context().Done():
		}
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), styles.Dim.Render("Shutting down..."))

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Gateway.ShutdownTimeoutSeconds)*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "%s shutdown: %v\n", styles.Warning.Render("Warning:"), err)
		}
	}()

	// Start server
	if err := server.Start(addr); err != nil {
		if err.Error() == "http: Server closed" {
			fmt.Fprintln(cmd.OutOrStdout(), styles.Success.Render("Server stopped"))
			return nil
		}
		return err
	}

	return nil
}

func validateServeBind(host string, allowRemote bool) error {
	if !isLoopbackBindHost(host) && !allowRemote {
		return fmt.Errorf("refusing non-loopback host %q without --allow-remote", host)
	}
	return nil
}

func isLoopbackBindHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}

func printBanner(addr string, backends []string) {
	printBannerTo(os.Stdout, addr, backends)
}

func printBannerTo(out io.Writer, addr string, backends []string) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, styles.Header.Render("Llambo Gateway"))
	fmt.Fprintln(out, styles.Dim.Render(strings.Repeat("─", 40)))
	fmt.Fprintln(out)

	fmt.Fprintf(out, "  %s %s\n", styles.Dim.Render("Address:"), styles.Success.Render("http://"+addr))
	fmt.Fprintf(out, "  %s %s\n", styles.Dim.Render("Backends:"), strings.Join(backends, ", "))
	fmt.Fprintln(out)

	fmt.Fprintln(out, styles.Dim.Render("  Endpoints:"))
	fmt.Fprintln(out, styles.Dim.Render("    POST /v1/chat/completions"))
	fmt.Fprintln(out, styles.Dim.Render("    POST /v1/messages"))
	fmt.Fprintln(out, styles.Dim.Render("    POST /v1/embeddings"))
	fmt.Fprintln(out, styles.Dim.Render("    POST /v1/jobs"))
	fmt.Fprintln(out, styles.Dim.Render("    GET  /health"))
	fmt.Fprintln(out, styles.Dim.Render("    GET  /stats"))
	fmt.Fprintln(out)

	fmt.Fprintln(out, styles.Dim.Render("  Press Ctrl+C to stop"))
	fmt.Fprintln(out)
}

// Scalar helpers retain their signatures with independent default options.
func runServe(cmd *commandIO, args []string) error {
	return defaultInvocationOptions().runServe(cmd, args)
}
