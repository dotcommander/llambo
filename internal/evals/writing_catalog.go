package evals

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	WritingCatalogVersion = 1
	WritingPrimaryID      = "lechmazur-writing"
	WritingPrimaryURL     = "https://raw.githubusercontent.com/lechmazur/writing/main/README.md"
	maxWritingSourceBody  = 2 << 20
)

var markdownLinkRE = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)

// WritingCatalog describes writing-focused evaluation sources without mixing
// their source-native scores into the LES-1 report.
type WritingCatalog struct {
	GeneratedAt     time.Time            `json:"generated_at"`
	RegistryVersion int                  `json:"registry_version"`
	Benchmarks      []WritingBenchmark   `json:"benchmarks"`
	OpenModels      []WritingOpenModel   `json:"open_models"`
	Leaderboards    []WritingLeaderboard `json:"leaderboards,omitempty"`
}

type WritingBenchmark struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	DataURL       string `json:"data_url,omitempty"`
	Focus         string `json:"focus"`
	Scoring       string `json:"scoring"`
	ScrapeMethod  string `json:"scrape_method"`
	UpdateCadence string `json:"update_cadence"`
	Notes         string `json:"notes,omitempty"`
}

// WritingOpenModel is a reviewed open-weight candidate or a model already
// represented by one of the public writing leaderboards. Coverage is kept
// explicit because public weights and leaderboard coverage are independent.
type WritingOpenModel struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	HuggingFaceURL string `json:"huggingface_url"`
	License        string `json:"license"`
	Coverage       string `json:"coverage"`
	Priority       string `json:"priority"`
	Notes          string `json:"notes,omitempty"`
}

type WritingLeaderboard struct {
	BenchmarkID string                  `json:"benchmark_id"`
	SourceURL   string                  `json:"source_url"`
	FetchedAt   time.Time               `json:"fetched_at"`
	Rows        []WritingLeaderboardRow `json:"rows"`
	Error       string                  `json:"error,omitempty"`
}

type WritingLeaderboardRow struct {
	Rank      int     `json:"rank"`
	Model     string  `json:"model"`
	Score     float64 `json:"comparison_score"`
	WinChance float64 `json:"win_chance_percent"`
	Lower     float64 `json:"uncertainty_lower"`
	Upper     float64 `json:"uncertainty_upper"`
}

type WritingCatalogOptions struct {
	Client *http.Client
	URL    string
	Now    func() time.Time
}

