package cmd

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/gateway"
	"github.com/dotcommander/llambo/providers"
)

func TestExecuteMetadataAndErrors(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"prompt", "--help"}, {"models", "catalog", "--help"}} {
		var out, errOut bytes.Buffer
		if err := execute(context.Background(), args, &out, &errOut); err != nil {
			t.Fatalf("execute(%v): %v", args, err)
		}
		if !strings.Contains(out.String(), "Usage:") {
			t.Fatalf("execute(%v) missing usage: %s", args, out.String())
		}
	}
	for _, args := range [][]string{{"completion"}, {"version"}, {"--bad-flag"}} {
		var out, errOut bytes.Buffer
		if err := execute(context.Background(), args, &out, &errOut); err == nil {
			t.Fatalf("execute(%v) unexpectedly succeeded", args)
		}
	}
}

func TestRootHelpGolden(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("CLICOLOR_FORCE", "")

	want, err := os.ReadFile(filepath.Join("testdata", "root_help.golden"))
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"--help"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != string(want) {
		t.Fatalf("root help mismatch (-want +got):\n-%s\n+%s", want, got)
	}
	for _, hidden := range []string{"init", "run", "pin", "simulate", "start"} {
		if strings.Contains(out.String(), "  "+hidden+" ") {
			t.Errorf("root help includes recursive command %q:\n%s", hidden, out.String())
		}
	}
}

func TestNestedHelpListsDirectChildren(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"models", "--help"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"catalog", "discover-free", "sync-pricing"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("models help missing direct child %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "import-quality") {
		t.Errorf("models help includes recursive descendant:\n%s", out.String())
	}
}

func TestVersionFlagsAndPositionalCommand(t *testing.T) {
	original := Version
	Version = "v0.1.0-test"
	t.Cleanup(func() { Version = original })
	for _, flag := range []string{"-v", "--version"} {
		var out, errOut bytes.Buffer
		if err := execute(context.Background(), []string{flag}, &out, &errOut); err != nil {
			t.Fatalf("execute(%q): %v", flag, err)
		}
		if got, want := out.String(), "llambo version v0.1.0-test\n"; got != want {
			t.Errorf("execute(%q) output = %q, want %q", flag, got, want)
		}
	}
	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"version"}, &out, &errOut); err == nil {
		t.Fatal("positional version unexpectedly succeeded")
	}
}

func TestHelpAndErrorsHonorTerminalColorControls(t *testing.T) {
	t.Run("forced color", func(t *testing.T) {
		t.Setenv("NO_COLOR", "")
		t.Setenv("CLICOLOR_FORCE", "1")
		var out, errOut bytes.Buffer
		if err := execute(context.Background(), []string{"serve", "--help"}, &out, &errOut); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "\x1b[") {
			t.Fatalf("forced-color help contains no ANSI styling: %q", out.String())
		}
		out.Reset()
		if err := execute(context.Background(), []string{"--help"}, &out, &errOut); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "\x1b[2mRun ") {
			t.Fatalf("forced-color command hint is not faint: %q", out.String())
		}
		if err := WriteError(&errOut, errors.New("unknown flag --wat")); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(errOut.String(), "\x1b[") || strings.Contains(errOut.String(), "Usage:") {
			t.Fatalf("unexpected forced-color error output: %q", errOut.String())
		}
	})
	t.Run("no color", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		t.Setenv("CLICOLOR_FORCE", "")
		var out, errOut bytes.Buffer
		if err := execute(context.Background(), []string{"--help"}, &out, &errOut); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "\x1b[") {
			t.Fatalf("NO_COLOR help contains ANSI styling: %q", out.String())
		}
		if err := WriteError(&errOut, errors.New("unknown flag --wat")); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(errOut.String(), "\x1b[") || strings.Contains(errOut.String(), "Usage:") {
			t.Fatalf("unexpected NO_COLOR error output: %q", errOut.String())
		}
	})
}

