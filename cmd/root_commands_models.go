package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/alecthomas/kong"
)

type cli struct {
	ConfigPath string           `name:"config" help:"Config file path (default ~/.config/llambo/config.json)"`
	Config     configCommand    `cmd:"" help:"Manage provider configuration"`
	Evals      evalsCommand     `cmd:"" help:"Rank models from external evaluation data"`
	Jobs       jobsCommand      `cmd:"" help:"Job testing and stress testing commands"`
	Models     modelsCommand    `cmd:"" help:"List providers and configured models"`
	Ping       pingCommand      `cmd:"" help:"Ping all configured providers"`
	Prompt     promptCommand    `cmd:"" help:"Send the same prompt to multiple configured LLM providers and compare responses."`
	Providers  providersCommand `cmd:"" help:"List configured providers and manage model catalog"`
	Route      routeCommand     `cmd:"" help:"Routing tools"`
	Serve      serveCommand     `cmd:"" help:"Start the LLM gateway server"`
}

type configCommand struct {
	Init configInitCommand `cmd:"" help:"Initialize default configuration file"`
	Show configShowCommand `cmd:"" help:"Show current configuration"`
}
type configInitCommand struct {
	Ignored []string `arg:"" optional:"" hidden:""`
}

func (*configInitCommand) Run(c *commandIO) error { return runConfigInit(c) }

type configShowCommand struct {
	Ignored []string `arg:"" optional:"" hidden:""`
}

func (*configShowCommand) Run(c *commandIO) error { return runConfigShow(c) }

type serveCommand struct {
	Port        string   `short:"P" default:"8080" help:"Port to listen on"`
	Host        string   `default:"127.0.0.1" help:"Host to bind to (use 0.0.0.0 for all interfaces)"`
	AllowRemote bool     `help:"Permit binding to a non-loopback host"`
	Ignored     []string `arg:"" optional:"" hidden:""`
}

func (c *serveCommand) Run(io *commandIO) error {
	servePort, serveHost, serveAllowRemote = c.Port, c.Host, c.AllowRemote
	return runServe(io, nil)
}

type evalsCommand struct {
	Report         evalsReportCommand  `cmd:"" default:"1" hidden:""`
	Writing        evalsWritingCommand `cmd:"" help:"List writing benchmarks and open-weight model coverage"`
	Local          evalsLocalCommand   `cmd:"" help:"Run sealed task-specific local model evaluations"`
	Refresh        bool                `help:"fetch fresh source snapshots or scrape the writing leaderboard"`
	Format         string              `default:"markdown" help:"report/catalog format: markdown, json, or html (writing supports markdown or json)"`
	Output         string              `short:"o" help:"write the report or catalog to this file instead of stdout"`
	ExportPrompts  string              `name:"export-prompts" help:"write normalized public writing prompts as JSONL (writing requires --refresh)"`
	PromptSource   string              `name:"prompt-source" default:"writingbench" help:"prompt export source: writingbench, eqbench-creative-v3, ifeval, or all"`
	PromptLimit    int                 `name:"prompt-limit" help:"maximum prompt records to export; 0 exports all selected records"`
	DiscoverModels bool                `name:"discover-open-models" help:"discover fresh public text-generation model candidates from Hugging Face (writing requires --refresh)"`
	DiscoverLimit  int                 `name:"discover-limit" default:"25" help:"maximum fresh Hugging Face candidates to include"`
	Limit          int                 `help:"maximum items (eval report defaults to 50; writing run defaults to 5 and requires 1..100); report value 0 includes all"`
	AllowPartial   bool                `name:"allow-partial" help:"continue with an LLM Stats-only report when Artificial Analysis is unavailable"`
	RankBy         string              `name:"rank-by" default:"overall" help:"ranking profile: overall, general, coding, reasoning, agents, writing, long-context, speed, value, price"`
	Offline        bool                `help:"use cached external snapshots and skip live OMLX discovery"`
	Projections    string              `help:"replace the built-in tracked local/OSS projection registry with this JSON file"`
	MinOverall     float64             `name:"min-overall" default:"40" help:"minimum overall score to include; models without overall are excluded; negative disables"`
	MaxOutputPrice float64             `name:"max-output-price" default:"10" help:"maximum known output price per 1M tokens; unknown and local prices remain eligible; negative disables"`
	OMLXURL        string              `name:"omlx-url" default:"http://127.0.0.1:8000" help:"loopback OMLX base URL for live local-model discovery"`
	NoOMLX         bool                `name:"no-omlx" help:"skip live OMLX discovery and use the reviewed projection registry as-is"`
}

type evalsReportCommand struct {
}

type evalsWritingCommand struct {
	Catalog evalsWritingCatalogCommand `cmd:"" default:"1" hidden:""`
	Run     evalsWritingRunCommand     `cmd:"" help:"Run a bounded local writing evaluation"`
}

