package evals

import (
	"fmt"
	"html"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RenderHTML renders a standalone browser-friendly report. It deliberately
// keeps CSS and JavaScript inline so the output can be opened from a file or
// shared without a companion asset directory or network access.
func RenderHTML(report Report, limit int) string {
	canonicalRows, projectedRows := partitionProjectionRows(report.Models)
	if limit <= 0 || limit > len(canonicalRows) {
		limit = len(canonicalRows)
	}

	var b strings.Builder
	b.Grow(32 * 1024)
	b.WriteString(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>Llambo evaluations · `)
	writeEscaped(&b, report.RankingProfile)
	b.WriteString(`</title>
<style>
:root {
  color-scheme: light dark;
  --bg: #f4f7fb;
  --panel: #ffffff;
  --panel-muted: #edf2f7;
  --ink: #122033;
  --muted: #5f7187;
  --line: #d8e1eb;
  --line-strong: #b9c8d8;
  --accent: #0b7285;
  --accent-strong: #075985;
  --accent-soft: #d9f2f5;
  --positive: #13795b;
  --warning: #a15c00;
  --shadow: 0 18px 50px rgba(18, 32, 51, 0.10);
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #0d1622;
    --panel: #142231;
    --panel-muted: #1b2d3f;
    --ink: #e7eff7;
    --muted: #a8bacb;
    --line: #294155;
    --line-strong: #3d5a70;
    --accent: #67d5df;
    --accent-strong: #8bd8ff;
    --accent-soft: #173d49;
    --positive: #65d6a6;
    --warning: #ffc26b;
    --shadow: 0 18px 50px rgba(0, 0, 0, 0.28);
  }
}
* { box-sizing: border-box; }
html { background: var(--bg); }
body {
  margin: 0;
  color: var(--ink);
  background:
    radial-gradient(circle at 10% 0%, rgba(11, 114, 133, 0.11), transparent 31rem),
    var(--bg);
  font-family: ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  line-height: 1.5;
}
a { color: var(--accent-strong); }
.wrap { width: min(1500px, calc(100% - 32px)); margin: 0 auto; padding: 32px 0 56px; }
.hero {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 24px;
  align-items: end;
  padding: 28px 30px;
  color: #f5fbff;
  background: linear-gradient(135deg, #102a43, #075985 65%, #0b7285);
  border-radius: 22px;
  box-shadow: var(--shadow);
}
.eyebrow {
  color: #a6edf1;
  font: 700 0.74rem/1.2 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  letter-spacing: 0.16em;
  text-transform: uppercase;
}
h1 { margin: 9px 0 8px; font-size: clamp(2rem, 4vw, 3.8rem); line-height: 0.98; letter-spacing: -0.045em; }
.hero p { max-width: 780px; margin: 0; color: #d9f5f7; }
.hero-meta { text-align: right; color: #d9f5f7; font-size: 0.9rem; }
.hero-meta strong { display: block; color: #fff; font-size: 1.7rem; line-height: 1.1; }
.stats { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; margin: 16px 0; }
.stat, .panel {
  background: var(--panel);
  border: 1px solid var(--line);
  border-radius: 16px;
  box-shadow: 0 8px 24px rgba(18, 32, 51, 0.045);
}
.stat { padding: 16px 18px; }
.stat-label { color: var(--muted); font-size: 0.76rem; font-weight: 700; letter-spacing: 0.08em; text-transform: uppercase; }
.stat-value { margin-top: 5px; font-size: 1.35rem; font-weight: 750; }
.panel { margin-top: 16px; overflow: hidden; }
.panel-head { display: flex; justify-content: space-between; gap: 16px; align-items: baseline; padding: 20px 22px 13px; }
.panel-head h2 { margin: 0; font-size: 1.15rem; letter-spacing: -0.02em; }
.panel-head p { margin: 0; color: var(--muted); font-size: 0.9rem; }
.source-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 12px; padding: 0 22px 22px; }
.source-card { padding: 14px 16px; background: var(--panel-muted); border: 1px solid var(--line); border-radius: 12px; }
.source-card header { display: flex; justify-content: space-between; gap: 12px; align-items: center; }
.source-name { font-weight: 750; }
.source-meta { margin-top: 6px; color: var(--muted); font-size: 0.83rem; }
.badge { display: inline-flex; align-items: center; gap: 5px; padding: 3px 8px; border-radius: 999px; font-size: 0.72rem; font-weight: 750; letter-spacing: 0.04em; text-transform: uppercase; }
.badge-fetched, .badge-fresh { color: var(--positive); background: color-mix(in srgb, var(--positive) 14%, transparent); }
.badge-stale, .badge-unavailable { color: var(--warning); background: color-mix(in srgb, var(--warning) 16%, transparent); }
.badge-cached { color: var(--accent-strong); background: var(--accent-soft); }
.table-wrap { overflow-x: auto; border-top: 1px solid var(--line); }
table { width: 100%; min-width: 1240px; border-collapse: separate; border-spacing: 0; }
caption { padding: 0; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); clip-path: inset(50%); white-space: nowrap; position: absolute; }
th, td { padding: 11px 12px; border-bottom: 1px solid var(--line); vertical-align: middle; text-align: left; }
th { position: sticky; top: 0; z-index: 1; color: var(--muted); background: var(--panel-muted); font-size: 0.72rem; letter-spacing: 0.06em; text-transform: uppercase; white-space: nowrap; }
th:first-child, td:first-child { padding-left: 22px; }
th:last-child, td:last-child { padding-right: 22px; }
th button { display: inline-flex; align-items: center; gap: 5px; padding: 0; border: 0; color: inherit; background: transparent; font: inherit; letter-spacing: inherit; text-transform: inherit; cursor: pointer; }
th button:hover, th button:focus-visible { color: var(--accent-strong); }
th button:focus-visible { outline: 3px solid color-mix(in srgb, var(--accent) 45%, transparent); outline-offset: 4px; border-radius: 4px; }
.sort-mark { opacity: 0.35; font-size: 0.9rem; }
th[aria-sort="ascending"] .sort-mark::after { content: "↑"; opacity: 1; }
th[aria-sort="descending"] .sort-mark::after { content: "↓"; opacity: 1; }
th[aria-sort="none"] .sort-mark::after { content: "↕"; }
tbody tr:hover { background: color-mix(in srgb, var(--accent-soft) 38%, transparent); }
tbody tr:last-child td { border-bottom: 0; }
.rank { color: var(--accent-strong); font: 750 1rem/1 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
.model { min-width: 220px; font-weight: 750; }
.org { color: var(--muted); }
.score, .number { font-variant-numeric: tabular-nums; text-align: right; white-space: nowrap; }
th.score, th.number { text-align: right; }
.selected-score { color: var(--accent-strong); font-weight: 800; }
.access-open { color: var(--positive); }
.access-closed { color: var(--muted); }
.access-unknown { color: var(--warning); }
.evidence { max-width: 260px; color: var(--muted); font-size: 0.78rem; }
.muted { color: var(--muted); }
.empty { padding: 28px 22px; color: var(--muted); text-align: center; }
.detail-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 12px 20px; padding: 0 22px 22px; }
.detail-grid dl { margin: 0; }
.detail-grid dt { color: var(--muted); font-size: 0.75rem; font-weight: 750; letter-spacing: 0.07em; text-transform: uppercase; }
.detail-grid dd { margin: 3px 0 0; }
details summary { padding: 16px 22px; color: var(--accent-strong); font-weight: 750; cursor: pointer; }
details summary:focus-visible { outline: 3px solid color-mix(in srgb, var(--accent) 45%, transparent); outline-offset: -3px; }
.footnote { margin: 18px 4px 0; color: var(--muted); font-size: 0.83rem; }
.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
@media (max-width: 760px) {
  .wrap { width: min(100% - 20px, 1500px); padding-top: 10px; }
  .hero { grid-template-columns: 1fr; padding: 22px; border-radius: 17px; }
  .hero-meta { text-align: left; }
  .stats { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .panel-head { display: block; padding-inline: 16px; }
  .panel-head p { margin-top: 4px; }
  .source-grid, .detail-grid { padding-inline: 16px; }
  th:first-child, td:first-child { padding-left: 16px; }
  th:last-child, td:last-child { padding-right: 16px; }
}
</style>
</head>
<body>
<main>
<div class="wrap">
<header class="hero">
  <div>
    <div class="eyebrow">Llambo · external evaluations</div>
    <h1>Model ranking, at a glance.</h1>
    <p>Deterministic LES-1 percentiles, ranked by <strong>`)
	writeEscaped(&b, report.RankingProfile)
	b.WriteString(`</strong>. Click any column heading to sort. The report is self-contained and works offline once saved.</p>
  </div>
  <div class="hero-meta"><strong>`)
	writeEscaped(&b, fmt.Sprintf("%d models", limit+len(projectedRows)))
	b.WriteString(`</strong>eligible rows shown<br>Generated `)
	writeEscaped(&b, formatHTMLTime(report.GeneratedAt))
	b.WriteString(`</div>
</header>

<section class="stats" aria-label="Report summary">
`)
	writeHTMLStat(&b, "Ranked by", report.RankingProfile)
	writeHTMLStat(&b, "Formula", report.FormulaVersion)
	writeHTMLStat(&b, "Canonical population", strconv.Itoa(report.Formula.Population))
	writeHTMLStat(&b, "Dual-source identities", strconv.Itoa(report.Formula.DualSourceModels))
	b.WriteString(`</section>

<section class="panel" aria-labelledby="sources-heading">
  <div class="panel-head"><h2 id="sources-heading">Source status</h2><p>Freshness and availability used for this report</p></div>
  <div class="source-grid">
`)
	if len(report.Sources) == 0 {
		b.WriteString(`<div class="empty">No source status was recorded.</div>`)
	} else {
		for _, source := range report.Sources {
			writeHTMLSourceCard(&b, source)
		}
	}
	b.WriteString(`</div>
</section>

<section class="panel" aria-labelledby="rankings-heading">
  <div class="panel-head"><h2 id="rankings-heading">Rankings</h2><p>Click a column to sort · the initial order follows the selected profile</p></div>
  <div class="table-wrap">
    <table id="rankings" data-sortable="true" data-sort-key="`)
	writeEscaped(&b, report.RankingProfile)
	b.WriteString(`" data-sort-column="0" data-sort-direction="asc">
      <caption>Canonical model rankings</caption>
      <thead><tr>
`)
	for index, column := range htmlRankingColumns() {
		ariaSort := "none"
		if index == 0 {
			ariaSort = "ascending"
		}
		fmt.Fprintf(&b, "        <th scope=\"col\" class=\"%s\" data-type=\"%s\" aria-sort=\"%s\"><button type=\"button\" data-label=\"%s\">%s<span class=\"sort-mark\" aria-hidden=\"true\"></span></button></th>\n", column.className, column.dataType, ariaSort, html.EscapeString(column.label), html.EscapeString(column.label))
	}
	b.WriteString(`      </tr></thead>
      <tbody>
`)
	if len(canonicalRows[:limit]) == 0 {
		b.WriteString(`<tr data-empty="true"><td class="empty" colspan="16">No eligible canonical models matched the current filters.</td></tr>`)
	} else {
		previousScore := 0.0
		previousRank := 0
		for i, row := range canonicalRows[:limit] {
			writeHTMLRankingRow(&b, report, row, i, &previousScore, &previousRank)
		}
	}
	b.WriteString(`      </tbody>
    </table>
  </div>
  <p id="rankings-status" class="sr-only" role="status" aria-live="polite"></p>
</section>
`)

	if len(projectedRows) > 0 {
		b.WriteString(`<section class="panel" aria-labelledby="projections-heading">
  <div class="panel-head"><h2 id="projections-heading">Tracked local/OSS projections</h2><p>Inherited upstream evidence, shown separately from canonical ranks</p></div>
  <div class="table-wrap"><table id="projections">
    <caption>Tracked local and open-source projection rows</caption>
    <thead><tr><th scope="col">Artifact</th><th scope="col">Projected from</th><th scope="col" class="score">Selected</th><th scope="col" class="score">Overall</th><th scope="col" class="score">Coding</th><th scope="col" class="score">Agents</th><th scope="col" class="score">Writing</th><th scope="col">Cost</th><th scope="col">Evidence</th></tr></thead>
    <tbody>
`)
		for _, row := range projectedRows {
			writeHTMLProjectionRow(&b, report, row)
		}
		b.WriteString(`    </tbody>
  </table></div>
</section>
`)
	}

	b.WriteString(`<section class="panel" aria-labelledby="details-heading">
  <details>
    <summary id="details-heading">Formula and diagnostics</summary>
    <div class="detail-grid">
`)
	writeHTMLDetail(&b, "Formula method", report.Formula.Method)
	writeHTMLDetail(&b, "Reference drift", report.Formula.Drift.Status)
	writeHTMLDetail(&b, "Maximum KS", fmt.Sprintf("%.3f", report.Formula.Drift.MaxKS))
	writeHTMLDetail(&b, "Jackknife stability", fmt.Sprintf("%s · Kendall %.3f · top-20 overlap %.1f%%", report.Formula.Stability.Status, report.Formula.Stability.MeanKendall, report.Formula.Stability.MinTop20Overlap*100))
	writeHTMLDetail(&b, "Frozen reference", formatHTMLTime(report.Formula.Reference.CreatedAt))
	writeHTMLDetail(&b, "Projection registry", fmt.Sprintf("%d applied of %d configured · version %d", report.Projections.Applied, report.Projections.Configured, report.Projections.Version))
	if len(report.Formula.Drift.Reasons) > 0 {
		writeHTMLDetail(&b, "Drift reasons", strings.Join(report.Formula.Drift.Reasons, " · "))
	}
	if len(report.Projections.Missing) > 0 {
		missing := make([]string, 0, len(report.Projections.Missing))
		for _, projection := range report.Projections.Missing {
			missing = append(missing, projection.ArtifactKey+" ← "+projection.SourceKey)
		}
		writeHTMLDetail(&b, "Missing projections", strings.Join(missing, " · "))
	}
	if report.Eligibility != nil {
		writeHTMLDetail(&b, "Eligibility", fmt.Sprintf("%d included of %d · %d excluded · %d missing overall · %d below overall · %d over price · %d unknown prices retained", report.Eligibility.IncludedModels, report.Eligibility.InputModels, report.Eligibility.ExcludedModels, report.Eligibility.MissingOverall, report.Eligibility.BelowOverall, report.Eligibility.OverOutputPrice, report.Eligibility.UnknownPriceKept))
	}
	if report.OMLX != nil {
		omlx := report.OMLX.Status
		if report.OMLX.Endpoint != "" {
			omlx += " at " + report.OMLX.Endpoint
		}
		if report.OMLX.Error != "" {
			omlx += ": " + report.OMLX.Error
		}
		writeHTMLDetail(&b, "OMLX live selection", omlx)
	}
	if report.AAVersion > 0 {
		writeHTMLDetail(&b, "Artificial Analysis index", fmt.Sprintf("%.1f", report.AAVersion))
	}
	b.WriteString(`    </div>
  </details>
</section>

<p class="footnote">Scores are relative external-evidence percentiles, not probabilities of task success. Missing evidence remains visible through coverage and evidence bounds.</p>
</div>
</main>
<script>
function sortTable(tableID, header) {
  const table = document.getElementById(tableID);
  if (!table || !header || !table.tBodies.length) return;
  const index = header.cellIndex;
  const type = header.dataset.type || "string";
  const currentIndex = table.dataset.sortColumn;
  const currentDirection = table.dataset.sortDirection;
	const direction = currentIndex === String(index)
	  ? (currentDirection === "asc" ? "desc" : "asc")
	  : (type === "number" ? "desc" : "asc");
	const rows = Array.prototype.slice.call(table.tBodies[0].rows)
	  .filter(row => row.dataset.empty !== "true" && row.cells.length > index);
	if (!rows.length) return;
	rows.forEach((row, rowIndex) => { row.dataset.originalIndex = String(rowIndex); });
	rows.sort((left, right) => {
    const a = left.cells[index];
    const b = right.cells[index];
    const aMissing = a.dataset.missing === "true";
    const bMissing = b.dataset.missing === "true";
    if (aMissing !== bMissing) return aMissing ? 1 : -1;
    const aValue = a.dataset.value ?? a.textContent.trim();
    const bValue = b.dataset.value ?? b.textContent.trim();
    let result;
    if (type === "number") {
      result = Number(aValue) - Number(bValue);
	  } else {
	    result = aValue.localeCompare(bValue, undefined, { numeric: true, sensitivity: "base" });
	  }
	  if (Number.isNaN(result)) result = 0;
	  if (result === 0) {
	    return Number(left.dataset.originalIndex) - Number(right.dataset.originalIndex);
	  }
	  return direction === "asc" ? result : -result;
  });
  rows.forEach(row => table.tBodies[0].appendChild(row));
  table.dataset.sortColumn = String(index);
  table.dataset.sortDirection = direction;
	table.querySelectorAll("thead th").forEach(th => {
	  th.setAttribute("aria-sort", th === header ? (direction === "asc" ? "ascending" : "descending") : "none");
	});
	const status = document.getElementById(tableID + "-status");
	if (status) status.textContent = "Sorted by " + (header.querySelector("[data-label]")?.dataset.label || "column") + " " + (direction === "asc" ? "ascending" : "descending") + ".";
}
document.querySelectorAll("#rankings th button").forEach(button => {
  button.addEventListener("click", () => sortTable("rankings", button.closest("th")));
});
</script>
</body>
</html>
`)
	return b.String()
}

type htmlRankingColumn struct {
	label     string
	className string
	dataType  string
}

func htmlRankingColumns() []htmlRankingColumn {
	return []htmlRankingColumn{
		{label: "Rank", className: "number", dataType: "number"},
		{label: "Model", className: "", dataType: "string"},
		{label: "Organization", className: "", dataType: "string"},
		{label: "Access", className: "", dataType: "string"},
		{label: "Selected", className: "score", dataType: "number"},
		{label: "Overall", className: "score", dataType: "number"},
		{label: "General", className: "score", dataType: "number"},
		{label: "Coding", className: "score", dataType: "number"},
		{label: "Reasoning", className: "score", dataType: "number"},
		{label: "Agents", className: "score", dataType: "number"},
		{label: "Writing", className: "score", dataType: "number"},
		{label: "Long context", className: "score", dataType: "number"},
		{label: "Speed", className: "score", dataType: "number"},
		{label: "Value", className: "score", dataType: "number"},
		{label: "Output $/1M", className: "score", dataType: "number"},
		{label: "Evidence", className: "evidence", dataType: "string"},
	}
}

func writeHTMLRankingRow(b *strings.Builder, report Report, row ReportModel, index int, previousScore *float64, previousRank *int) {
	selected := row.Scores[report.RankingProfile]
	rankText, rankValue := "—", ""
	if selected != nil && finiteHTMLNumber(selected.Score) {
		if *previousRank == 0 || selected.Score != *previousScore {
			*previousRank = index + 1
			*previousScore = selected.Score
		}
		rankText, rankValue = strconv.Itoa(*previousRank), strconv.Itoa(*previousRank)
	}
	modelName := row.Name
	if modelName == "" {
		modelName = row.Key
	}
	access := fmtAccess(row.Open)
	accessClass := "access-" + access
	if access == "unknown" {
		accessClass = "access-unknown"
	}
	b.WriteString("        <tr>\n")
	writeHTMLCell(b, "rank", rankText, rankValue, "")
	writeHTMLCell(b, "model", modelName, strings.ToLower(modelName), row.Key)
	writeHTMLCell(b, "org", row.Organization, strings.ToLower(row.Organization), "")
	writeHTMLCell(b, accessClass, access, strings.ToLower(access), "")
	writeHTMLScoreCell(b, "selected-score", selected)
	for _, profile := range []string{"overall", "general", "coding", "reasoning", "agents", "writing", "long-context", "speed", "value"} {
		writeHTMLScoreCell(b, "score", row.Scores[profile])
	}
	writeHTMLPriceCell(b, row)
	writeHTMLCell(b, "evidence", fmtEvidence(selected), strings.ToLower(fmtEvidence(selected)), "")
	b.WriteString("        </tr>\n")
}

func writeHTMLProjectionRow(b *strings.Builder, report Report, row ReportModel) {
	selected := row.Scores[report.RankingProfile]
	modelName := row.Name
	if modelName == "" {
		modelName = row.Key
	}
	b.WriteString("      <tr>\n")
	writeHTMLCell(b, "model", modelName, strings.ToLower(modelName), row.Key)
	writeHTMLCell(b, "org", projectionSourceKey(row), strings.ToLower(projectionSourceKey(row)), "")
	writeHTMLScoreCell(b, "selected-score", selected)
	for _, profile := range []string{"overall", "coding", "agents", "writing"} {
		writeHTMLScoreCell(b, "score", row.Scores[profile])
	}
	writeHTMLCell(b, "muted", fmtOutputPrice(row), "", "")
	writeHTMLCell(b, "evidence", fmtEvidence(selected), strings.ToLower(fmtEvidence(selected)), "")
	b.WriteString("      </tr>\n")
}

func writeHTMLSourceCard(b *strings.Builder, source SourceStatus) {
	status := source.Cache
	if status == "" {
		status = "unknown"
	}
	statusClass := "badge-" + status
	if status != "fetched" && status != "fresh" && status != "stale" && status != "unavailable" && status != "cached" {
		statusClass = "badge-unavailable"
	}
	fetched := "not available"
	if !source.FetchedAt.IsZero() {
		fetched = formatHTMLTime(source.FetchedAt)
	}
	b.WriteString("    <article class=\"source-card\">\n      <header><span class=\"source-name\">")
	writeEscaped(b, source.Name)
	fmt.Fprintf(b, "</span><span class=\"badge %s\" role=\"status\">", statusClass)
	writeEscaped(b, status)
	b.WriteString("</span></header><div class=\"source-meta\">")
	fmt.Fprintf(b, "%d models · fetched %s", source.Models, html.EscapeString(fetched))
	if safeURL := htmlSourceURL(source.URL); safeURL != "" {
		fmt.Fprintf(b, " · <a href=\"%s\" rel=\"noreferrer\">source</a>", html.EscapeString(safeURL))
	}
	if source.Error != "" {
		b.WriteString("<br><span class=\"muted\">")
		writeEscaped(b, source.Error)
		b.WriteString("</span>")
	}
	b.WriteString("</div></article>\n")
}

func writeHTMLStat(b *strings.Builder, label, value string) {
	b.WriteString("  <div class=\"stat\"><div class=\"stat-label\">")
	writeEscaped(b, label)
	b.WriteString("</div><div class=\"stat-value\">")
	writeEscaped(b, value)
	b.WriteString("</div></div>\n")
}

func writeHTMLDetail(b *strings.Builder, label, value string) {
	b.WriteString("      <dl><dt>")
	writeEscaped(b, label)
	b.WriteString("</dt><dd>")
	writeEscaped(b, value)
	b.WriteString("</dd></dl>\n")
}

func writeHTMLScoreCell(b *strings.Builder, class string, score *ExternalScore) {
	if score == nil || !finiteHTMLNumber(score.Score) {
		writeHTMLCellWithMissing(b, class, "—", fmtEvidence(score))
		return
	}
	value := strconv.FormatFloat(score.Score, 'f', 3, 64)
	writeHTMLCell(b, class, fmtExternalScore(score), value, fmtEvidence(score))
}

func writeHTMLPriceCell(b *strings.Builder, row ReportModel) {
	value := fmtOutputPrice(row)
	sortValue := htmlOutputPriceValue(row)
	if sortValue == "" {
		writeHTMLCellWithMissing(b, "score", value, "")
		return
	}
	writeHTMLCell(b, "score", value, sortValue, "highest known output price")
}

func writeHTMLCell(b *strings.Builder, class, text, sortValue, title string) {
	if sortValue == "" {
		writeHTMLCellWithMissing(b, class, text, title)
		return
	}
	fmt.Fprintf(b, "        <td class=\"%s\" data-value=\"%s\"", class, html.EscapeString(sortValue))
	if title != "" {
		fmt.Fprintf(b, " title=\"%s\"", html.EscapeString(title))
	}
	b.WriteString(">")
	writeEscaped(b, text)
	b.WriteString("</td>\n")
}

func writeHTMLCellWithMissing(b *strings.Builder, class, text, title string) {
	fmt.Fprintf(b, "        <td class=\"%s\" data-value=\"\" data-missing=\"true\"", class)
	if title != "" {
		fmt.Fprintf(b, " title=\"%s\"", html.EscapeString(title))
	}
	b.WriteString(">")
	writeEscaped(b, text)
	b.WriteString("</td>\n")
}

func writeEscaped(b *strings.Builder, value string) {
	b.WriteString(html.EscapeString(value))
}

func formatHTMLTime(value time.Time) string {
	if value.IsZero() {
		return "not available"
	}
	return value.Format("2006-01-02 15:04 UTC")
}

func finiteHTMLNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func htmlOutputPriceValue(row ReportModel) string {
	if row.Projection != nil {
		return ""
	}
	prices := make([]float64, 0, 2)
	if row.LLMStats != nil && row.LLMStats.OutputPrice != nil {
		prices = appendFinitePrice(prices, *row.LLMStats.OutputPrice)
	}
	if row.AA != nil && row.AA.OutputPrice != nil {
		prices = appendFinitePrice(prices, *row.AA.OutputPrice)
	}
	if len(prices) == 0 {
		return ""
	}
	sort.Float64s(prices)
	return strconv.FormatFloat(prices[len(prices)-1], 'f', 6, 64)
}

func projectionSourceKey(row ReportModel) string {
	if row.Projection == nil {
		return "—"
	}
	return row.Projection.SourceKey
}

func htmlSourceURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	return raw
}