func TestParseErrorsAreConcise(t *testing.T) {
	var out, errOut bytes.Buffer
	err := execute(context.Background(), []string{"--bad-flag"}, &out, &errOut)
	if err == nil {
		t.Fatal("invalid flag unexpectedly succeeded")
	}
	if out.Len() != 0 || errOut.Len() != 0 || strings.Contains(err.Error(), "Usage:") {
		t.Fatalf("parse error was not concise: stdout=%q stderr=%q error=%q", out.String(), errOut.String(), err)
	}
}

func TestWriterAwareOperationalOutput(t *testing.T) {
	t.Run("jobs progress", func(t *testing.T) {
		var out bytes.Buffer
		printProgressTo(&out, &gateway.JobResponse{Status: "pending", Total: 2})
		if !strings.Contains(out.String(), "pending") {
			t.Fatalf("missing progress output: %q", out.String())
		}
	})
	t.Run("route baseline", func(t *testing.T) {
		var out bytes.Buffer
		printActualBaselineTo(&out, []providers.RouteEvent{{ChosenProvider: "p", LatencyMs: 12}})
		if !strings.Contains(out.String(), "Actual baseline") {
			t.Fatalf("missing route output: %q", out.String())
		}
	})
	t.Run("serve banner", func(t *testing.T) {
		var out bytes.Buffer
		printBannerTo(&out, "127.0.0.1:8080", []string{"p"})
		if !strings.Contains(out.String(), "127.0.0.1:8080") {
			t.Fatalf("missing banner output: %q", out.String())
		}
	})
	t.Run("eval notification", func(t *testing.T) {
		var errOut bytes.Buffer
		path := filepath.Join(t.TempDir(), "report.md")
		if err := writeEvalsReportTo(&errOut, path, []byte("ok")); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(errOut.String(), path) {
			t.Fatalf("missing eval notification: %q", errOut.String())
		}
	})
}

func TestValidateServeBindRequiresRemoteAcknowledgment(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name        string
		host        string
		allowRemote bool
		wantErr     bool
	}{
		{name: "IPv4 loopback", host: "127.0.0.1"},
		{name: "IPv6 loopback", host: "::1"},
		{name: "localhost", host: "localhost"},
		{name: "all interfaces rejected", host: "0.0.0.0", wantErr: true},
		{name: "all interfaces acknowledged", host: "0.0.0.0", allowRemote: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateServeBind(tt.host, tt.allowRemote)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateServeBind(%q, %t) error = %v, wantErr %t", tt.host, tt.allowRemote, err, tt.wantErr)
			}
		})
	}
}

func TestLegacyHelpContract(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		wants []string
	}{
		{[]string{"models", "--help"}, []string{"--all", "catalog", "discover-free"}},
		{[]string{"models", "catalog", "--help"}, []string{"[provider]", "--metadata", "import-quality"}},
		{[]string{"models", "catalog", "import-quality", "--help"}, []string{"JSON array", "Scores must be between 0 and 1", "catalog tags"}},
		{[]string{"prompt", "--help"}, []string{"via stdin", "--fuse-models", "Examples:"}},
		{[]string{"ping", "--help"}, []string{"-P", "--timeout-seconds", "Example:"}},
	} {
		var out, errOut bytes.Buffer
		if err := execute(context.Background(), tc.args, &out, &errOut); err != nil {
			t.Fatalf("execute(%v): %v", tc.args, err)
		}
		for _, want := range tc.wants {
			if !strings.Contains(out.String(), want) {
				t.Errorf("help %v missing %q:\n%s", tc.args, want, out.String())
			}
		}
	}
}