type evalsLocalCommand struct {
	Run evalsLocalRunCommand `cmd:"" help:"Run one sealed task-specific model evaluation"`
}

type evalsWritingCatalogCommand struct{}

func (*evalsReportCommand) Run(c *evalsCommand, io *commandIO) error {
	limit := c.Limit
	if !io.FlagChanged("limit") {
		limit = 50
	}
	evalsRefresh, evalsFormat, evalsOutput, evalsLimit, evalsPartial, evalsRankBy, evalsOffline, evalsProjections, evalsMinOverall, evalsMaxOutputPrice, evalsOMLXURL, evalsNoOMLX = c.Refresh, c.Format, c.Output, limit, c.AllowPartial, c.RankBy, c.Offline, c.Projections, c.MinOverall, c.MaxOutputPrice, c.OMLXURL, c.NoOMLX
	return runEvals(io, nil)
}

func (*evalsWritingCatalogCommand) Run(c *evalsCommand, io *commandIO) error {
	return runWritingCatalog(io, c.Format, c.Output, c.Refresh, c.ExportPrompts, c.PromptSource, c.PromptLimit, c.DiscoverModels, c.DiscoverLimit)
}

type jobsCommand struct {
	Run    jobsRunCommand    `cmd:"" help:"Submit and monitor a batch job"`
	Stress jobsStressCommand `cmd:"" help:"Stress test with multiple concurrent jobs"`
}
type jobsRunCommand struct {
	Count   int           `short:"n" default:"10" help:"Number of requests in the batch"`
	Prompt  string        `short:"p" default:"What is {n}+{n}?" help:"Prompt template ({n} replaced with index)"`
	System  string        `short:"s" help:"System prompt"`
	Server  string        `default:"http://localhost:8080" help:"Server URL"`
	Wait    bool          `short:"w" default:"true" negatable:"" help:"Wait for job completion"`
	Poll    time.Duration `default:"100ms" help:"Polling interval when waiting"`
	Timeout time.Duration `default:"5m" help:"Maximum wait time"`
	Verify  bool          `default:"true" negatable:"" help:"Verify response integrity"`
	Ignored []string      `arg:"" optional:"" hidden:""`
}

func (c *jobsRunCommand) Run(io *commandIO) error {
	runCount, runPrompt, runSystem, runServer, runWait, runPoll, runTimeout, runVerify = c.Count, c.Prompt, c.System, c.Server, c.Wait, c.Poll, c.Timeout, c.Verify
	return runJobsRun(io, nil)
}

type jobsStressCommand struct {
	Jobs           int      `short:"j" default:"5" help:"Number of concurrent jobs"`
	RequestsPerJob int      `name:"requests-per-job" short:"r" default:"20" help:"Requests per job"`
	Prompt         string   `short:"p" default:"What is {n}+{n}?" help:"Prompt template ({n} replaced with index)"`
	Server         string   `default:"http://localhost:8080" help:"Server URL"`
	Ignored        []string `arg:"" optional:"" hidden:""`
}

func (c *jobsStressCommand) Run(io *commandIO) error {
	stressJobs, stressRequestsPerJob, stressPrompt, stressServer = c.Jobs, c.RequestsPerJob, c.Prompt, c.Server
	return runJobsStress(io, nil)
}

type modelsCommand struct {
	List         modelsListCommand         `cmd:"" help:"List configured and scored catalog models immediately with cached metrics"`
	Catalog      modelsCatalogCommand      `cmd:"" help:"List models from the local catalog"`
	DiscoverFree modelsDiscoverFreeCommand `cmd:"" name:"discover-free" help:"Discover and health-check zero-price catalog models"`
	SyncPricing  modelsSyncPricingCommand  `cmd:"" name:"sync-pricing" help:"Sync model pricing from models.dev into the local pricing cache"`
}

type modelsListCommand struct {
	All            bool     `help:"Include disabled providers"`
	CSV            bool     `help:"Output CSV with score,speed,latency columns"`
	Available      bool     `help:"Fetch all available models from provider APIs"`
	Provider       string   `short:"P" help:"Limit --available to providers (comma-separated), for example omlx"`
	TimeoutSeconds int      `name:"timeout-seconds" default:"10" help:"HTTP timeout for --available"`
	Metrics        bool     `help:"Compatibility flag; cached score, speed, and latency are shown by default"`
	Grouped        bool     `default:"true" negatable:"" help:"Group models per provider (model1, model2, model3)"`
	Ignored        []string `arg:"" optional:"" hidden:""`
}

func (c *modelsListCommand) Run(io *commandIO) error {
	modelsAll, modelsCSV, modelsAvailable, modelsTimeoutSec, modelsProviderFilter = c.All, c.CSV, c.Available, c.TimeoutSeconds, c.Provider
	// Metrics are the default listing contract. Keep --metrics accepted so
	// existing scripts remain valid, but do not require callers to pass it.
	modelsMetrics = true
	modelsGrouped = false
	return runModels(io, nil)
}