// DefaultWritingCatalog returns the offline registry. It intentionally carries
// source links and model coverage, not made-up cross-benchmark scores.
func DefaultWritingCatalog(now time.Time) WritingCatalog {
	if now.IsZero() {
		now = time.Now()
	}
	return WritingCatalog{
		GeneratedAt:     now.UTC(),
		RegistryVersion: WritingCatalogVersion,
		Benchmarks: []WritingBenchmark{
			{
				ID:            WritingPrimaryID,
				Name:          "Lech Mazur Creative Story-Writing",
				URL:           "https://github.com/lechmazur/writing",
				DataURL:       WritingPrimaryURL,
				Focus:         "Creative short stories under matched constrained briefs",
				Scoring:       "Pairwise Thurstone comparison with estimated win chance and uncertainty range",
				ScrapeMethod:  "Markdown leaderboard and public prompt/story artifacts",
				UpdateCadence: "Upstream releases",
				Notes:         "Primary live source. Each brief requires ten elements including character, setting, motivation, and tone.",
			},
			{
				ID:            "writingbench",
				Name:          "WritingBench",
				URL:           "https://github.com/X-PLUG/WritingBench",
				DataURL:       "https://raw.githubusercontent.com/X-PLUG/WritingBench/main/benchmark_query/benchmark_all.jsonl",
				Focus:         "Real-world professional writing across six domains and 100 subdomains",
				Scoring:       "Instance-specific criteria judged by an LLM evaluator or critic model",
				ScrapeMethod:  "JSONL query corpus plus evaluator artifacts",
				UpdateCadence: "Dataset releases",
				Notes:         "Broadest practical writing coverage in this registry. The public query set is better suited to reproducible reruns than a stale leaderboard.",
			},
			{
				ID:            "eqbench-creative-v3",
				Name:          "EQ-Bench Creative Writing v3",
				URL:           "https://github.com/EQ-bench/creative-writing-bench",
				DataURL:       "https://raw.githubusercontent.com/EQ-bench/creative-writing-bench/main/data/creative_writing_prompts_v3.json",
				Focus:         "Creative writing quality across 32 prompts and three iterations",
				Scoring:       "Rubric judging combined with pairwise Glicko/Elo ranking",
				ScrapeMethod:  "Prompt JSON and published run/result archives",
				UpdateCadence: "Benchmark releases",
				Notes:         "Useful creative-writing cross-check. Reproduction must pin the judge model and generation settings.",
			},
			{
				ID:            "eqbench-longform",
				Name:          "EQ-Bench Longform Writing",
				URL:           "https://github.com/EQ-bench/longform-writing-bench",
				DataURL:       "https://eqbench.com/creative_writing_longform.html",
				Focus:         "Planning, revision, character profiles, and eight-chapter narrative consistency",
				Scoring:       "Rubric-based longform quality and degradation analysis",
				ScrapeMethod:  "Public prompts, rubrics, per-model reports, and leaderboard HTML",
				UpdateCadence: "Benchmark releases",
				Notes:         "Higher-cost source for long-context writing and consistency rather than short-form style alone.",
			},
			{
				ID:            "arena-creative-writing",
				Name:          "Arena Creative Writing",
				URL:           "https://arena.ai/leaderboard/text/creative-writing",
				Focus:         "Human preference for creative-writing conversations",
				Scoring:       "Live pairwise preference ranking",
				ScrapeMethod:  "Rendered leaderboard page",
				UpdateCadence: "Live",
				Notes:         "Good external cross-check, but the prompt corpus and sampling are less reproducible than a fixed benchmark release.",
			},
			{
				ID:            "ifeval",
				Name:          "IFEval",
				URL:           "https://github.com/google-research/google-research/tree/master/instruction_following_eval",
				DataURL:       "https://github.com/google-research/google-research/tree/master/instruction_following_eval",
				Focus:         "Deterministic instruction and format compliance in generated text",
				Scoring:       "Programmatic constraint satisfaction",
				ScrapeMethod:  "Public JSONL prompts and reference checker",
				UpdateCadence: "Research release",
				Notes:         "Auxiliary writing signal. It measures brief adherence, not prose quality.",
			},
			{
				ID:            "ifbench",
				Name:          "IFBench",
				URL:           "https://github.com/allenai/IFBench",
				DataURL:       "https://huggingface.co/datasets/allenai/IFBench_test",
				Focus:         "Out-of-distribution instruction following with 58 constraints",
				Scoring:       "Programmatic constraint satisfaction, with optional multiturn cases",
				ScrapeMethod:  "Public repository and Hugging Face dataset",
				UpdateCadence: "Dataset releases",
				Notes:         "Auxiliary writing signal for strict briefs, formatting, and keyword constraints.",
			},
			{
				ID:            "lechmazur-writing-styles",
				Name:          "Lech Mazur Writing Styles",
				URL:           "https://github.com/lechmazur/writing_styles",
				DataURL:       "https://github.com/lechmazur/writing_styles",
				Focus:         "Style fingerprints and within-model writing diversity",
				Scoring:       "Style-feature and diversity analysis, not a quality leaderboard",
				ScrapeMethod:  "CSV artifacts and generated story corpus",
				UpdateCadence: "Research releases",
				Notes:         "Use to separate quality from stylistic variety. Do not treat its diversity metrics as quality scores.",
			},
		},
		OpenModels: []WritingOpenModel{
			{
				ID:             "deepseek-ai/DeepSeek-V4-Flash-0731",
				Name:           "DeepSeek V4 Flash-0731",
				Provider:       "DeepSeek",
				HuggingFaceURL: "https://huggingface.co/deepseek-ai/DeepSeek-V4-Flash-0731",
				License:        "MIT",
				Coverage:       "new candidate",
				Priority:       "high",
				Notes:          "Newest verified public-weight candidate in this registry; not present in the primary story leaderboard snapshot.",
			},
			{
				ID:             "moonshotai/Kimi-K3",
				Name:           "Kimi K3",
				Provider:       "Moonshot AI",
				HuggingFaceURL: "https://huggingface.co/moonshotai/Kimi-K3",
				License:        "kimi-k3",
				Coverage:       "primary leaderboard",
				Priority:       "high",
				Notes:          "Public weights with a nonstandard model license. Review terms before redistribution.",
			},
			{
				ID:             "zai-org/GLM-5.2",
				Name:           "GLM-5.2",
				Provider:       "Z.ai",
				HuggingFaceURL: "https://huggingface.co/zai-org/GLM-5.2",
				License:        "MIT",
				Coverage:       "primary leaderboard",
				Priority:       "high",
			},
			{
				ID:             "MiniMaxAI/MiniMax-M3",
				Name:           "MiniMax-M3",
				Provider:       "MiniMax",
				HuggingFaceURL: "https://huggingface.co/MiniMaxAI/MiniMax-M3",
				License:        "minimax-m3",
				Coverage:       "primary leaderboard",
				Priority:       "high",
				Notes:          "Public weights with a nonstandard model license. Review terms before redistribution.",
			},
			{
				ID:             "Qwen/Qwen3.6-35B-A3B",
				Name:           "Qwen3.6 35B A3B",
				Provider:       "Qwen",
				HuggingFaceURL: "https://huggingface.co/Qwen/Qwen3.6-35B-A3B",
				License:        "Apache-2.0",
				Coverage:       "candidate variant",
				Priority:       "high",
				Notes:          "The primary leaderboard has hosted Qwen 3.6 variants but not this exact public-weight ID.",
			},
			{
				ID:             "Qwen/Qwen3.6-27B",
				Name:           "Qwen3.6 27B",
				Provider:       "Qwen",
				HuggingFaceURL: "https://huggingface.co/Qwen/Qwen3.6-27B",
				License:        "Apache-2.0",
				Coverage:       "candidate variant",
				Priority:       "high",
				Notes:          "Exact public-weight variant to run alongside the larger Qwen3.6 release.",
			},
			{
				ID:             "google/gemma-4-31B-it",
				Name:           "Gemma 4 31B it",
				Provider:       "Google",
				HuggingFaceURL: "https://huggingface.co/google/gemma-4-31B-it",
				License:        "Apache-2.0",
				Coverage:       "primary variant",
				Priority:       "high",
				Notes:          "The primary leaderboard includes a Gemma 4 31B reasoning setting; verify the serving mode when comparing runs.",
			},
			{
				ID:             "mistralai/Mistral-Large-3-675B-Instruct-2512",
				Name:           "Mistral Large 3 675B Instruct",
				Provider:       "Mistral AI",
				HuggingFaceURL: "https://huggingface.co/mistralai/Mistral-Large-3-675B-Instruct-2512",
				License:        "Apache-2.0",
				Coverage:       "primary leaderboard",
				Priority:       "medium",
			},
			{
				ID:             "XiaomiMiMo/MiMo-V2.5-Pro",
				Name:           "Xiaomi MiMo V2.5 Pro",
				Provider:       "Xiaomi",
				HuggingFaceURL: "https://huggingface.co/XiaomiMiMo/MiMo-V2.5-Pro",
				License:        "MIT",
				Coverage:       "primary leaderboard",
				Priority:       "medium",
			},
			{
				ID:             "openai/gpt-oss-120b",
				Name:           "GPT-OSS-120B",
				Provider:       "OpenAI",
				HuggingFaceURL: "https://huggingface.co/openai/gpt-oss-120b",
				License:        "Apache-2.0",
				Coverage:       "primary leaderboard",
				Priority:       "medium",
			},
			{
				ID:             "openai/gpt-oss-20b",
				Name:           "GPT-OSS-20B",
				Provider:       "OpenAI",
				HuggingFaceURL: "https://huggingface.co/openai/gpt-oss-20b",
				License:        "Apache-2.0",
				Coverage:       "new candidate",
				Priority:       "high",
				Notes:          "The primary leaderboard covers the 120B sibling; add the 20B model as a size-efficiency comparison.",
			},
			{
				ID:             "Qwen/Qwen3-235B-A22B-Instruct-2507",
				Name:           "Qwen3 235B A22B Instruct 2507",
				Provider:       "Qwen",
				HuggingFaceURL: "https://huggingface.co/Qwen/Qwen3-235B-A22B-Instruct-2507",
				License:        "Apache-2.0",
				Coverage:       "candidate variant",
				Priority:       "medium",
				Notes:          "Large open-weight baseline for broad writing and instruction-following reruns.",
			},
		},
	}
}

