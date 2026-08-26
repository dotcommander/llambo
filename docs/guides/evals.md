# External Evaluation Scores

## 30-second category scorecard

Generate all six cache-only LLAMBO-9 category scores without executing a model or
contacting a remote source:

```bash
llambo evals --offline --rank-by matrix
llambo evals --offline --rank-by coding
llambo evals --offline --format json --output /tmp/llambo-evals.json
```

Use `--live-omlx` only when the scorecard must reflect the OMLX models visible on
loopback now. It refreshes inventory metadata, not external benchmark data, and it
does not run inference:

```bash
llambo evals --live-omlx --rank-by matrix
```

Each model can receive independent scores for agents, coding, instruction
following, long context, reasoning, and writing. There is no overall composite.
Speed, latency, memory, and price remain separate operational facts.

## Export a normalized evidence dataset

Create a cache-only JSONL dataset that joins source-native evidence to the six
LLAMBO category scores:

```bash
llambo evals export normalized \
  --output-dir /tmp/llambo-normalized-evals
```

The export writes a manifest plus JSONL records for sources, models, identity
bridges, source observations, scores, score contributions, operational metrics,
projections, frozen cohorts, and drift diagnostics. It makes no provider call,
runs no local model, queries no live OMLX inventory, and never publishes the
score snapshot.

| Safety property | Contract |
| --- | --- |
| Semantic deduplication | Equivalent `identity + benchmark + revision + direction + score` rows collapse to one record; peers remain in `mirrors` |
| Editorial selection | Benchmark owner > first party > aggregator, then evidence grade, identity confidence, and stable source ID |
| Editorial score ranking | Category score descending, trusted coverage descending, identity confidence descending, then key ascending |
| Score boundary | Operational metrics and local task scores never alter capability scores |
| Admission transparency | Non-admitted source rows remain with `admission_status` and `admission_reason` |
| Publication | The output directory must be new and is published atomically after all artifacts encode |

## Show or republish only OMLX scores

Use the dedicated OMLX view when you want the current score snapshot rather than
the full external-model report:

```bash
# Immediate cached view; no network, provider, or local-model call
llambo evals omlx

# Explicit live admin discovery, OMLX-only rebuild, and snapshot publication
llambo evals omlx --live

# Machine-readable snapshot
llambo evals omlx --format json
```

The cached view reads `~/.config/llambo/llambo-scores.json`. `--live` is the
only mode that queries OMLX or replaces that snapshot. Hidden, helper, virtual,
audio, and embedding models remain excluded and are reported with their IDs.
Markdown cells include confidence and trusted coverage. Missing categories remain
`—`; `ᵉ` is retained only for readable legacy LLAMBO-6 snapshots. The summary
separately counts benchmark-backed, estimated, and unresolved cells.

## Refresh sealed evaluation sources

Source-scoped refresh avoids a full evaluation refresh:

```bash
# All reviewed writing sources
llambo evals sources refresh

# One source
llambo evals sources refresh writingbench

# Explicit comma-separated set
llambo evals sources refresh writingbench,eqbench-creative-v3,lechmazur-writing,arena-creative-writing

# Authenticated LLM Stats model and reviewed-benchmark refresh
llambo evals sources refresh llm-stats-stats-v1
```

The source service fetches into memory, validates every row's source ID, source
class, evidence grade, methodology, version, and content hash, preserves
predecessor caches under the cache `backups/` directory, and only then replaces
the named files. WritingBench, EQ-Bench Creative v3, and Lech Mazur Creative
Story-Writing, and Arena Creative Writing all publish the same normalized
snapshot contract. Arena ingestion reads the official `arena-catalog` JSON
artifact's `creative_writing` category and emits `lmsys-writing` observations;
it does not scrape the rendered leaderboard page.
`llm-stats-stats-v1` performs the same staged replacement for
the complete Stats v1 LLM identity inventory—359 models in the current sealed
collection—and every reviewed benchmark with at least five unique models. It
never runs provider or local-model inference.

## Cost-safe local evaluation

External benchmark evidence comes first. Provider-backed local runs are an explicit exception: execution requires a concrete `--local-use-case`, a per-run `--max-run-cost`, and a shared `--max-campaign-cost` ledger ceiling.

