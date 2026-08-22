package evals

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	lfm25Revision       = "a334ee78cd38458bb71eda24109ac42dcec1309d"
	lfm25ContentSHA     = "321173d06e4d13010fdf8ad9fffb2b4fbb76f39ffcc4c493cdd0895b39d7a37c"
	qwen38Revision      = "1d4bf0f2ff6012fd82039f2fa52739d0dd7c60c0"
	qwen38ContentSHA    = "57e4bdb258ee1a7d2635c5174ebd4e56abe392505cdb5f8bbb356b0dc4293641"
	gptOSSRevision      = "2508.10925v1"
	gptOSSContentSHA    = "689d78673abc5ab9d9033e4ea88d8e4dd4379e811a8f279b45d0a0041b001b91"
	lfm25VLRevision     = "5a414ead75d45db003906d06fb62bd5b6846cec0"
	lfm25VLContentSHA   = "2ad0e4f36a755e3c70b338f0118e6a603a4cb7b65c034263dc10a4d3d9ce7a03"
	gemma4Revision      = "5bbc2fb1c1b2c611d06e3d9f23c170ba21659d89"
	gemma4ContentSHA    = "9871799033826e5aca148dfd2c8167c5386fedb669aa486846dadbd11d42a872"
	modelCardUnit       = "percent"
	modelCardDirection  = "higher"
	modelCardEvidence   = "first_party"
	modelCardSourceType = "published comparison table"
)

// officialCardTargetScores seals the complete target row from each pinned
// card. It is intentionally checked again when loading cache snapshots because
// the raw source bytes live in the sealed cache, not the JSON summary.
var officialCardTargetScores = map[string]map[string]map[string]float64{
	"official-lfm25-2.6b": {"lfm-2.5-2.6b": {
		"aime": 51.87, "livecodebench-v6": 59.41, "ifbench": 59.17, "multi-if": 80.07, "ifstruct": 85.49, "bfcl-v4": 56.88, "toolsandbox": 77.83,
	}},
	"official-qwen3.8-27b": {"qwen3.8-27b": {
		"swe-bench-pro": 61.7, "ifbench": 79.5, "gpqa": 89.2, "livecodebench-v6": 90.3,
	}},
	"official-gpt-oss-20b": {"gpt-oss-20b-high": {
		"aime": 91.7, "gpqa": 71.5, "swe-bench-verified": 60.7, "tau-bench-retail": 54.8, "aider": 34.2,
	}},
	"official-lfm25-vl-3b": {"lfm-2.5-vl-3b": {
		"toolsandbox": 59.5, "bfcl-v4": 32.5,
	}},
	"official-gemma4": {
		"gemma-4-31b-it":     {"mmlu-pro": 85.2, "aime": 89.2, "livecodebench-v6": 80.0, "gpqa": 84.3, "tau2-bench": 76.9},
		"gemma-4-26b-a4b-it": {"mmlu-pro": 82.6, "aime": 88.3, "livecodebench-v6": 77.1, "gpqa": 82.3, "tau2-bench": 68.2},
	},
}

type officialCardTarget struct {
	modelKey     string
	modelName    string
	organization string
	column       int
}

type officialCardSpec struct {
	sourceID     string
	modelKey     string
	modelName    string
	organization string
	revision     string
	contentSHA   string
	url          func(Options) string
	parse        func([]byte) (map[string][]float64, error)
	targets      []officialCardTarget
	columns      int
}

func lfm25CardSpec() officialCardSpec {
	return officialCardSpec{
		sourceID: "liquidai-lfm25-2.6b-card", modelKey: "lfm-2.5-2.6b", modelName: "LFM2.5-2.6B", organization: "Liquid AI", revision: lfm25Revision, contentSHA: lfm25ContentSHA,
		url: func(opts Options) string { return opts.OfficialLFMURL }, parse: parseLFM25Card,
	}
}

