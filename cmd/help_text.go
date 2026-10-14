package cmd

import "strings"

// This file owns the curated help copy for command paths. Entries pair a
// hand-authored prefix (description and examples) with an optional appendix
// (subcommand list) rendered around the generated Kong template.

// helpOverride carries curated fragments for one command path.
type helpOverride struct {
	prefix   string
	appendix string
}

// curatedCommandHelp holds descriptions, examples, and subcommand lists
// that the generated Kong help template cannot express.
var curatedCommandHelp = map[string]helpOverride{
	"models list": {
		prefix:   "List configured provider model names immediately from ~/.config/llambo/config.json with cached score, speed, and latency. This path never calls provider APIs unless --available is set.\n\nBy default, shows enabled providers only and always includes cached metrics. Use --all to include disabled providers. The --metrics flag remains accepted for compatibility but is no longer required. Metrics scores are standardized to 0-100 for display; catalog quality remains stored as 0-1. The task label stays visible because task scores are not interchangeable. Missing cached values show —. To list the live OMLX inventory without querying every configured provider, use `llambo models list --available -P omlx`.\n\n",
		appendix: "\nCommands:\n  catalog       List models from the local catalog\n  discover-free Discover and health-check zero-price catalog models\n  sync-pricing  Sync model pricing from models.dev into the local pricing cache\n",
	},
	"models catalog list": {
		prefix:   "List models tracked in ~/.config/llambo/catalog.json.\n\nOptionally filter to a single provider. Use --pinned, --avoid, --new, --free, --tag, --metadata, or --json to filter or format results.\n\n",
		appendix: "\nCommands:\n  pin, unpin, avoid, unavoid, tag, untag, import-quality\n",
	},
	"models catalog import-quality": {
		prefix: "Import task-specific quality evidence into ~/.config/llambo/catalog.json.\n\nThe input file must contain a JSON array of records:\n[\n  {\"provider\":\"openrouter\",\"model\":\"qwen/qwen3-30b-a3b-instruct-2507\",\"task\":\"extraction\",\"score\":1.0,\"source\":\"distill 86-fact benchmark\"}\n]\n\nScores must be between 0 and 1. Imported tasks are also added as catalog tags.\n\n",
	},
	"providers list": {
		appendix: "\nCommands:\n  refresh [provider ...]    Refresh model catalog from upstream APIs\n",
	},
	"route query": {
		appendix: "\nCommands:\n  simulate    Replay routing events against one or more routing modes\n  canary      Canary routing management\n",
	},
	"prompt": {
		prefix: "Send the same prompt to multiple configured LLM providers and compare responses.\n\nExamples:\n  llambo prompt \"What is the capital of France?\"\n  llambo prompt --smart \"Compare these options and give me the best answer\"\n  llambo prompt \"Explain quantum computing in simple terms\"\n  llambo prompt --system \"Be concise\" \"What is the capital of France?\"\n\nThe prompt text can be provided as a positional argument or via stdin.\n\nFor lower-friction defaults, set LLAMBO_PROMPT_MODELS, LLAMBO_PROMPT_FUSE,\nor LLAMBO_PROMPT_FUSE_MODELS in your shell.\n\n",
	},
	ping1: {
		prefix: "Test each enabled provider with a standard prompt and report timing/results.\n\nExample:\n  llambo ping\n  llambo ping --prompt \"What is 2+2?\"\n  llambo ping --output results.json\n  llambo ping --provider nvidia\n  llambo ping -P nvidia,groq\n\n",
	},
	"jobs": {
		prefix: "Submit and monitor batch jobs to test the gateway's job processing.\n\nExamples:\n  llambo jobs run --count 10\n  llambo jobs stress --jobs 5 --requests-per-job 20\n\n",
	},
	"jobs run": {
		prefix: "Submit a batch of requests and monitor progress until completion.\n\nExamples:\n  llambo jobs run --count 5\n  llambo jobs run -n 20 -p \"Explain {n} in one sentence\"\n  llambo jobs run --count 10 --no-wait\n\n",
	},
	"jobs stress": {
		prefix: "Submit multiple jobs concurrently to stress test the gateway.\n\nExamples:\n  llambo jobs stress --jobs 5 --requests-per-job 20\n  llambo jobs stress -j 10 -r 50 -p \"Calculate {n}*2\"\n\n",
	},
	"serve": {
		prefix: "Start llambo as an HTTP server providing OpenAI-compatible API\nwith parallel job processing capabilities.\n\nEndpoints:\n  POST /v1/chat/completions  - Chat completion (single request with failover)\n  POST /v1/messages          - Anthropic-compatible messages (translated to internal chat flow)\n  POST /v1/embeddings        - Generate embeddings\n  GET  /v1/models            - List available models\n  POST /v1/jobs              - Submit batch job for parallel processing\n  GET  /v1/jobs/{id}         - Get job status and results\n  POST /v1/jobs/{id}/cancel  - Cancel a running job\n  GET  /health               - Health check with backend status\n  GET  /providers            - List configured providers\n  GET  /stats                - Token usage and cost statistics\n\nJob Queue Features:\n  - Real-time parallel processing across all backends\n  - Per-backend circuit breakers for resilience\n  - Weighted round-robin load balancing\n  - Results become visible through job-status polling as they complete\n  - Automatic failover on backend failures\n\n",
	},
	"evals writing": {
		prefix:   "List writing benchmarks, validate public artifacts, export normalized prompts, or run a separately bounded local evaluation. WritingBench is the primary external writing authority; local results never change Llambo Scores.\n\nExamples:\n  llambo evals writing\n  llambo evals writing --refresh\n  llambo evals writing --refresh --prompt-source writingbench --prompt-limit 20 --export-prompts /tmp/writingbench.jsonl\n  llambo evals writing run --input /tmp/writingbench.jsonl --benchmark writingbench --model deepseek/deepseek-v4.1-flash --judge-model openrouter/anthropic/claude-sonnet-5 --output-dir /tmp/writing-run\n\n",
		appendix: "\nCommands:\n  run    Run a bounded, resumable local writing evaluation\n",
	},
	"evals writing run": {
		prefix: "Validate and plan a local writing evaluation without provider calls by default. Paid execution requires explicit run and campaign cost ceilings. One exact provider/model identity, known pricing, an explicit output directory, a shared campaign ledger, and a sealed prompt file are required.\n\nSafety defaults:\n  - 5 prompts (range 1..100)\n  - generation concurrency 2 (range 1..8, also capped by provider workers)\n  - judge concurrency 1\n  - 5-minute timeout per call\n  - 8,192 judge output tokens\n  - WritingBench: 1 generation and 1 combined-criteria judgment per prompt\n  - EQ-Bench Creative v3: 3 iterations and local-rubric scoring only\n\nExamples:\n  llambo evals writing run --input /tmp/writingbench.jsonl --benchmark writingbench --model deepseek/deepseek-v4.1-flash --judge-model gemini/gemini-3.7-flash --output-dir /tmp/writing-run --campaign-ledger /tmp/campaign-budget.json\n  llambo evals writing run --input /tmp/writingbench.jsonl --benchmark writingbench --model deepseek/deepseek-v4.1-flash --judge-model gemini/gemini-3.7-flash --output-dir /tmp/writing-run --campaign-ledger /tmp/campaign-budget.json --execute --max-run-cost 10.00 --max-campaign-cost 25.00\n\n",
	},
	"evals export normalized": {
		prefix: "Export a cache-only normalized JSONL dataset joining source observations, identity bridges, frozen cohorts, operational metadata, projections, and all six LLAMBO category scores. Semantically equivalent observations are deduplicated with provenance retained as mirrors; scores are editorially ranked within each category. No provider, local model, or live OMLX call runs.\n\nExample:\n  llambo evals export normalized --output-dir /tmp/llambo-normalized-evals\n\n",
	},
	"evals omlx": {
		prefix: "Show the cached OMLX LLAMBO score snapshot immediately with no network, provider, or local-model call. Use --live only to discover the current OMLX admin inventory, rebuild the OMLX-only category snapshot, and publish it. Markdown is compact by default; --format json emits the snapshot.\n\nExamples:\n  llambo evals omlx\n  llambo evals omlx --live\n  llambo evals omlx --format json\n\n",
	},
	"evals sources refresh": {
		prefix: "Refresh named sealed evaluation source caches through one ingestion pipeline. The default writing set is WritingBench, EQ-Bench Creative v3, Lech Mazur Creative Story-Writing, and Arena Creative Writing; llm-stats-stats-v1 performs the authenticated Stats v1 refresh alone. Every adapter validates provenance before publication, preserves predecessor caches, and atomically replaces each normalized snapshot. No model provider or local inference runs.\n\nExamples:\n  llambo evals sources refresh\n  llambo evals sources refresh arena-creative-writing\n  llambo evals sources refresh writingbench,eqbench-creative-v3,lechmazur-writing,arena-creative-writing\n  llambo evals sources refresh llm-stats-stats-v1\n\n",
	},
	"evals": {
		prefix: "Render six independent LLAMBO-9 capability scores—agents, coding, instruction-following, long-context, reasoning, and writing—from sealed cached remote benchmark evidence. Direct scores use equal independent benchmark families and common frozen-cohort percentiles. Missing OMLX-only cells use a sealed calibrated estimate marked ᵉ with zero coverage and low confidence; canonical hosted rows remain evidence-only. No overall composite exists. Speed, latency, memory, and price remain operational metadata and never affect a capability rank. Default operation is cache-only: it performs no provider call, no local inference, and no live OMLX discovery.\n\nExamples:\n  llambo evals\n  llambo evals --rank-by writing --min-score 70\n  llambo evals --format html --output /tmp/llambo-evals-categories.html\n  llambo evals --validation-receipts /tmp/sealed-local-receipts.json\n  llambo evals --projections ./eval-projections.json\n\n",
	},
}

// normalizeRenderedHelp applies display aliases and strips ignored-argument
// noise from the generated Kong help template.
func normalizeRenderedHelp(rendered string) string {
	help := strings.ReplaceAll(rendered, "llambo models list", "llambo models")
	help = strings.ReplaceAll(help, "llambo models catalog list", "llambo models catalog [provider]")
	help = strings.ReplaceAll(help, "llambo providers list", "llambo providers")
	help = strings.ReplaceAll(help, "llambo route query", "llambo route <query>")
	help = strings.ReplaceAll(help, " [<ignored> ...]", "")
	help = strings.ReplaceAll(help, "Arguments:\n  [<ignored> ...]\n\n", "")
	help = strings.ReplaceAll(help, "Arguments:\n \n\n", "")
	return help
}