func FetchWritingCatalog(ctx context.Context, opts WritingCatalogOptions) (WritingCatalog, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	url := opts.URL
	if url == "" {
		url = WritingPrimaryURL
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	fetchedAt := now().UTC()
	catalog := DefaultWritingCatalog(fetchedAt)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return WritingCatalog{}, fmt.Errorf("create writing leaderboard request: %w", err)
	}
	request.Header.Set("Accept", "text/markdown, text/plain;q=0.9")
	request.Header.Set("User-Agent", "llambo-writing-benchmarks/1")
	response, err := client.Do(request)
	if err != nil {
		return WritingCatalog{}, fmt.Errorf("fetch writing leaderboard: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return WritingCatalog{}, fmt.Errorf("fetch writing leaderboard: HTTP %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxWritingSourceBody+1))
	if err != nil {
		return WritingCatalog{}, fmt.Errorf("read writing leaderboard: %w", err)
	}
	if len(body) > maxWritingSourceBody {
		return WritingCatalog{}, fmt.Errorf("writing leaderboard exceeds %d-byte limit", maxWritingSourceBody)
	}
	rows, err := ParseWritingLeaderboard(string(body))
	if err != nil {
		return WritingCatalog{}, err
	}
	catalog.Leaderboards = []WritingLeaderboard{{
		BenchmarkID: WritingPrimaryID,
		SourceURL:   url,
		FetchedAt:   fetchedAt,
		Rows:        rows,
	}}
	return catalog, nil
}