Writing evaluation uses one synchronous structured judge call per generated response. That call returns every rubric criterion together, uses Gemini thinking level `medium`, and is not automatically retried after a paid dispatch or invalid judge JSON. Receipts retain prompt, completion, cache-read, cache-write, and reasoning token counts; Gemini reasoning is already included in completion usage and is never billed twice.

Dry runs remain provider-free and should always be reviewed before adding `--execute`.

```bash
llambo evals
llambo evals --rank-by coding
llambo evals --rank-by writing --limit 100
llambo evals --format json --output /tmp/llambo-evals.json
llambo evals --rank-by coding --format html --output /tmp/llambo-evals-coding.html
llambo evals --rank-by coding --min-score 40 --max-output-price -1
llambo evals --no-omlx
llambo evals --projections ./eval-projections.json
llambo evals writing
llambo evals writing --refresh
llambo evals writing --refresh --format json --output /tmp/llambo-writing.json
```

`llambo evals` transforms sealed cached remote benchmark snapshots into transparent
category rankings. It never executes models, reads historical Llambo results, or
fits against local evaluation targets. External sources refresh only with the
explicit `--refresh` flag. Live loopback inventory refresh is separately explicit
through `--live-omlx`.

Use `--refresh` only when you deliberately want newer source snapshots:

```bash
export AA_API_KEY=your-free-api-key
export LLM_STATS_KEY=your-llm-stats-api-key
llambo evals --refresh
```

When `LLM_STATS_KEY` is present, full benchmark ingestion uses the authenticated
LLM Stats Stats v1 `/v1/models`, `/v1/benchmarks`, and `/v1/scores` endpoints
with HTTP Bearer authentication, cursor pagination, exact-model deduplication,
and percent normalization. Without that key, refresh retains the legacy public
leaderboard adapter. The source-scoped command performs the same Stats v1
ingest without touching WritingBench, EQ-Bench, or Artificial Analysis.

`--offline` disables the loopback OMLX request as well as external source access
and cannot be combined with `--refresh`. `--no-omlx` skips only local discovery.

## Writing benchmark catalog

Use the writing workspace when you want concrete prose tests and source
discovery around the same normalized evaluation evidence:

```bash
# Offline: show the reviewed source registry and current open-weight queue
llambo evals writing

# Deliberately scrape the latest leaderboard and validate public artifacts
llambo evals writing --refresh

# Save machine-readable source-native rows for a rerun or review
llambo evals writing --refresh --format json --output /tmp/llambo-writing.json

# Export normalized prompts for direct model testing
llambo evals writing --refresh --prompt-source writingbench --prompt-limit 20 \
  --export-prompts /tmp/writingbench.jsonl

# Discover recent public text-generation candidates without inventing scores
llambo evals writing --refresh --discover-open-models --discover-limit 25
```