type modelsCatalogCommand struct {
	List          modelsCatalogListCommand `cmd:"" hidden:""`
	Pin           catalogPairCommand       `cmd:"" help:"Pin a model in the catalog"`
	Unpin         catalogPairCommand       `cmd:"" help:"Remove pin from a catalog model"`
	AvoidModel    catalogAvoidCommand      `cmd:"" name:"avoid" help:"Mark a catalog model as avoided"`
	Unavoid       catalogPairCommand       `cmd:"" help:"Clear avoid flag from a catalog model"`
	TagModel      catalogTagCommand        `cmd:"" name:"tag" help:"Add a tag to a catalog model"`
	Untag         catalogTagCommand        `cmd:"" help:"Remove a tag from a catalog model"`
	ImportQuality catalogImportCommand     `cmd:"" name:"import-quality" help:"Import task-specific model quality evidence"`
}

type modelsCatalogListCommand struct {
	Pinned   bool     `help:"Only show pinned models"`
	Avoid    bool     `help:"Only show avoided models"`
	New      bool     `help:"Only show models new in the last refresh"`
	Free     bool     `help:"Only show models with explicit zero input and output cost"`
	Tag      string   `help:"Only show models with this tag"`
	Metadata bool     `help:"Show catalog metadata such as context length, reasoning, and parameter count"`
	JSON     bool     `help:"Output JSON"`
	Provider string   `name:"provider-compat" hidden:""`
	Ignored  []string `arg:"" optional:"" hidden:""`
}

func (c *modelsCatalogListCommand) Run(io *commandIO) error {
	catalogFilterPinned, catalogFilterAvoid, catalogFilterNew, catalogFilterFree, catalogFilterTag, catalogShowMetadata, catalogOutputJSON = c.Pinned, c.Avoid, c.New, c.Free, c.Tag, c.Metadata, c.JSON
	return runModelsCatalog(io, optionalString(c.Provider))
}

type catalogPairCommand struct {
	Provider string `arg:""`
	Model    string `arg:""`
}

func (c *catalogPairCommand) args() []string { return []string{c.Provider, c.Model} }
func (c *catalogPairCommand) Run(k *kong.Context, io *commandIO) error {
	switch strings.Fields(k.Command())[2] {
	case "pin":
		return runModelsCatalogPin(io, c.args())
	case "unpin":
		return runModelsCatalogUnpin(io, c.args())
	case "unavoid":
		return runModelsCatalogUnavoid(io, c.args())
	}
	return fmt.Errorf("unknown catalog operation")
}

type catalogAvoidCommand struct {
	Provider string `arg:""`
	Model    string `arg:""`
	Reason   string `help:"Reason for avoiding the model"`
}

func (c *catalogAvoidCommand) Run(io *commandIO) error {
	avoidReason = c.Reason
	return runModelsCatalogAvoid(io, []string{c.Provider, c.Model})
}

type catalogTagCommand struct {
	Provider string `arg:""`
	Model    string `arg:""`
	Tag      string `arg:""`
}

func (c *catalogTagCommand) Run(k *kong.Context, io *commandIO) error {
	if strings.HasSuffix(k.Command(), " untag") {
		return runModelsCatalogUntag(io, []string{c.Provider, c.Model, c.Tag})
	}
	return runModelsCatalogTag(io, []string{c.Provider, c.Model, c.Tag})
}

type catalogImportCommand struct {
	File string `arg:""`
}

func (c *catalogImportCommand) Run(io *commandIO) error {
	return runModelsCatalogImportQuality(io, []string{c.File})
}

type modelsDiscoverFreeCommand struct {
	Pin                bool     `help:"Pin free models that pass the health check"`
	IncludeQuarantined bool     `name:"include-quarantined" help:"Ping models still in catalog quarantine"`
	Provider           string   `short:"P" help:"Filter to specific providers (comma-separated)"`
	TimeoutSeconds     int      `name:"timeout-seconds" default:"30" help:"Per-model timeout in seconds"`
	Ignored            []string `arg:"" optional:"" hidden:""`
}

func (c *modelsDiscoverFreeCommand) Run(io *commandIO) error {
	discoverFreePin, discoverFreeIncludeQuarantine, discoverFreeProviders, discoverFreeTimeout = c.Pin, c.IncludeQuarantined, c.Provider, c.TimeoutSeconds
	return runModelsDiscoverFree(io, nil)
}

type modelsSyncPricingCommand struct {
	DryRun   bool     `name:"dry-run" help:"compute counts without writing the pricing file"`
	Provider []string `short:"P" sep:"," help:"limit to these providers (comma-separated)"`
	Ignored  []string `arg:"" optional:"" hidden:""`
}

func (c *modelsSyncPricingCommand) Run(io *commandIO) error {
	modelsSyncPricingDryRun, modelsSyncPricingProviders = c.DryRun, c.Provider
	return runModelsSyncPricing(io, nil)
}