func qwen38CardSpec() officialCardSpec {
	return officialCardSpec{
		sourceID: "qwen3.8-27b-card", modelKey: "qwen3.8-27b", modelName: "Qwen3.8-27B", organization: "Qwen", revision: qwen38Revision, contentSHA: qwen38ContentSHA,
		url: func(opts Options) string { return opts.OfficialQwenURL }, parse: parseQwen38Card,
	}
}

func gptOSSCardSpec() officialCardSpec {
	return officialCardSpec{
		sourceID: "openai-gpt-oss-model-card", modelKey: "gpt-oss-20b-high", modelName: "gpt-oss-20b (high)", organization: "OpenAI", revision: gptOSSRevision, contentSHA: gptOSSContentSHA,
		url: func(opts Options) string { return opts.OfficialGPTOSSURL }, parse: parseGPTOSSCard, columns: 6,
	}
}

func lfm25VLCardSpec() officialCardSpec {
	return officialCardSpec{
		sourceID: "liquidai-lfm25-vl-3b-card", modelKey: "lfm-2.5-vl-3b", modelName: "LFM2.5-VL-3B", organization: "Liquid AI", revision: lfm25VLRevision, contentSHA: lfm25VLContentSHA,
		url: func(opts Options) string { return opts.OfficialLFMVLURL }, parse: parseLFM25VLCard, columns: 6,
	}
}

func gemma4CardSpec() officialCardSpec {
	return officialCardSpec{
		sourceID: "google-gemma4-model-card", revision: gemma4Revision, contentSHA: gemma4ContentSHA,
		url: func(opts Options) string { return opts.OfficialGemmaURL }, parse: parseGemma4Card, columns: 6,
		targets: []officialCardTarget{
			{modelKey: "gemma-4-31b-it", modelName: "Gemma 4 31B", organization: "Google", column: 0},
			{modelKey: "gemma-4-26b-a4b-it", modelName: "Gemma 4 26B A4B", organization: "Google", column: 1},
		},
	}
}

func officialCardSpecForCache(cacheName string) (officialCardSpec, bool) {
	switch cacheName {
	case "official-lfm25-2.6b":
		return lfm25CardSpec(), true
	case "official-qwen3.8-27b":
		return qwen38CardSpec(), true
	case "official-gpt-oss-20b":
		return gptOSSCardSpec(), true
	case "official-lfm25-vl-3b":
		return lfm25VLCardSpec(), true
	case "official-gemma4":
		return gemma4CardSpec(), true
	default:
		return officialCardSpec{}, false
	}
}

func fetchOfficialLFM25(ctx context.Context, opts Options) (sourceSnapshot, error) {
	return fetchOfficialCard(ctx, opts, lfm25CardSpec())
}

func fetchOfficialQwen38(ctx context.Context, opts Options) (sourceSnapshot, error) {
	return fetchOfficialCard(ctx, opts, qwen38CardSpec())
}

func fetchOfficialGPTOSS(ctx context.Context, opts Options) (sourceSnapshot, error) {
	return fetchOfficialCard(ctx, opts, gptOSSCardSpec())
}

func fetchOfficialLFMVL(ctx context.Context, opts Options) (sourceSnapshot, error) {
	return fetchOfficialCard(ctx, opts, lfm25VLCardSpec())
}

func fetchOfficialGemma4(ctx context.Context, opts Options) (sourceSnapshot, error) {
	return fetchOfficialCard(ctx, opts, gemma4CardSpec())
}

