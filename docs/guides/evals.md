# External Evaluation Scores

```bash
llambo evals
llambo evals --rank-by coding
llambo evals --rank-by value --limit 100
llambo evals --format json --output /tmp/llambo-evals.json
llambo evals --rank-by coding --format html --output /tmp/llambo-evals-coding.html
llambo evals --min-overall -1 --max-output-price -1
llambo evals --no-omlx
llambo evals --projections ./eval-projections.json
llambo evals writing
llambo evals writing --refresh
llambo evals writing --refresh --format json --output /tmp/llambo-writing.json
```

`llambo evals` transforms cached LLM Stats and Artificial Analysis snapshots
into transparent, task-specific rankings. It never executes models, reads
historical Llambo results, or fits against local evaluation targets. Plain
`llambo evals` does make a bounded loopback request to OMLX so the local section
contains only currently available text-capable artifacts. It does not refresh
either external source.

Use `--refresh` only when you deliberately want newer source snapshots:

```bash
export AA_API_KEY=your-free-api-key
llambo evals --refresh
```

`--offline` disables the loopback OMLX request as well as external source access
and cannot be combined with `--refresh`. `--no-omlx` skips only local discovery.

## Writing benchmark catalog

Use the separate writing catalog when you want concrete prose tests rather than
the LES-1 ranking report:

```bash
# Offline: show the reviewed source registry and current open-weight queue
llambo evals writing

# Deliberately scrape the latest leaderboard and validate public artifacts
llambo evals writing --refresh

# Save machine-readable source-native rows for a rerun or review
llambo evals writing --refresh --format json --output /tmp/llambo-writing.json
```

The catalog keeps scores in the scale used by each upstream benchmark. It does
not average, percentile-transform, or inject them into `llambo evals` LES-1
scores. Refresh scrapes the public
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

After a live refresh, **Open-weight leaderboard coverage** matches reviewed
model aliases to the primary leaderboard. `measured` means a public row was
matched. `variant` means the row may use a different reasoning mode or serving
variant. `needs-run` means no score is inferred and the reviewed model still
needs an evaluation run.

The offline registry also points to:

| Source | Best use | Surface |
| --- | --- | --- |
| [WritingBench](https://github.com/X-PLUG/WritingBench) | Real-world professional writing across six domains | Public JSONL query corpus and evaluator artifacts |
| [EQ-Bench Creative Writing v3](https://github.com/EQ-bench/creative-writing-bench) | Creative quality across repeated prompts | Prompt JSON and published result archives |
| [EQ-Bench Longform Writing](https://github.com/EQ-bench/longform-writing-bench) | Planning, revision, and narrative consistency | Prompts, rubrics, reports, and leaderboard HTML |
| [Arena Creative Writing](https://arena.ai/leaderboard/text/creative-writing) | Human-preference cross-check | Live rendered leaderboard |
| [IFEval](https://github.com/google-research/google-research/tree/master/instruction_following_eval) and [IFBench](https://github.com/allenai/IFBench) | Strict brief and format adherence | Public prompt data and deterministic checkers |
| [Lech Mazur Writing Styles](https://github.com/lechmazur/writing_styles) | Style fingerprints and diversity | CSV artifacts and story corpus |

The model queue includes the latest reviewed public-weight candidates and marks
whether each one is already covered by a public leaderboard or still needs a
writing rerun. It currently includes DeepSeek V4 Flash-0731, Kimi K3, GLM-5.2,
MiniMax-M3, Qwen3.6 27B and 35B A3B, Gemma 4 31B, Mistral Large 3, Xiaomi MiMo
V2.5 Pro, GPT-OSS 20B and 120B, and Qwen3 235B A22B Instruct 2507. The JSON
catalog exposes canonical Hugging Face IDs and license labels. “Public weights”
does not always mean permissive redistribution, so review nonstandard licenses
before shipping a model.

On first use, run `llambo evals --refresh` once to create the snapshots. Llambo
stores them under your operating system's user cache directory at
`llambo/evals/`. Later `llambo evals` runs read those files regardless of age;
they never refresh implicitly. Local OMLX discovery is live and is not cached.

## Command options

| Option | Type | Default | Behavior |
| --- | --- | --- | --- |
| `--rank-by` | string | `overall` | Selects `overall`, `general`, `coding`, `reasoning`, `agents`, `writing`, `long-context`, `speed`, `value`, or `price` |
| `--format` | string | `markdown` | Emits `markdown`/`md`, `json`, or standalone `html` |
| `--limit` | integer | `50` | Limits eligible canonical ranked rows; eligible tracked projections are always included; `0` includes every eligible model |
| `--min-overall` | number | `40` | Includes only models with overall score at least this value; models without overall are excluded; a negative value disables the filter |
| `--max-output-price` | number | `10` | Excludes canonical models whose highest known source output price exceeds this amount per 1M tokens; unknown prices and local projections remain eligible; a negative value disables the filter |
| `--output`, `-o` | path | stdout | Writes the report atomically to a file |
| `--projections` | path | built-in registry | Replaces the built-in tracked local/OSS projection registry with a JSON file |
| `--omlx-url` | URL | `http://127.0.0.1:8000` | Loopback OMLX base URL used for live local-model discovery |
| `--no-omlx` | boolean | `false` | Skips live OMLX discovery and uses the reviewed registry as-is |
| `--refresh` | boolean | `false` | Fetches and replaces external source snapshots |
| `--offline` | boolean | `false` | Uses cached external snapshots and skips live OMLX discovery |
| `--allow-partial` | boolean | `false` | Allows an LLM Stats-only report when the Artificial Analysis cache or refresh is unavailable |

Use `--format html --output <path>` to write a standalone report for viewing in a
browser. Markdown and JSON may be written to the same `--output` option.

Without `--allow-partial`, both cached snapshots are required. LLM Stats is
always required because it supplies the broader profile inventory.

The default eligibility policy is `overall >= 40` and maximum known output
price `$10/1M`. It is applied after LES-1 scores, drift, and stability are
calculated, so it changes only which rows are shown—not the formula population,
percentiles, or scores. When the two sources disagree on output price, the
higher quote enforces the ceiling. Canonical rows with unknown prices remain
visible and are counted in diagnostics. Local projections are exempt because a
hosted API quote does not describe local serving cost. Use both
`--min-overall -1 --max-output-price -1` when auditing every source row.

The Markdown ranking includes `Output $/1M`, the source-native output-token
price per one million tokens. A single available quote is shown directly. If
LLM Stats and Artificial Analysis disagree, the cell shows the minimum–maximum
range instead of averaging unlike prices. `—` means neither source supplied a
price. JSON reports retain each source's raw `output_price` field.

## Tracked local and OSS artifacts

`llambo evals` includes a separate tracked-projections section for local model
artifacts whose upstream identity has been reviewed. These rows reuse the
upstream model's LES-1 scores after the canonical population, drift, and
jackknife calculations are complete. They never duplicate a model in the
reference population or change another model's percentile or rank.

By default, Llambo first reads OMLX `/admin/api/models`, whose `model_type`
distinguishes `llm`/`vlm` text-capable models from ASR, TTS, embeddings, helpers,
and virtual tools. If that endpoint is unavailable, it falls back to
`/v1/models` with conservative filtering. Exact live IDs are intersected with
the reviewed registry; filenames are never guessed into upstream identities.
Live unreviewed, inactive reviewed, and excluded non-text IDs are reported.
If OMLX is unavailable, local projections are omitted rather than presented as
live. Set `OMLX_API_KEY` if the local endpoint requires bearer authentication.

The built-in registry currently tracks:

| Artifact | External source row | Projection confidence |
| --- | --- | --- |
| `Qwen3.6-27B-MLX-4bit` | AA Qwen3.6 27B reasoning | medium |
| `Qwen3.6-35B-A3B-oQ4-fp16-mtp` | AA Qwen3.6 35B A3B reasoning | medium |
| `gemma-4-26B-A4B-it-heretic-4bit` | AA Gemma 4 26B A4B reasoning | low |
| `gpt-oss-20b-MXFP4-Q8` | merged GPT OSS 20B High | low |
| `granite-4.1-8b-nvfp4` | AA Granite 4.1 8B | medium |
| `Qwen-AgentWorld-35B-A3B-oQ4-MLX` | LLM Stats Qwen3.6 35B A3B | low |

Medium confidence is used for a quantized or packaging variant with a reviewed
base identity. Low confidence is used when a fine-tune or reasoning-mode
difference may materially change behavior. Projection confidence caps the
copied score confidence. A projection is not an exact measurement of the local
artifact, and Llambo does not invent a quantization or fine-tune penalty.
Missing upstream source keys are reported explicitly.

Projected rows omit hosted price, speed, value, access, and operational raw
fields because they do not describe local serving. JSON retains capability
fields and explicit `projection` provenance, including review basis and date,
for auditability.

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

Cross-source rows merge only when normalized model names and canonical
organization families match uniquely in both snapshots. Matching normalized
source keys are labeled `exact`; name-and-organization matches with different
source keys are labeled `normalized` and capped at medium confidence. Reasoning effort,
quantization, fine-tune, and `+` qualifiers remain identity-significant.
Ambiguous identities stay separate and are labeled `ambiguous`.

## LES-1 formula

Formula `LES-1` converts every metric to an empirical
percentile against a checked-in frozen reference distribution. Higher is better
for capability and throughput; lower is better for price and latency. Ties use
midranks.

Within each source and domain, an absent metric contributes a neutral 50.
Missing evidence therefore widens the exact `[low, high]` evidence interval but
does not become zero. A source component requires at least 50% metric coverage.
When a unique exact or normalized identity has both sources, its domain scores
are averaged and the absolute disagreement is reported separately. Normalized
matches are capped at medium confidence.

Capability weights are fixed policy, never learned coefficients:

| Domain | Artificial Analysis | LLM Stats |
| --- | --- | --- |
| General | intelligence 60%, coding 20%, agentic 20% | general 50%, reasoning 25%, instruction following 15%, factuality 10% |
| Coding | coding 60%, intelligence 25%, agentic 15% | code 50%, SWE-bench Verified 30%, SWE-bench Pro 20% |
| Reasoning | intelligence 55%, coding 25%, agentic 20% | reasoning 60%, general 20%, instruction following 10%, structured output 10% |
| Agents | agentic 60%, intelligence 25%, coding 15% | agents 45%, tool calling 35%, reasoning 10%, structured output 10% |
| Writing | — | writing 35%, creativity 25%, language 20%, communication 15%, instruction following 5% |
| Long context | — | long context 50%, instruction following 20%, factuality 15%, grounding 15% |

The default overall score is:

```text
25% coding + 20% general + 20% reasoning + 20% agents
+ 10% writing + 5% long context
```

An unavailable domain contributes 50. Overall requires at least 60% weighted
coverage. Speed and price are deliberately separate from capability. `value`
combines 75% overall capability with 25% price percentile.

Supported `--rank-by` values are `overall`, `general`, `coding`, `reasoning`,
`agents`, `writing`, `long-context`, `speed`, `value`, and `price`.

## Replacing the prose and writing evals

Use the `writing` profile instead of running the retired local prose suite:

```bash
llambo evals --rank-by writing
llambo evals --rank-by writing --format json --output /tmp/llambo-writing.json
```

The writing score combines LLM Stats writing (35%), creativity (25%), language
(20%), communication (15%), and instruction following (5%). The report keeps
coverage, evidence bounds, and rank sensitivity beside the score. Artificial
Analysis does not currently provide a writing metric, so this profile is based
on LLM Stats evidence and may omit models without enough writing coverage. It
is an external replacement ranking, not a reconstruction of the retired local
prose score.

## Trust and drift

LES-1 embeds the source distributions from its reference snapshot, including
model counts, Artificial Analysis index version, and source fingerprints. A
refresh does not silently redefine old percentiles.

The report marks reference drift when:

- the Artificial Analysis index version changes;
- a source model count changes by more than 20%;
- active metric coverage changes by more than 10 percentage points; or
- a metric distribution has a two-sample KS statistic above 0.15.

Every ranking also runs a deterministic metric jackknife. Each active metric is
removed globally and its remaining source weights are renormalized. The report
records per-model rank span plus mean Kendall agreement and worst top-20
overlap. Formula stability requires mean Kendall at least 0.90 and top-20
overlap at least 0.80.

With the current frozen snapshots, `overall`, `general`, `coding`, `reasoning`,
`value`, and `price` meet that stability gate. `agents`, `writing`,
`long-context`, and `speed` remain available but are labeled unstable because
their external evidence is more sensitive to individual metrics. Treat that
label as a warning, not as a score penalty.

## Interpretation

LES scores are relative external-evidence percentiles, not probabilities of
task success and not reconstructions of retired local suites. Coverage measures
evidence completeness, cross-source disagreement measures consensus, and rank
span measures sensitivity to formula inputs. Keep these diagnostics separate
from the score itself.

Use a task-specific profile whenever the workload is narrower than the balanced
default. Use source-native values when a particular benchmark is the actual
decision criterion.
