package writingcampaign

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"sort"
	"time"
)

type pageRow struct {
	Model        string
	Status       string
	Output       string
	Speed        string
	Cost         string
	Latency      string
	TotalTime    string
	FinishReason string
	Attempts     int
}

type pageData struct {
	Generated string
	Rows      []pageRow
}

var pageTemplate = template.Must(template.New("writing").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Writing model comparison</title>
<style>
:root { color-scheme: light dark; --bg:#f4f1e8; --ink:#171713; --muted:#68665f; --line:#b8b2a2; --panel:#fffdf7; --accent:#9d2f1f; }
@media (prefers-color-scheme: dark) { :root { --bg:#171713; --ink:#f4f1e8; --muted:#aaa69a; --line:#514d43; --panel:#211f1a; --accent:#ff8b72; } }
* { box-sizing:border-box; }
body { margin:0; background:var(--bg); color:var(--ink); font:15px/1.55 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace; }
main { width:min(1800px,100%); margin:0 auto; padding:32px 20px 80px; }
h1 { margin:0; font:700 clamp(28px,4vw,52px)/1.05 Georgia,serif; letter-spacing:-.03em; }
.meta { color:var(--muted); margin:8px 0 24px; }
.table-wrap { overflow-x:auto; border:1px solid var(--line); background:var(--panel); }
table { width:100%; border-collapse:collapse; table-layout:fixed; }
th,td { border-bottom:1px solid var(--line); border-right:1px solid var(--line); padding:12px; vertical-align:top; text-align:left; }
th:last-child,td:last-child { border-right:0; }
th { position:sticky; top:0; z-index:1; background:var(--ink); color:var(--bg); text-transform:uppercase; font-size:12px; letter-spacing:.08em; }
tr:last-child td { border-bottom:0; }
.model { width:300px; overflow-wrap:anywhere; color:var(--accent); font-weight:700; white-space:pre-line; }
.status { width:105px; }
.number { width:105px; font-variant-numeric:tabular-nums; white-space:nowrap; }
.article-row td { border-right:0; padding:0; }
.article-cell { background:var(--panel); }
.output { width:100%; padding:24px clamp(18px,4vw,56px) 32px; white-space:pre-wrap; overflow-wrap:anywhere; font-family:Georgia,serif; font-size:17px; line-height:1.7; }
.model-group:not(:last-child) .article-cell { border-bottom:3px solid var(--ink); }
.empty { color:var(--muted); font-style:italic; }
@media (max-width:800px) { main{padding:20px 10px 50px} table{min-width:1100px} }
</style>
</head>
<body><main>
<h1>Writing model comparison</h1>
<p class="meta">OpenRouter-only, fixed model IDs plus openrouter/auto · generated {{.Generated}}</p>
<div class="table-wrap"><table>
<thead><tr><th class="model">Provider / model</th><th class="status">Status</th><th class="number">Speed</th><th class="number">Cost</th><th class="number">Latency</th><th class="number">Total time</th><th class="number">Finish</th><th class="number">Attempts</th></tr></thead>
{{range .Rows}}<tbody class="model-group"><tr class="metadata-row"><td class="model">{{.Model}}</td><td class="status">{{.Status}}</td><td class="number">{{.Speed}}</td><td class="number">{{.Cost}}</td><td class="number">{{.Latency}}</td><td class="number">{{.TotalTime}}</td><td class="number">{{.FinishReason}}</td><td class="number">{{.Attempts}}</td></tr><tr class="article-row"><td colspan="8" class="article-cell"><article class="output">{{if .Output}}{{.Output}}{{else}}<span class="empty">No output collected</span>{{end}}</article></td></tr></tbody>{{end}}
</table></div>
</main></body></html>
`))

func RenderHTML(w io.Writer, roster Roster, receipts []Receipt, generated time.Time) error {
	byModel := make(map[string]Receipt, len(receipts))
	for _, receipt := range receipts {
		byModel[receipt.ModelID] = receipt
	}
	rows := make([]pageRow, 0, len(roster.Models))
	for _, model := range roster.Models {
		receipt, ok := byModel[model.OpenRouterModelID]
		row := pageRow{Model: "openrouter/" + model.OpenRouterModelID, Status: "pending", Speed: "—", Cost: "—", Latency: "—", TotalTime: "—", FinishReason: "—"}
		if ok {
			if receipt.ServedModel != "" && receipt.ServedModel != model.OpenRouterModelID {
				row.Model += "\n→ " + receipt.ServedModel
				if receipt.ServedProvider != "" {
					row.Model += " (" + receipt.ServedProvider + ")"
				}
			}
			row.Status = receipt.Status
			row.Output = receipt.OutputText
			row.Speed = fmt.Sprintf("%.1f tok/s", receipt.TokensPerSecond)
			row.Cost = fmt.Sprintf("$%.6f", receipt.TotalCostUSD)
			row.Latency = fmt.Sprintf("%.2fs", float64(receipt.TotalLatencyMS)/1000)
			row.TotalTime = fmt.Sprintf("%.2fs", float64(receipt.TotalTimeToFinishMS)/1000)
			row.FinishReason = receipt.FinishReason
			row.Attempts = len(receipt.Attempts)
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Model < rows[j].Model })
	return pageTemplate.Execute(w, pageData{Generated: generated.UTC().Format(time.RFC3339), Rows: rows})
}

func WriteHTML(path string, roster Roster, receipts []Receipt, generated time.Time) error {
	var out bytes.Buffer
	if err := RenderHTML(&out, roster, receipts, generated); err != nil {
		return err
	}
	return writeAtomic(path, out.Bytes())
}