func fetchOfficialCard(ctx context.Context, opts Options, spec officialCardSpec) (sourceSnapshot, error) {
	data, err := getBytes(ctx, opts.Client, spec.url(opts))
	if err != nil {
		return sourceSnapshot{}, err
	}
	if got := SealBytes(data); got != spec.contentSHA {
		return sourceSnapshot{}, fmt.Errorf("%s content SHA-256 %s does not match pinned %s", spec.sourceID, got, spec.contentSHA)
	}
	rows, err := spec.parse(data)
	if err != nil {
		return sourceSnapshot{}, fmt.Errorf("parse %s: %w", spec.sourceID, err)
	}
	targets := spec.cardTargets()
	models := make([]Model, 0, len(targets))
	for _, target := range targets {
		benchmarks := make(map[string]BenchmarkResult, len(rows))
		for benchmark, values := range rows {
			if spec.columns != 0 && len(values) != spec.columns {
				return sourceSnapshot{}, fmt.Errorf("%s %s has %d comparison values, want %d", spec.sourceID, benchmark, len(values), spec.columns)
			}
			if len(values) <= target.column {
				return sourceSnapshot{}, fmt.Errorf("%s %s has %d comparison values, need column %d", spec.sourceID, benchmark, len(values), target.column+1)
			}
			value := values[target.column]
			benchmarks[benchmark] = BenchmarkResult{
				Score: &value, Identity: IdentityMatchExact, Version: spec.revision, URL: spec.url(opts), ContentSHA: spec.contentSHA,
				Method: modelCardSourceType, EvidenceGrade: modelCardEvidence, Unit: modelCardUnit, Direction: modelCardDirection,
				Cohort: "official-comparison-table", SourceID: spec.sourceID, SourceRevision: spec.revision, SourceClass: string(SourceFirstPartyResult),
				Locator: "published benchmark table: " + benchmark,
			}
		}
		models = append(models, Model{Key: target.modelKey, Name: target.modelName, Organization: target.organization, IdentityMatch: IdentityMatchExact, Benchmarks: benchmarks})
	}
	if len(models) == 0 || len(rows) == 0 {
		return sourceSnapshot{}, fmt.Errorf("%s supplied no reviewed benchmark rows", spec.sourceID)
	}
	snapshot := sourceSnapshot{Models: models, URL: spec.url(opts), Version: spec.revision, ContentSHA: spec.contentSHA, Method: modelCardSourceType, Observations: len(rows) * len(models), RegistryVersion: SourceRegistryVersion, EvidenceGrade: modelCardEvidence}
	// Production adapters are all registry-known and therefore must match their
	// complete pinned target row before loadSource writes the snapshot. The
	// generic helper remains usable by narrow transport tests with a synthetic
	// source spec.
	if expected := expectedScoresForSpec(spec); expected != nil {
		if err := validateOfficialCardSnapshotForSpec(spec, expected, snapshot); err != nil {
			return sourceSnapshot{}, err
		}
	}
	return snapshot, nil
}

func (spec officialCardSpec) cardTargets() []officialCardTarget {
	if len(spec.targets) != 0 {
		return append([]officialCardTarget(nil), spec.targets...)
	}
	column := 0
	if spec.modelKey == "gpt-oss-20b-high" {
		// Table 3 orders six model+reasoning-effort configurations as 120B
		// low/medium/high then 20B low/medium/high.
		column = 5
	}
	return []officialCardTarget{{modelKey: spec.modelKey, modelName: spec.modelName, organization: spec.organization, column: column}}
}

func validateOfficialCardSnapshot(cacheName string, snapshot sourceSnapshot) error {
	spec, ok := officialCardSpecForCache(cacheName)
	if !ok {
		return fmt.Errorf("unknown official card cache %q", cacheName)
	}
	if snapshot.Version != spec.revision {
		return fmt.Errorf("revision %q does not match pinned %q", snapshot.Version, spec.revision)
	}
	if snapshot.ContentSHA != spec.contentSHA {
		return fmt.Errorf("content SHA-256 %q does not match pinned %q", snapshot.ContentSHA, spec.contentSHA)
	}
	if snapshot.URL != pinnedOfficialCardURL(spec) {
		return fmt.Errorf("source URL %q does not match pinned %q", snapshot.URL, pinnedOfficialCardURL(spec))
	}
	if !officialCardCompatibleRegistryVersion(snapshot.RegistryVersion) {
		return fmt.Errorf("registry version %q does not match %q", snapshot.RegistryVersion, SourceRegistryVersion)
	}
	return validateOfficialCardSnapshotForSpec(spec, officialCardTargetScores[cacheName], snapshot)
}