The workspace keeps scores in the scale used by each upstream benchmark. It does
not maintain a parallel scoring system: admitted sealed results flow through the
same source snapshots, identity reconciliation, and LLAMBO-9 family scorer.
The standard source refresh ingests the public
[Lech Mazur creative story-writing leaderboard](https://github.com/lechmazur/writing),
which publishes pairwise comparison scores, estimated win chance, and an
uncertainty range. Its public artifacts include the prompts and generated
stories, so a model can be rerun against the same constrained briefs.

Refresh also checks the registered public artifact URLs. It parses known
machine-readable corpora where possible, including WritingBench's 1,000 JSONL
queries, EQ-Bench Creative Writing v3's 32 prompt records, and IFEval's JSONL
corpus. HTML and repository-backed sources are reported as endpoint-available
when fetched, without pretending that a live page is a reproducible score.
The same refresh verifies every reviewed Hugging Face model ID and records its
remote license, creation time, last modification time, download count, and
gated status. This is a freshness check for the reviewed queue, not an attempt
to discover every new model on Hugging Face.

The Markdown and JSON outputs also include a **Run plan** for each source. It
links the prompt artifact, records known prompt counts, names the expected
generation and judging flow, and calls out reproducibility traps such as judge
model, temperature, serving mode, and multi-stage context.

Use `--export-prompts` when you need records that can be passed directly to a
model runner. The exporter writes one normalized JSON object per line with
`benchmark_id`, `id`, `prompt`, `source_url`, and the original `source_record`.
Supported machine-readable sources are `writingbench`, `eqbench-creative-v3`,
and `ifeval`; use `--prompt-source all` to combine them. `--prompt-limit 0`
exports every parsed record. Export requires `--refresh` because it fetches the
current public artifact and never silently uses a stale local copy.

## Run a bounded local writing evaluation

Dry-run first. This validates the sealed prompt file, exact models, benchmark
records, pricing, call count, and worst-case cost without contacting a provider
or creating the output directory:

```bash
llambo evals writing run \
  --input /tmp/writingbench.jsonl \
  --benchmark writingbench \
  --model deepseek/deepseek-v4-pro \
  --judge-model openrouter/anthropic/claude-sonnet-5 \
  --output-dir /tmp/writing-run
```

Execute only after reviewing that JSON plan:

```bash
llambo evals writing run \
  --input /tmp/writingbench.jsonl \
  --benchmark writingbench \
  --model deepseek/deepseek-v4-pro \
  --judge-model openrouter/anthropic/claude-sonnet-5 \
  --output-dir /tmp/writing-run \
  --campaign-ledger /tmp/writing-campaign.json \
  --execute --max-run-cost 25.00 --max-campaign-cost 50.00
```

### Gemini judges use one synchronous call

The judge combines every criterion for a generated response into one structured
Gemini request. This cuts WritingBench judging from five calls to one and keeps
the judge worker count at one:

```bash
llambo evals writing run \
  --input /tmp/writingbench.jsonl \
  --benchmark writingbench \
  --model omlx/gemma-4-26B-A4B-it-heretic-4bit \
  --judge-model gemini/gemini-3.7-flash \
  --output-dir /tmp/writing-gemini-sync \
  --local-use-case "compare exact local quantization variants"
```

The sealed run identity fixes `judge_layout=combined-v1`,
`judge_thinking_level=medium`, and `judge_concurrency=1`. A failed dispatch or
invalid structured response is preserved as terminal evidence and is not
automatically charged again.

The run is bound to the prompt-file hash, exact model and judge identities,
pricing snapshot, adapter version, settings, and limits. Reusing the same output
directory resumes missing work only; any identity mismatch fails closed.
Generations and judgments are appended to synced JSONL ledgers as they arrive.
An interrupted run remains inspectable and gets a partial receipt when the CLI
can finish artifact generation.

| Safety control | Contract |
| --- | --- |
| Prompt limit | Defaults to 5; allowed range is 1–100 |
| Concurrency | Defaults to 2; maximum 8 and still capped by provider `workers` |
| Timeout | Defaults to 5 minutes per provider call |
| Gemini judge | One synchronous combined-criteria call; medium thinking |
| Judge output | Defaults to 8,192 tokens; override with `--judge-max-output-tokens` |
| Pricing | Unknown input or output prices are rejected |
| Paid execution | Requires `--execute`, `--max-run-cost`, and `--max-campaign-cost`; local targets also require a specific `--local-use-case` |
| Catalog | Never updated automatically; `quality-import.json` is review-only |

Both adapters produce local, non-leaderboard-comparable scores. WritingBench
generates once per prompt, judges all five source checklist criteria, and labels
the result `writingbench/local-checklist`; it is not an upstream WritingBench
leaderboard score. EQ-Bench Creative v3 defaults to three iterations and pins
temperature `0.7` plus `min_p: 0.1` through existing provider request options.
Its result is labeled `eqbench-creative-v3/local-rubric`; it is **not** the
official pairwise Elo, Glicko, or leaderboard score. Selecting the same judge as
an upstream benchmark does not make either local score leaderboard-comparable.

Each output directory contains `manifest.json`, `generations.jsonl`,
`judgments.jsonl`, `report.json`, `report.md`, `quality-import.json`, and
`receipt.json`. Reports are derived locally from the ledgers and can be rebuilt
without provider calls. Catalog import remains a separate explicit command.
Run directories and files are written with owner-only permissions because model
responses and judge reasoning may contain sensitive prompt material.

The current exporter intentionally does not flatten repository-backed or HTML
surfaces such as Lech Mazur's prompt directory, EQ-Bench Longform, Arena
Creative Writing, IFBench, or Writing Styles. Those sources remain listed and
endpoint-checked in the catalog, but their multi-stage or rendered formats need
benchmark-specific adapters before they can be treated as reproducible JSONL.

After a live refresh, **Open-weight leaderboard coverage** matches reviewed
model aliases to the primary leaderboard. `measured` means a public row was
matched. `variant` means the row may use a different reasoning mode or serving
variant. `needs-run` means no score is inferred and the reviewed model still
needs an evaluation run.

The offline registry also points to:

| Source | Best use | Surface |
| --- | --- | --- |
| [WritingBench](https://github.com/X-PLUG/WritingBench) | Real-world professional writing across six domains | Apache-2.0 JSONL query corpus and evaluator artifacts |
| [EQ-Bench Creative Writing v3](https://github.com/EQ-bench/creative-writing-bench) | Creative quality across repeated prompts | Publicly accessible prompt JSON and result archives; upstream currently publishes no license file |
| [EQ-Bench Longform Writing](https://github.com/EQ-bench/longform-writing-bench) | Planning, revision, and narrative consistency | Prompts, rubrics, reports, and leaderboard HTML |
| [Arena Creative Writing](https://arena.ai/leaderboard/text/creative-writing) | Human-preference cross-check | Live rendered leaderboard |
| [IFEval](https://github.com/google-research/google-research/tree/master/instruction_following_eval) and [IFBench](https://github.com/allenai/IFBench) | Strict brief and format adherence | Public prompt data and deterministic checkers |
| [Lech Mazur Writing Styles](https://github.com/lechmazur/writing_styles) | Style fingerprints and diversity | CSV artifacts and story corpus |

Repository access does not grant redistribution rights. Before publishing
exported prompts, generated corpora, or result archives, check the upstream
license and terms for that specific benchmark revision. Llambo stores those
artifacts locally and does not bundle them in this repository.

The model queue includes the latest reviewed public-weight candidates and marks
whether each one is already covered by a public leaderboard or still needs a
writing rerun. It currently includes DeepSeek V4 Flash-0731, Kimi K3, GLM-5.2,
MiniMax-M3, Qwen3.6 27B and 35B A3B, Gemma 4 31B, Mistral Large 3, Xiaomi MiMo
V2.5 Pro, GPT-OSS 20B and 120B, and Qwen3 235B A22B Instruct 2507. The JSON
catalog exposes canonical Hugging Face IDs and license labels. “Public weights”
does not always mean permissive redistribution, so review nonstandard licenses
before shipping a model.

`--discover-open-models` performs a separate bounded Hugging Face query sorted by
`lastModified`. It excludes reviewed IDs, private repositories, gated models,
and non-text-generation rows, then labels the remaining public candidates
`needs-review`. Discovery captures identity, timestamps, downloads, likes,
license tags, and raw tags. It never assigns a writing score, leaderboard
coverage, or a redistribution approval. This keeps freshness useful without
turning an unreviewed model feed into evaluation evidence.

On first use, run `llambo evals --refresh` once to create the snapshots. Llambo
stores them under your operating system's user cache directory at
`llambo/evals/`. Later `llambo evals` runs read those files regardless of age;
they never refresh implicitly. OMLX inventory discovery is opt-in through
`--live-omlx` and is never cached.

## Command options

| Option | Type | Default | Behavior |
| --- | --- | --- | --- |
| `--rank-by` | string | `matrix` | Selects `coding`, `agents`, `reasoning`, `writing`, `instruction-following`, `long-context`, `speed`, or `price`; omit it for the alphabetical matrix |
| `--format` | string | `markdown` | Emits `markdown`/`md`, `json`, or standalone `html` |
| `--limit` | integer | `50` | Limits eligible canonical ranked rows; eligible tracked projections are always included; `0` includes every eligible model |
| `--min-score` | number | `-1` | With a capability `--rank-by`, includes only models at or above this category score; models without the primary are excluded; a negative value disables the filter |
| `--max-output-price` | number | `10` | Excludes canonical models whose highest known source output price exceeds this amount per 1M tokens; unknown prices and local projections remain eligible; a negative value disables the filter |
| `--output`, `-o` | path | stdout | Writes the report atomically to a file |
| `--projections` | path | built-in registry | Replaces the built-in tracked local/OSS projection registry with a JSON file |
| `--validation-receipts` | path | unset | Reads sealed exact-ID local receipt summaries and reports per-category Spearman correlation; never changes scores or ordering |
| `--live-omlx` | boolean | `false` | Explicitly queries the loopback OMLX inventory, filters local projections to its exact live IDs, and uses that inventory for the published snapshot; failure stops the command before publication |
| `--omlx-url` | URL | `http://127.0.0.1:8000` | Loopback OMLX base URL used for live local-model discovery |
| `--refresh` | boolean | `false` | Fetches and replaces external source snapshots |
| `--refresh-official-model-cards` | boolean | `false` | Fetches only the six pinned LiquidAI, Qwen, OpenAI, and Google model cards; LLM Stats, Artificial Analysis, WritingBench, and EQ-Bench remain cache-only. Cannot be combined with `--refresh` or `--offline` |
| `--export-prompts` | path | unset | Writes normalized prompt JSONL; writing mode requires `--refresh` |
| `--prompt-source` | string | `writingbench` | Selects `writingbench`, `eqbench-creative-v3`, `ifeval`, or `all` for prompt export |
| `--prompt-limit` | integer | `0` | Limits exported prompt records; `0` exports all selected records |
| `--discover-open-models` | boolean | `false` | Queries Hugging Face for recent public text-generation candidates; requires `--refresh` |
| `--discover-limit` | integer | `25` | Maximum fresh candidates retained; discovery is bounded to 100 |
| `--offline` | boolean | `false` | Uses cached external snapshots and skips live OMLX discovery |
| `--allow-partial` | boolean | `false` | Allows an LLM Stats-only report when the Artificial Analysis cache or refresh is unavailable |

Use `--format html --output <path>` to write a standalone report for viewing in a
browser. Markdown and JSON may be written to the same `--output` option.

LLM Stats and Artificial Analysis remain the base inventory. WritingBench and
EQ-Bench snapshots are cached separately; offline absence is reported and leaves
writing evidence unavailable rather than failing or inventing a score.

The sixth official-card adapter pins LiquidAI's LFM2.5-8B-A1B revision
`b9aebfcbe28b6cb374042f495d733037550ab146` and admits only its consistent
AIME25, IFEval, IFBench, and Multi-IF rows. BFCLv4 is excluded because the same
revision reports two different target values. Other non-portfolio benchmark
names remain inspectable in the source card but cannot enter LLAMBO scoring.
Only `google/gemma-4-26B-A4B-it` and `google/gemma-4-31B-it` are exact hosted-ID
aliases for the corresponding official Gemma rows; generic gpt-oss IDs and
lookalikes do not inherit high-reasoning evidence.

The default matrix has no capability cutoff and retains the maximum known output
price ceiling of `$10/1M`. Filtering changes only displayed rows, never the
frozen reference or scores. When sources disagree on output price, the higher
quote enforces the ceiling. Canonical rows with unknown prices remain visible;
local projections are exempt because hosted pricing does not describe local
serving cost. Use `--max-output-price -1` to audit every source row.

The Markdown ranking includes `Output $/1M`, the source-native output-token
price per one million tokens. A single available quote is shown directly. If
LLM Stats and Artificial Analysis disagree, the cell shows the minimum–maximum
range instead of averaging unlike prices. `—` means neither source supplied a
price. JSON reports retain each source's raw `output_price` field.

## Tracked local and OSS artifacts

`llambo evals` includes a separate tracked-projections section for local model
artifacts whose upstream identity has been reviewed. These rows inherit admitted
upstream evidence after canonical scoring is complete. Projection confidence
calibrates trusted coverage and confidence; it never changes the raw score.
Projected rows never duplicate a model in the reference population or change
another model's percentile or rank.

With explicit `--live-omlx`, Llambo first reads OMLX `/admin/api/models`, whose `model_type`
distinguishes `llm`/`vlm` text-capable models from ASR, TTS, embeddings, helpers,
and virtual tools. If that endpoint is unavailable, it falls back to
`/v1/models` with conservative filtering. Exact live IDs are intersected with
the reviewed registry; filenames are never guessed into upstream identities.
Live unreviewed, inactive reviewed, and excluded non-text IDs are reported.
If OMLX is unavailable, the command fails before it renders or publishes a
live-filtered score snapshot. Set `OMLX_API_KEY` if the local endpoint requires
bearer authentication. The complete reviewed projection registry is
[`internal/evals/projections.json`](../../internal/evals/projections.json);
the report names any configured source row that is unavailable.

Medium confidence is used for a quantized or packaging variant with a reviewed
base identity. Low confidence is used when a fine-tune or reasoning-mode
difference may materially change behavior. Exact and high-confidence mappings use
multiplier `1.00`, medium uses `0.75`, and low uses `0.50`. A projection is not an
exact measurement of the local artifact, and Llambo does not invent a separate
quantization or fine-tune penalty.
Missing upstream source keys are reported explicitly.

Projected rows omit hosted price, speed, access, and operational raw fields
because they do not describe local serving. Markdown and HTML render them under
**Local projections**; JSON places them in `local_projections`, separate from
canonical `models`, with explicit projection basis and review date.

To replace the built-in registry, pass a versioned JSON file:

```json
{
  "version": 1,
  "projections": [
    {
      "artifact_key": "my-qwen-quant",
      "source_key": "aa:qwen3.6-27b",
      "confidence": "medium",
      "basis": "Reviewed base identity and serving mode.",
      "reviewed_at": "2026-07-10T00:00:00Z"
    }
  ]
}
```

Use a source `key` from `llambo evals --format json --limit 0`. Registry
replacement is local file I/O only and does not refresh either source cache.

## Sources and identity

- [LLM Stats](https://llm-stats.com/leaderboards/open-llm-leaderboard)
  supplies conservative category indexes, benchmark results, price, and speed.
- [Artificial Analysis](https://artificialanalysis.ai/) supplies intelligence,
  coding, agentic, price, and performance fields through its supported API.
- [WritingBench](https://huggingface.co/spaces/WritingBench/WritingBench/blob/main/score.xlsx)
  supplies the rubric-long-form family plus domain, subdomain, and requirement
  details from its versioned XLSX workbook.
- [EQ-Bench Creative Writing v3](https://github.com/EQ-bench/EQ-bench-site)
  supplies pinned `elo_score` corroboration. Its repository may lag the live
  leaderboard, so it never owns or changes the writing score.

Cross-source rows merge only when normalized model names and canonical
organization families match uniquely in both snapshots. Matching normalized
source keys are labeled `exact`; name-and-organization matches with different
source keys are labeled `normalized` and capped at medium confidence. Reasoning effort,
quantization, fine-tune, and `+` qualifiers remain identity-significant.
Ambiguous identities stay separate and are labeled `ambiguous`.

Each writing observation retains its URL, source version or pinned commit,
content SHA-256, fetch time, methodology, judge version, and identity match.

## LLAMBO-9 category formula

`LLAMBO-9-category` reports six independent capability scores. A reviewed
registry assigns every admitted benchmark to exactly one category and one
independent capability family. Each category targets three to five families.

For each represented family, Llambo computes an empirical midrank percentile
against that benchmark's common frozen cohort. The category score is the
unweighted mean of represented family percentiles:

```text
category score = mean(represented-family common-cohort percentiles)
```

Source authority chooses a raw result; it does not choose a tiny source-native
percentile population when a broad common cohort exists. Evidence grade,
partial family coverage, projection identity, and stale state are reported as
trusted coverage and confidence. They do not pull the score toward neutral 50.

LLAMBO-9 preserves the 21 Stats v1 benchmark populations frozen by LLAMBO-8 from sealed artifact
`540bdcdff3b2eae7c816d993950789f15ca1685a7ba09ff679a056dc05478583`:
`arena-hard` (26), `bfcl-v4` (15), `gpqa` (239), `humaneval` (66), `ifbench`
(34), `ifeval` (67), `livecodebench` (75), `livecodebench-v6` (56),
`longbench-v2` (17), `math` (71), `mbpp` (33), `mmlu-pro` (134), `multi-if`
(23), `multipl-e` (13), `osworld` (20), `scicode` (21), `swe-bench-pro` (50),
`swe-bench-verified` (111), `tau-bench-retail` (25), `tau3-bench` (5), and
`writingbench` (15). Benchmarks outside that manifest retain their prior
reviewed owner/first-party cohort lookup.

No eligible canonical evidence renders `—`, never synthetic `0` or `50`. Valid stale cache
entries remain usable and show their age and stale warning. Incompatible revisions
never share a reference population. Mirrored results contribute once; lower-
authority copies are corroboration, and unresolved peer conflicts are quarantined.
Canonical hosted rows remain evidence-only. For reviewed OMLX projection rows,
LLAMBO-9 fills only missing cells from the sealed estimator artifact. Every estimate
is marked `ᵉ`, has zero coverage and trusted coverage, low confidence, no benchmark
families or contributions, and records its method, direct input categories, support,
validation error, and calibration fingerprint. Each target is predicted independently
from the row's original direct scores; estimates never feed other estimates. With no
direct category input or reviewed identity, the frozen category prior is used and
marked `prior-only`. New direct evidence automatically replaces the estimate.

| Category | Reviewed capability families |
| --- | --- |
| Agents | Tool/API orchestration; environment task completion; computer use |
| Coding | Repository editing; live synthesis; function generation; scientific or multilingual coding |
| Instruction following | Verifiable constraints; structured multi-turn adherence; preference-based adherence |
| Long context | Retrieval stress; multi-document reasoning; persistent long-state reasoning |
| Reasoning | Advanced science; competition mathematics; abstraction; broad advanced knowledge |
| Writing | Rubric long form; fiction/creative writing; broad generation; human preference; style calibration |

An official category winner requires evidence from at least two independent
families. A lower-coverage leader is `provisional`. Ranking uses full-precision
score, trusted coverage, then identity confidence; a remaining exact tie produces
co-winners. Estimates participate in provisional ordering and `--min-score`, but
can never receive official-winner status. Operational metrics and task-specific
local evaluations never enter these rankings.

Successful explicit generation atomically replaces the single local category
snapshot at `~/.config/llambo/llambo-scores.json`. A failed run leaves the
current snapshot intact. Older snapshots remain readable, but no longer create
formula-specific archives or publication lineage.

## Read-only local validation

`--validation-receipts` accepts a strict version-1 manifest. Each manifest entry
names a `receipt_path` and its 64-character `receipt_sha256`; relative paths are
resolved beside the manifest and the hash is verified before the sealed receipt
is decoded. Each receipt contains one canonical `model_key`, capability
`category`, finite local `score`, and `identity_match: "exact"`. The diagnostic reports exact-pair sample size and
tie-aware Spearman rank correlation per category. Fewer than eight pairs is
explicitly `insufficient_validation`; local results never fit the formula,
change a score, or reorder the matrix.

## Replacing the prose and writing evals

Use the `writing` profile instead of running the retired local prose suite:

```bash
llambo evals --rank-by writing
llambo evals --rank-by writing --format json --output /tmp/llambo-writing.json
```

The writing category combines only the writing families admitted by the reviewed
LLAMBO-9 registry. WritingBench supplies rubric-long-form evidence; EQ-Bench
Creative v3 supplies an independent creative-writing family when its frozen
revision is compatible. Local writing runs remain separate diagnostics and never
alter the score.

## Trust and drift

LLAMBO-9 retains the reference snapshot for drift diagnostics, including source
model counts, Artificial Analysis index version, and fingerprints. A refresh does
not silently redefine the pinned scoring cohorts.

The report marks reference drift when:

- the Artificial Analysis index version changes;
- a source model count changes by more than 20%;
- active metric coverage changes by more than 10 percentage points; or
- a metric distribution has a two-sample KS statistic above 0.15.

## Interpretation

Llambo Scores are relative external-evidence percentiles, not probabilities of
task success. Use the category matching the workload; the default alphabetical
matrix intentionally makes no implicit cross-category ranking.
