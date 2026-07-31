package cmd

import "fmt"

type pingCommand struct {
	Prompt               string   `short:"p" default:"What is 2+2? Reply with just the number." help:"Prompt to send to all providers"`
	Output               string   `short:"o" help:"Output file for JSON results"`
	TimeoutSeconds       int      `name:"timeout-seconds" default:"30" help:"Per-provider timeout in seconds"`
	Provider             string   `short:"P" help:"Filter to specific providers (comma-separated)"`
	Models               string   `help:"Model selector: all, free, pinned, healthy, tag:<name>, category:<name>"`
	IncludeQuarantined   bool     `name:"include-quarantined" help:"Include catalog-quarantined models"`
	FreeOnly             bool     `name:"free-only" help:"Only include models with explicit zero input and output cost"`
	IncludeUnknownCost   bool     `name:"include-unknown-cost" help:"Include unknown-cost models when a cost cap is set"`
	MaxOutputCost        float64  `name:"max-output-cost" help:"skip targets whose output cost per 1M tokens exceeds this; 0 disables"`
	RecordRoutingMetrics bool     `name:"record-routing-metrics" help:"Record ping latency/success/token metrics into routing.metrics_path"`
	Ignored              []string `arg:"" optional:"" hidden:""`
}

func (c *pingCommand) Run(io *commandIO) error {
	pingPrompt, pingOutput, pingTimeout, pingProviders, pingModels, pingIncludeQuarantine, pingFreeOnly, pingIncludeUnknownCost, pingMaxOutputCost, pingRecordMetrics = c.Prompt, c.Output, c.TimeoutSeconds, c.Provider, c.Models, c.IncludeQuarantined, c.FreeOnly, c.IncludeUnknownCost, c.MaxOutputCost, c.RecordRoutingMetrics
	return runPing(io, nil)
}

type promptCommand struct {
	System             string   `help:"System prompt to provide context (optional)"`
	Output             string   `help:"Write markdown results to file (optional)"`
	Timeout            int      `default:"60" help:"Per-provider timeout in seconds"`
	Models             string   `help:"Model selector: all, free, pinned, healthy, tag:<name>, category:<name>"`
	Provider           string   `short:"P" help:"Filter to specific providers (comma-separated)"`
	IncludeQuarantined bool     `name:"include-quarantined" help:"Include catalog-quarantined models"`
	MaxOutputCost      float64  `name:"max-output-cost" help:"skip targets whose output cost per 1M tokens exceeds this; 0 disables"`
	FreeOnly           bool     `name:"free-only" help:"Only include models with explicit zero input and output cost"`
	IncludeUnknownCost bool     `name:"include-unknown-cost" help:"Include unknown-cost models when a cost cap is set"`
	Smart              bool     `help:"Use the usual comparison preset: --models healthy --fuse"`
	Fuse               bool     `help:"Fuse successful model responses into one final answer"`
	FuseModels         string   `name:"fuse-models" default:"zai/GLM-5.2" help:"Model selector for the final fusion response"`
	FuseControl        bool     `name:"fuse-control" help:"Also run the fusion model directly on the original prompt for comparison"`
	Text               []string `arg:"" optional:""`
}

func (c *promptCommand) Run(io *commandIO) error {
	systemPrompt, outputFile, timeoutSeconds, promptModels, promptProviders, promptIncludeQuarantine, promptMaxOutputCost, promptFreeOnly, promptIncludeUnknownCost, promptSmart, promptFuse, promptFuseModels, promptFuseControl = c.System, c.Output, c.Timeout, c.Models, c.Provider, c.IncludeQuarantined, c.MaxOutputCost, c.FreeOnly, c.IncludeUnknownCost, c.Smart, c.Fuse, c.FuseModels, c.FuseControl
	return runPromptCommand(io, c.Text)
}

type providersCommand struct {
	List    providersListCommand    `cmd:"" hidden:""`
	Refresh providersRefreshCommand `cmd:"" help:"Refresh model catalog from upstream APIs"`
}

type providersListCommand struct {
	Ignored []string `arg:"" optional:"" hidden:""`
}

func (*providersListCommand) Run(io *commandIO) error { return runProviders(io, nil) }

type providersRefreshCommand struct {
	Providers []string `arg:"" optional:""`
}

func (c *providersRefreshCommand) Run(io *commandIO) error {
	return runProvidersRefresh(io, c.Providers)
}

type routeCommand struct {
	Query    routeQueryCommand    `cmd:"" hidden:""`
	Simulate routeSimulateCommand `cmd:""`
	Canary   canaryCommand        `cmd:""`
}

type routeQueryCommand struct {
	Query string `name:"query-compat" hidden:""`
}

func (c *routeQueryCommand) Run(io *commandIO) error {
	if c.Query == "" {
		return fmt.Errorf("expected one argument")
	}
	return runRouteQuery(io, []string{c.Query})
}

type routeSimulateCommand struct {
	From    string   `help:"Path to routing events JSONL (default from config routing.events_path)"`
	Modes   string   `default:"fastest,cheapest,balanced,quality" help:"Comma-separated routing modes to replay"`
	Limit   int      `help:"Limit number of events to replay (0 = all)"`
	Report  string   `help:"Write markdown backtest report to this file"`
	Ignored []string `arg:"" optional:"" hidden:""`
}

func (c *routeSimulateCommand) Run(io *commandIO) error {
	routeEventsPath, routeModes, routeLimit, routeReportPath = c.From, c.Modes, c.Limit, c.Report
	return runRouteSimulate(io, nil)
}

type canaryCommand struct {
	Start   canaryStartCommand   `cmd:""`
	Status  canaryStatusCommand  `cmd:""`
	Promote canaryPromoteCommand `cmd:""`
	Stop    canaryStopCommand    `cmd:""`
}
type canaryStartCommand struct {
	Provider     string   `required:"" help:"Provider to canary"`
	Traffic      float64  `default:"0.1" help:"Fraction of traffic 0.0-1.0"`
	PromoteAfter int      `name:"promote-after" help:"Promote after N requests (0=manual)"`
	Baseline     string   `help:"Baseline provider (default: top scorer)"`
	Ignored      []string `arg:"" optional:"" hidden:""`
}

func (c *canaryStartCommand) Run(io *commandIO) error {
	canaryStartProvider, canaryStartTrafficPct, canaryStartPromoteAfter, canaryStartBaseline = c.Provider, c.Traffic, c.PromoteAfter, c.Baseline
	return runCanaryStart(io, nil)
}

type canaryStatusCommand struct {
	Ignored []string `arg:"" optional:"" hidden:""`
}

func (*canaryStatusCommand) Run(io *commandIO) error { return runCanaryStatus(io, nil) }

type canaryPromoteCommand struct {
	Ignored []string `arg:"" optional:"" hidden:""`
}

func (*canaryPromoteCommand) Run(io *commandIO) error { return runCanaryPromote(io, nil) }

type canaryStopCommand struct {
	Ignored []string `arg:"" optional:"" hidden:""`
}

func (*canaryStopCommand) Run(io *commandIO) error { return runCanaryStop(io, nil) }