func officialCardCompatibleRegistryVersion(version string) bool {
	// Source registry v4/v5 added Stats v1 collection/scoring without changing
	// official-card admission. Accept pinned predecessor caches so this source
	// transition does not force unrelated card refreshes.
	return version == SourceRegistryVersion || version == "LLAMBO-6-sources-v4" || version == "LLAMBO-6-sources-v3"
}

func expectedScoresForSpec(spec officialCardSpec) map[string]map[string]float64 {
	for cacheName, scores := range officialCardTargetScores {
		candidate, ok := officialCardSpecForCache(cacheName)
		if ok && candidate.sourceID == spec.sourceID {
			return scores
		}
	}
	return nil
}

func validateOfficialCardSnapshotForSpec(spec officialCardSpec, want map[string]map[string]float64, snapshot sourceSnapshot) error {
	if len(want) == 0 {
		return fmt.Errorf("no pinned target row for source %q", spec.sourceID)
	}
	if len(snapshot.Models) != len(want) {
		return fmt.Errorf("model count %d does not match pinned target count %d", len(snapshot.Models), len(want))
	}
	for _, model := range snapshot.Models {
		expectedScores, ok := want[model.Key]
		if !ok {
			return fmt.Errorf("canonical card model %q is not pinned", model.Key)
		}
		if model.IdentityMatch != IdentityMatchExact {
			return fmt.Errorf("canonical card model %q identity is %q, want exact", model.Key, model.IdentityMatch)
		}
		if len(model.Benchmarks) != len(expectedScores) {
			return fmt.Errorf("model %q benchmark count %d does not match pinned row count %d", model.Key, len(model.Benchmarks), len(expectedScores))
		}
		for benchmark, expected := range expectedScores {
			result, present := model.Benchmarks[benchmark]
			if !present {
				return fmt.Errorf("model %q missing pinned benchmark %q", model.Key, benchmark)
			}
			if result.Score == nil || *result.Score != expected {
				return fmt.Errorf("model %q benchmark %q score does not match pinned source row", model.Key, benchmark)
			}
			if result.Identity != IdentityMatchExact || result.Version != spec.revision || result.URL != pinnedOfficialCardURL(spec) || result.ContentSHA != spec.contentSHA || result.Method != modelCardSourceType || result.EvidenceGrade != modelCardEvidence || result.Unit != modelCardUnit || result.Direction != modelCardDirection || result.Cohort != "official-comparison-table" || result.Locator != "published benchmark table: "+benchmark || result.SourceID != spec.sourceID || result.SourceClass != string(SourceFirstPartyResult) || result.SourceRevision != spec.revision {
				return fmt.Errorf("model %q benchmark %q provenance does not match pinned source", model.Key, benchmark)
			}
		}
	}
	return nil
}

func pinnedOfficialCardURL(spec officialCardSpec) string {
	options := Options{}
	options.applyDefaults()
	return spec.url(options)
}

func parseLFM25Card(data []byte) (map[string][]float64, error) {
	return parseMarkdownTable(data, map[string]string{
		"AIME25": "aime", "LiveCodeBenchv6": "livecodebench-v6", "IFBench": "ifbench", "Multi-IF": "multi-if", "IFStruct": "ifstruct", "BFCLv4": "bfcl-v4", "ToolSandbox": "toolsandbox",
	})
}

func parseQwen38Card(data []byte) (map[string][]float64, error) {
	return parseHTMLTable(data, map[string]string{
		"SWE-bench Pro": "swe-bench-pro", "IFBench": "ifbench", "GPQA Diamond": "gpqa", "LiveCodeBench v6": "livecodebench-v6",
	})
}

func parseGPTOSSCard(data []byte) (map[string][]float64, error) {
	return parseHTMLTable(data, map[string]string{
		"AIME 2025 (no tools)": "aime", "GPQA Diamond (no tools)": "gpqa", "SWE-Bench Verified": "swe-bench-verified", "Tau-Bench Retail": "tau-bench-retail", "Aider Polyglot": "aider",
	})
}

