# Prose-screen evaluation

> **TL;DR:** `prose-screen` is a sealed, evidence-backed pre-screen for writer
> models. It measures prose quality; it does not prove facts, source fidelity,
> semantic quality, or promotion readiness.

## 🚀 30-second dry run

Set exact configured model IDs, then run the provider-free plan:

```bash
WRITER='provider/exact-writer-model'
JUDGE='gemini/exact-judge-model'

llambo evals writing run \
  --input internal/evals/testdata/prose-screen-fixtures.jsonl \
  --benchmark prose-screen \
  --model "$WRITER" \
  --judge-model "$JUDGE" \
  --output-dir .work/prose-screen-dry-run \
  --campaign-ledger .work/writing-campaign.json \
  --limit 1
```

The default dry run seals the manifest, identities, controls, call count, and
worst-case cost without contacting a provider.

## ✅ What it measures

| Dimension | Checks |
| --- | --- |
| Depth and development | Mechanisms, implications, and examples |
| Structural coherence | Sequence, headings, transitions, and paragraph flow |
| Prose craft | Precision, clarity, rhythm, economy, and concrete language |
| Anti-repetition | Repeated claims, recap padding, filler, and AI clichés |
| Target-reader fit | Usefulness for the stated public reader profile |
| Mechanics | Grammar, spelling, formatting, placeholders, and unfinished artifacts |

Each judgment must provide:

- A score from 1 through 5.
- Explicit applicability.
- One concise rationale.
- One or more zero-based, end-exclusive UTF-8 byte ranges into the exact prose.

Mechanics is always applicable because it owns the artifact cap.

Llambo rejects unknown or duplicate fields, trailing JSON, missing dimensions,
invalid scores, and invalid evidence spans. The judge does not supply an
aggregate. Go computes the versioned weighted aggregate and caps it at the
Mechanics score, preventing fluent text from hiding production artifacts.

## 🔒 Public-input boundary

Every `source_record` must contain exactly these public string fields:

```json
{
  "prompt": "Write a practical deployment note.",
  "task": "Explain a safe deployment workflow.",
  "reader_profile": "An on-call application engineer."
}
```

Do not include private source material, reader dossiers, TLDW content, or extra
metadata. The adapter rejects additional and duplicate source fields.

## 🧠 Candidate reasoning controls

| Goal | Flag |
| --- | --- |
| Preserve model defaults | Omit reasoning flags |
| Set a reasoning level | `--reasoning-effort off\|low\|medium\|high` |
| Set a thinking budget | `--thinking-budget-tokens 32768` |
| Raise a proven-small output cap | `--generation-max-output-tokens 32768` |

`--reasoning-effort off` sends all OMLX controls required to suppress thinking:
`reasoning_effort: off`, `thinking_budget: 0`, and
`chat_template_kwargs.enable_thinking: false`.

A positive thinking budget enables thinking and cannot be combined with
`--reasoning-effort off`. Reasoning and output-token overrides are sealed into
the run identity, so changing either produces a different run ID.

Generation and judgment temperatures are explicitly fixed at `0` for this
benchmark. The judge output cap defaults to `8192`; an explicit
`--judge-max-output-tokens` value may replace it.

## 💾 Executed-run artifacts

An authorized `--execute` run preserves:

- Dispatch intents before every provider call.
- Generation and raw-judgment JSONL ledgers.
- Usage, accounting, and exact served identities.
- Capped per-case aggregates and model dispersion.
- Failure records and an immutable completion receipt.

A one-case result is marked `insufficient sample`. Complete prose-screen runs
are marked `complete_pre_screen` and never emit quality-import candidates.

## ⚠️ Boundary

TLDW remains authoritative for facts and relationships, source overlap, outline
retention, contradiction checks, semantic qualification, blind reader review,
and final writer promotion.

## 🛠️ Troubleshooting

| Symptom | Action |
| --- | --- |
| Source-record rejection | Keep only `prompt`, `task`, and `reader_profile` |
| Invalid evidence span | Use UTF-8 byte offsets, not rune or character indexes |
| Truncated judgment | Raise the judge cap only in a new sealed run |
| Output directory already completed | Preserve it and choose a new directory |
| Reasoning flags conflict | Remove the thinking budget when effort is `off` |
