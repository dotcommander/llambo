# CLI

```bash
llambo ping --models healthy -P nvidia
llambo prompt --models tag:smart "Explain this in one paragraph"
llambo models catalog --free --json
```

Use the CLI to inspect providers, discover working models, run health checks,
fan out prompts, replay routing events, and test the gateway.

## Install The Local Binary

When you build from this repo, update the PATH-visible binary:

```bash
go build -o llambo . && ln -sf $(pwd)/llambo ~/go/bin/llambo
```

Most commands below assume `llambo` resolves to `~/go/bin/llambo`.

## Command Map

```bash
llambo config init
llambo config show

llambo providers
llambo providers refresh [provider...]

llambo models
llambo models --available --timeout-seconds 5
llambo models catalog [provider] --free --json
llambo models catalog openrouter --metadata
llambo models catalog import-quality ./quality.json
llambo models discover-free -P nvidia --pin

llambo evals
llambo evals --refresh --output /tmp/llambo-evals.md
llambo evals --rank-by coding --format html --output /tmp/llambo-evals-coding.html
llambo evals --rank-by coding
llambo evals --min-overall -1 --max-output-price -1
llambo evals --no-omlx
llambo evals --projections ./eval-projections.json
llambo evals writing --refresh --format json --output /tmp/llambo-writing.json
llambo evals writing --refresh --prompt-source writingbench --prompt-limit 20 --export-prompts /tmp/writingbench.jsonl
llambo evals writing --refresh --discover-open-models --discover-limit 25

llambo ping --models healthy -P nvidia
llambo ping --models category:long_context -P openrouter
llambo prompt --models free "Reply with exactly OK"

llambo serve --host 127.0.0.1 --port 8080
llambo jobs run --server http://127.0.0.1:8080 --count 5
llambo jobs stress --server http://127.0.0.1:8080 --jobs 2 --requests-per-job 10

llambo route simulate --limit 100 --modes fastest,balanced
llambo route canary start --provider openai --traffic 0.1
```

## Model Selectors

`ping` and `prompt` both accept `--models`:

| Selector | Meaning |
| --- | --- |
| `all` | Catalog models for enabled providers |
| `free` | Models with explicit zero input and output prices in `model-costs.csv` or provider metadata |
| `pinned` | Catalog models marked pinned |
| `healthy` | Models with a successful health check and no active quarantine |
| `tag:<name>` | Models with a manual catalog tag |
| `category:<name>` | Derived/manual categories such as `speed`, `free`, `healthy`, `long_context`, `tools`, `structured_outputs`, `reasoning`, or a tag |

Add `-P` or `--provider` to filter providers after selector resolution:

```bash
llambo ping --models free -P gemini,nvidia
llambo prompt --models category:speed -P nvidia "Give me a short answer"
llambo prompt --models category:tools -P openrouter "Call tools only if needed"
```

For the common compare-and-synthesize workflow, use the prompt shortcut:

```bash
llambo prompt --smart "Compare these approaches and recommend one"
```

`--smart` expands to the usual healthy-model comparison plus fusion:
`--models healthy --fuse`. Explicit flags still win, so
`llambo prompt --smart --models tag:cheap "..."` keeps the fusion preset but
uses the `tag:cheap` selector.

To make the same behavior the daily default without typing extra flags, set:

```bash
export LLAMBO_PROMPT_MODELS=healthy
export LLAMBO_PROMPT_FUSE=true
export LLAMBO_PROMPT_FUSE_MODELS=zai/GLM-5.2
```

Quarantined models are skipped by default. Use `--include-quarantined` only when
debugging a failing or slow model.

## Free Model Discovery

Free means both input and output price are explicitly `0` in:

```text
~/.config/llambo/model-costs.csv
```

Provider metadata can also mark a model as free when both input and output
prices are explicit zeroes. Unknown prices are not free.

Run discovery and health checks:

```bash
llambo models discover-free -P nvidia
llambo models discover-free -P gemini --pin
```

The command refreshes the catalog, selects explicit zero-price models, pings
them, and records health. It does not delete broken models. It records failures
and quarantines models that fail three consecutive checks or take more than five
seconds.

## Catalog Tags And State

```bash
llambo models catalog --free
llambo models catalog openrouter --metadata
llambo models catalog --tag smart --json

llambo models catalog tag nvidia openai/gpt-oss-120b smart
llambo models catalog untag nvidia openai/gpt-oss-120b smart

llambo models catalog pin nvidia openai/gpt-oss-120b
llambo models catalog avoid nvidia z-ai/glm4.7 --reason "provider returned 410"
llambo models catalog unavoid nvidia z-ai/glm4.7
```

Catalog entries live in `~/.config/llambo/catalog.json` and may include:

- manual tags
- pinned and avoid flags
- provider metadata such as context length, supported parameters, reasoning, and price
- imported task quality evidence
- last ping success/failure
- latency and token counts
- error category
- failure count
- `quarantine_until`

## OpenRouter Metadata And Quality Evidence

```bash
llambo providers refresh openrouter
llambo models catalog openrouter --metadata
llambo models catalog import-quality ./quality.json
llambo models catalog pin openrouter qwen/qwen3-30b-a3b-instruct-2507
```

`providers refresh openrouter` reads OpenRouter model metadata into the catalog.
`--metadata` shows context length, supported parameter count, reasoning mode,
imported quality, tags, and status.

Use `import-quality` when you have benchmark evidence for a task:

```json
[
  {
    "provider": "openrouter",
    "model": "qwen/qwen3-30b-a3b-instruct-2507",
    "task": "extraction",
    "score": 1.0,
    "source": "distill 86-fact benchmark"
  }
]
```

Scores must be between `0` and `1`. Imported task names are also added as
catalog tags. When `routing.catalog_models` is `pinned`, pinned catalog models
carry imported quality, task tags, metadata-derived capabilities, and metadata
pricing into route simulation and runtime routing.

## Ping

```bash
llambo ping --models healthy -P nvidia
llambo ping --models free --include-quarantined -P gemini
llambo ping --models healthy --output /tmp/ping.json --record-routing-metrics
```

`ping` sends a small arithmetic prompt by default. It writes catalog health
metadata after each run. If health writeback fails, the command prints a warning
but still reports the provider results.

For model-quality comparisons without running local probes, use `llambo evals`.
It transforms cached external source metrics into frozen-reference percentile
scores without local evaluation targets. Cache-only operation is the default;
`--refresh` is the only mode that fetches source data. Use `--rank-by` to select
a task profile. See [External Evaluation Scores](evals.md).

## Prompt Fanout

```bash
llambo prompt --models healthy -P nvidia "Reply with exactly OK"
printf 'Reply with exactly STDINOK\n' | llambo prompt --models healthy -P nvidia
llambo prompt --models tag:smart --system "Be concise." "Summarize this"
llambo prompt --models free --fuse "Compare these options and give the best answer"
llambo prompt --models tag:smart --fuse --fuse-control "Compare direct vs fused answers"
llambo prompt --models free --output /tmp/prompt.md "Compare the selected models"
```

`prompt` accepts the prompt as positional text or stdin. It fans out to all
selected models and returns a markdown comparison. Add `--fuse` to turn the
first pass into diverse draft collection: each selected model gets a different
lens such as practical operator, creative strategist, skeptical reviewer, or
domain specialist. Successful drafts are then sent to a final fusion model,
which appends one synthesized answer. The fusion model selector defaults to
`zai/GLM-5.2`; override it with `--fuse-models <selector>`.

Use `--fuse-control` to prove whether fusion helped. It runs the same fusion
model directly on the original prompt and prints a `Direct Fusion Model Control`
section before the fused answer, so the direct answer and merge-fuse answer can
be judged side by side.

## Cost Filters

Both `ping` and `prompt` expose price-aware filters:

```bash
llambo ping --models all --max-output-cost 1.00
llambo prompt --free-only "Use only explicit zero-price models"
llambo ping --models all --include-unknown-cost --max-output-cost 5.00
```

Cost labels are `free`, `paid`, or `unknown`. Explicit rows in
`model-costs.csv` take precedence; provider metadata is used when no explicit
row is present. Budget visibility is advisory and filter-based; configured
budgets are not hard-blocked by these commands.

## Jobs And Server Commands

Start a gateway:

```bash
llambo serve --host 127.0.0.1 --port 8080
```

In another terminal:

```bash
llambo jobs run --server http://127.0.0.1:8080 --count 3
llambo jobs run --server http://127.0.0.1:8080 --count 1 --wait=false
llambo jobs stress --server http://127.0.0.1:8080 --jobs 2 --requests-per-job 5
```

Use longer `--timeout` values for live cloud models. Slow providers can take
more than 30 seconds.

## Routing Tools

Query routing decisions for a prompt string:

```bash
llambo route query "Write a fast Go routine to parse JSON"
```

Replay route events:

```bash
llambo route simulate --limit 100 --modes fastest,cheapest,balanced,quality
llambo route simulate --report /tmp/routing-backtest.md
```

Simulation prints sample decision reasons with quality, runtime quality, task
fit, estimated latency, estimated cost, and reliability. Use those reasons to
check whether imported quality evidence or metadata-derived price changed a
mode's choices.

Manage canaries:

```bash
llambo route canary start --provider openai --traffic 0.1 --promote-after 100
llambo route canary status
llambo route canary promote
llambo route canary stop
```

Canary commands mutate `~/.config/llambo/config.json`.