func parseLFM25VLCard(data []byte) (map[string][]float64, error) {
	return parseMarkdownTable(data, map[string]string{
		"ToolSandBox": "toolsandbox", "BFCLv4": "bfcl-v4",
	})
}

func parseGemma4Card(data []byte) (map[string][]float64, error) {
	return parseMarkdownTable(data, map[string]string{
		"MMLU Pro": "mmlu-pro", "AIME 2026 no tools": "aime", "LiveCodeBench v6": "livecodebench-v6", "GPQA Diamond": "gpqa", "Tau2 (average over 3)": "tau2-bench",
	})
}

func parseMarkdownTable(data []byte, labels map[string]string) (map[string][]float64, error) {
	rows := make(map[string][]float64, len(labels))
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 3 {
			continue
		}
		benchmark, ok := labels[strings.TrimSpace(cells[1])]
		if !ok {
			continue
		}
		values := parseScoreCells(cells[2:])
		if len(values) == 0 {
			return nil, fmt.Errorf("%s has no numeric values", cells[1])
		}
		rows[benchmark] = values
	}
	return checkedCardRows(rows, labels)
}

var (
	htmlRowRE  = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	htmlCellRE = regexp.MustCompile(`(?is)<t[dh][^>]*>(.*?)</t[dh]>`)
	htmlTagRE  = regexp.MustCompile(`(?is)<[^>]+>`)
	spaceRE    = regexp.MustCompile(`\s+`)
	scoreRE    = regexp.MustCompile(`[-+]?(?:\d+(?:\.\d*)?|\.\d+)`)
)

func parseHTMLTable(data []byte, labels map[string]string) (map[string][]float64, error) {
	rows := make(map[string][]float64, len(labels))
	for _, row := range htmlRowRE.FindAllSubmatch(data, -1) {
		cells := htmlCellRE.FindAllSubmatch(row[1], -1)
		if len(cells) < 2 {
			continue
		}
		first := cleanHTMLCell(string(cells[0][1]))
		matched := ""
		for label, benchmark := range labels {
			if strings.Contains(first, label) {
				matched = benchmark
				break
			}
		}
		if matched == "" {
			continue
		}
		values := make([]float64, 0, len(cells)-1)
		for _, cell := range cells[1:] {
			values = append(values, parseScoreCells([]string{cleanHTMLCell(string(cell[1]))})...)
		}
		if len(values) == 0 {
			return nil, fmt.Errorf("%s has no numeric values", first)
		}
		rows[matched] = values
	}
	return checkedCardRows(rows, labels)
}

func cleanHTMLCell(value string) string {
	value = htmlTagRE.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	return strings.TrimSpace(spaceRE.ReplaceAllString(value, " "))
}

func parseScoreCells(cells []string) []float64 {
	values := make([]float64, 0, len(cells))
	for _, cell := range cells {
		cell = strings.TrimSpace(strings.Trim(cell, "|"))
		if cell == "" || cell == "--" || strings.Contains(cell, "---") || strings.Contains(strings.ToLower(cell), "n/a") {
			continue
		}
		cell = cleanHTMLCell(cell)
		cell = strings.Trim(cell, "*_` ")
		match := scoreRE.FindString(cell)
		if match == "" {
			continue
		}
		value, err := strconv.ParseFloat(match, 64)
		if err == nil {
			values = append(values, value)
		}
	}
	return values
}

func checkedCardRows(rows map[string][]float64, labels map[string]string) (map[string][]float64, error) {
	missing := make([]string, 0)
	for _, benchmark := range labels {
		if _, ok := rows[benchmark]; !ok {
			missing = append(missing, benchmark)
		}
	}
	if len(missing) != 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("missing reviewed rows: %s", strings.Join(missing, ", "))
	}
	return rows, nil
}
