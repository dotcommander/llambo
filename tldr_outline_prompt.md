Plan the structure and assemble the complete factual ingredients for a
restrained, precise, source-grounded TLDR. The outline serves as the writer's
complete meaning ledger: provide all necessary facts, identifiers, mechanisms,
and quantitative details in structured form so the writer has every essential
ingredient ready to synthesize without hunting or omissions. Do not write the
finished prose.

Choose exactly one structure from the source:
- CHEATSHEET — discrete facts, commands, settings, definitions, or lookups.
- RECIPE / WORKFLOW — actions that must be performed in a meaningful order.
- BLOG POST — an explanation, argument, chronology, or story.

## Output contract
- Return Markdown only. Start immediately with one plain, specific `# ` title.
- On the next line write exactly one of: `Format: CHEATSHEET`,
  `Format: RECIPE / WORKFLOW`, or `Format: BLOG POST`.
- Add one plain, descriptive `## ` heading per section. Do not use witty
  colon-subtitles or parenthetical asides in headings.
- Under each `## ` heading, write concise bullet points (`- `) listing what that
  section must cover. Write these coverage bullets as terse factual atoms and
  underlying mechanisms, not article prose or source-shaped sentences.
- Use only as many sections as the source needs, normally 4–8.
- Do not include citations or `[Source N]` tags.
- Do not use em dashes (`—`). Use colons or standard dashes (`- `).

## Planning method
- For a BLOG POST, silently form a story spine of 3–4 source-backed beats: the
  strongest true hook, the mechanism or sequence, a supported human angle,
  contrast, or consequence, and the unresolved issue or supported conclusion.
- For every format, silently form a meaning ledger of the load-bearing claims,
  relationships, attributions, qualifications, named safeguards, exact required
  anchors, and open questions that the finished piece cannot lose.
- Do not output either guide separately. Use them only to choose sections and
  coverage bullets.

## Coverage, Entities and Precision
- Assign every material source fact to a section. Keep exact names, dates,
  numbers, commands, identifiers, qualifications, conflicts, and open questions
  in the coverage bullets.
- **Entity & Phonetic Standardization**: When processing spoken transcripts with
  phonetic speech-to-text slips of established entities, standard physical terms,
  or scientific names (e.g. "vacuum beer fringing" -> "vacuum birefringence",
  "Roberto Mcnani" -> "Roberto Mignani", "X-ray polarry explorer" -> "NASA's IXPE /
  X-ray Polarimetry Explorer"), standardize them to their canonical technical
  names and identifiers. Mark truly unidentifiable terms as `[unclear in source]`.
- **Quantitative & Physical Anchors**: Never omit concrete quantitative anchors,
  physical dimensions, thresholds, durations, or unit-bearing measurements
  (e.g., `Tesla`, `km`, `light-years`, `seconds`, `%`, `radii`). Record every
  quantitative value and threshold precisely.
- Retain all underlying technical, physical, or operational mechanisms,
  mathematical thresholds, and quantitative measurements without compressing
  them away into vague generalizations.
- Retain all named missions, observatories, instruments, algorithms, physical
  scales, and key contributors.
- Preserve each proposition and its epistemic role, not merely its topic. Keep a
  question, possibility, proposal, observation, or conclusion in that role.
  Distinguish observation, plausible contributor, and established cause.
- When the source contains superseding revisions or draft notes, outline only
  final approved categories and values, ignoring obsolete drafts.
- For a CHEATSHEET or RECIPE / WORKFLOW, put each action or topic label in its
  section heading once; make the coverage bullets add details instead of restating
  the label.
- Do not calculate or headline a duration, total, percentage, severity, or other
  derived fact unless the source explicitly states it. Preserve the source's
  stated facts instead of adding arithmetic or inferred labels.
- Preserve source order when order carries meaning: counted or ranked lists,
  procedures, and chronologies. Otherwise lead with the most important fact.
- For a dense chronology, retain every critical pivot and its order, but group
  related facts around trigger, consequence, diagnosis, intervention, outcome,
  and unresolved issue instead of creating one section per timestamp.
- When repeated measurements establish the same trend, plan one representative
  measurement plus the trend. Retain every distinct threshold, reversal,
  failure, recovery, or other fact that changes the interpretation.
- For a counted or enumerated list of N items, create exactly N item sections in
  the same order. Never merge, rename, reorder, or drop an item. If the source
  includes a standalone condition or rule outside the numbered items, give it a
  distinct section rather than conflating it with the counted rules. Add an
  introduction or conclusion only when the source supports one.

If the source is too thin for a useful piece, return the title, format line, and
one `## ` section stating what can be recovered, then stop.

Output the outline only.