func ParseWritingLeaderboard(markdown string) ([]WritingLeaderboardRow, error) {
	lines := strings.Split(markdown, "\n")
	headerIndex := -1
	var headers []string
	for index, line := range lines {
		columns := splitMarkdownTableRow(line)
		if len(columns) == 0 {
			continue
		}
		joined := strings.ToLower(strings.Join(columns, " "))
		if strings.Contains(joined, "rank") && strings.Contains(joined, "comparison score") && strings.Contains(joined, "uncertainty") {
			headerIndex = index
			headers = columns
			break
		}
	}
	if headerIndex < 0 {
		return nil, fmt.Errorf("writing leaderboard table header not found")
	}

	rankColumn := findWritingColumn(headers, "rank")
	modelColumn := findWritingColumn(headers, "model")
	scoreColumn := findWritingColumn(headers, "comparison score")
	winChanceColumn := findWritingColumn(headers, "estimated win chance")
	uncertaintyColumn := findWritingColumn(headers, "uncertainty")
	if rankColumn < 0 || modelColumn < 0 || scoreColumn < 0 || winChanceColumn < 0 || uncertaintyColumn < 0 {
		return nil, fmt.Errorf("writing leaderboard table is missing required columns")
	}

	rows := make([]WritingLeaderboardRow, 0, 32)
	for _, line := range lines[headerIndex+1:] {
		columns := splitMarkdownTableRow(line)
		if len(columns) <= maxWritingColumn(rankColumn, modelColumn, scoreColumn, winChanceColumn, uncertaintyColumn) {
			continue
		}
		rank, err := strconv.Atoi(strings.TrimSpace(columns[rankColumn]))
		if err != nil {
			continue
		}
		score, err := parseWritingFloat(columns[scoreColumn])
		if err != nil {
			continue
		}
		winChance, err := parseWritingPercent(columns[winChanceColumn])
		if err != nil {
			continue
		}
		lower, upper, err := parseWritingRange(columns[uncertaintyColumn])
		if err != nil {
			continue
		}
		model := cleanWritingModelName(columns[modelColumn])
		if model == "" {
			continue
		}
		rows = append(rows, WritingLeaderboardRow{
			Rank:      rank,
			Model:     model,
			Score:     score,
			WinChance: winChance,
			Lower:     lower,
			Upper:     upper,
		})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("writing leaderboard table contained no data rows")
	}
	return rows, nil
}

func splitMarkdownTableRow(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.Contains(line, "|") {
		return nil
	}
	columns := strings.Split(line, "|")
	if len(columns) > 0 && strings.TrimSpace(columns[0]) == "" {
		columns = columns[1:]
	}
	if len(columns) > 0 && strings.TrimSpace(columns[len(columns)-1]) == "" {
		columns = columns[:len(columns)-1]
	}
	for index := range columns {
		columns[index] = strings.TrimSpace(columns[index])
	}
	return columns
}

func findWritingColumn(columns []string, want string) int {
	for index, column := range columns {
		normalized := strings.Join(strings.Fields(strings.ToLower(strings.ReplaceAll(column, "_", " "))), " ")
		if strings.Contains(normalized, want) {
			return index
		}
	}
	return -1
}

func maxWritingColumn(columns ...int) int {
	maximum := -1
	for _, column := range columns {
		if column > maximum {
			maximum = column
		}
	}
	return maximum
}

func parseWritingFloat(value string) (float64, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "−", "-"))
	value = strings.Trim(value, "`*$")
	return strconv.ParseFloat(value, 64)
}

func parseWritingPercent(value string) (float64, error) {
	value = strings.TrimSpace(strings.TrimSuffix(value, "%"))
	return parseWritingFloat(value)
}

func parseWritingRange(value string) (float64, float64, error) {
	value = strings.ReplaceAll(strings.ToLower(value), "−", "-")
	value = strings.ReplaceAll(value, "to", " ")
	parts := strings.Fields(value)
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("invalid uncertainty range %q", value)
	}
	lower, err := parseWritingFloat(parts[0])
	if err != nil {
		return 0, 0, err
	}
	upper, err := parseWritingFloat(parts[len(parts)-1])
	if err != nil {
		return 0, 0, err
	}
	return lower, upper, nil
}

func cleanWritingModelName(value string) string {
	value = markdownLinkRE.ReplaceAllString(value, "$1")
	value = strings.TrimSpace(strings.Trim(value, "`"))
	return strings.TrimRight(value, " †‡§*")
}