func TestLegacyArgumentNormalization(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"models", "--all"}, []string{"models", "list", "--all"}},
		{[]string{"--config", "cfg.json", "models", "--csv"}, []string{"--config", "cfg.json", "models", "list", "--csv"}},
		{[]string{"models", "catalog", "openrouter", "--json"}, []string{"models", "catalog", "list", "--provider-compat=openrouter", "--json"}},
		{[]string{"models", "catalog", "openrouter", "extra", "--json"}, []string{"models", "catalog", "list", "--provider-compat=openrouter", "extra", "--json"}},
		{[]string{"models", "catalog", "--json", "openrouter"}, []string{"models", "catalog", "list", "--json", "--provider-compat=openrouter"}},
		{[]string{"models", "catalog", "--tag", "smart", "openrouter"}, []string{"models", "catalog", "list", "--tag", "smart", "--provider-compat=openrouter"}},
		{[]string{"route", "write a draft"}, []string{"route", "query", "--query-compat=write a draft"}},
		{[]string{"providers"}, []string{"providers", "list"}},
	} {
		got := normalizeBranchPositionals(tc.args)
		if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Errorf("normalizeBranchPositionals(%q) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestRunnableParentCommandsDispatch(t *testing.T) {
	oldConfig := providers.ConfigFile()
	t.Cleanup(func() { providers.SetConfigFile(oldConfig) })
	configPath := filepath.Join(t.TempDir(), "config.json")
	config := `{"default_provider":"openai","providers":{"openai":{"base_url":"https://api.openai.com","model":"gpt-4o","enabled":true,"requires_key":false}}}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	var providersOut, providersErr bytes.Buffer
	if err := execute(context.Background(), []string{"--config", configPath, "providers"}, &providersOut, &providersErr); err != nil {
		t.Fatalf("bare providers failed: %v", err)
	}
	if !strings.Contains(providersOut.String(), "PROVIDER") || !strings.Contains(providersOut.String(), "openai") {
		t.Fatalf("bare providers did not list providers: %q", providersOut.String())
	}

	var routeOut, routeErr bytes.Buffer
	if err := execute(context.Background(), []string{"--config", configPath, "route", "write a draft"}, &routeOut, &routeErr); err != nil {
		t.Fatalf("route query failed: %v", err)
	}
	for _, want := range []string{"intent:", "model:", "reason:"} {
		if !strings.Contains(routeOut.String(), want) {
			t.Errorf("route output missing %q: %q", want, routeOut.String())
		}
	}
}

func TestHistoricallyPermissiveLeavesIgnoreExtraArgs(t *testing.T) {
	oldConfig := providers.ConfigFile()
	t.Cleanup(func() { providers.SetConfigFile(oldConfig) })
	missing := filepath.Join(t.TempDir(), "missing.json")
	for _, args := range [][]string{
		{"--config", missing, "providers", "extra"},
		{"--config", missing, "config", "show", "extra"},
		{"--config", missing, "route", "simulate", "extra"},
		{"--config", missing, "serve", "extra"},
		{"--config", missing, "ping", "extra"},
	} {
		var out, errOut bytes.Buffer
		err := execute(context.Background(), args, &out, &errOut)
		if err == nil {
			t.Errorf("execute(%v) unexpectedly succeeded with missing config", args)
			continue
		}
		if strings.Contains(err.Error(), "unexpected argument") {
			t.Errorf("execute(%v) rejected historically ignored argument: %v", args, err)
		}
	}
}

func TestCompactShorthandAndExplicitDefaultDetection(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"models", "discover-free", "-Popenrouter", "--help"}, &out, &errOut); err != nil {
		t.Fatalf("compact provider shorthand rejected: %v", err)
	}
	changed := presentFlags([]string{"prompt", "--models=free", "--fuse=false", "--fuse-models=zai/explicit"})
	for _, name := range []string{"models", "fuse", "fuse-models"} {
		if !changed[name] {
			t.Errorf("explicit default flag %q was not detected", name)
		}
	}
}

func TestExecutePropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	err := execute(ctx, []string{"providers", "refresh"}, &out, &errOut)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if out.Len() != 0 || errOut.Len() != 0 {
		t.Fatalf("canceled dispatch wrote output: stdout=%q stderr=%q", out.String(), errOut.String())
	}
}

func TestJobsRunCancelsInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		var out, errOut bytes.Buffer
		result <- execute(ctx, []string{"jobs", "run", "--server", server.URL, "--count", "1"}, &out, &errOut)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("jobs request did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected in-flight cancellation, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("jobs run did not return after cancellation")
	}
}

func TestJobsStressPropagatesCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		var out, errOut bytes.Buffer
		result <- execute(ctx, []string{"jobs", "stress", "--server", server.URL, "--jobs", "1", "--requests-per-job", "1"}, &out, &errOut)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("stress request did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected stress cancellation, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("jobs stress did not return after cancellation")
	}
}
