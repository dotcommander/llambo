package evals

import (
	"fmt"
	"strings"
	"unicode"
)

var modelAliases = map[string]string{
	"qwen3-6-27b":            "qwen3.6-27b",
	"gemma-4-26b-a4b":        "gemma-4-26b-a4b-it",
	"gpt-oss-20b":            "gpt-oss-20b",
	"gemini-2-5-flash":       "gemini-2.5-flash",
	"gemini-2-5-flash-lite":  "gemini-2.5-flash-lite",
	"gemini-2-5-pro":         "gemini-2.5-pro",
	"gemini-3-flash-preview": "gemini-3-flash-preview",
	"gemini-3-1-flash-lite":  "gemini-3.1-flash-lite",
	"gemini-3-1-pro-preview": "gemini-3.1-pro-preview",
	"gemini-3-5-flash":       "gemini-3.5-flash",
	"gemma-4-26b-a4b-it":     "gemma-4-26b-a4b-it",
}

// officialCardKeyAlias resolves only the reviewed external Gemma IDs. Generic
// gpt-oss IDs must not be promoted to the card's high-reasoning configuration.
func officialCardKeyAlias(key string) (string, bool) {
	switch key {
	case "google/gemma-4-26B-A4B-it":
		return "gemma-4-26b-a4b-it", true
	case gemma431BHostedID:
		return gemma431BModelKey, true
	default:
		return "", false
	}
}

// organizationFamilies is the reviewed boundary for source-native organization
// labels. Values identify one model-producing family; unrelated organizations
// must never be collapsed merely because a model name happens to match.
var organizationFamilies = map[string]string{
	"alibaba":                 "alibaba",
	"alibaba cloud qwen team": "alibaba",
	"bytedance":               "bytedance",
	"bytedance seed":          "bytedance",
	"kimi":                    "moonshot",
	"moonshot ai":             "moonshot",
	"mistral":                 "mistral",
	"mistral ai":              "mistral",
	"sarvam":                  "sarvam",
	"sarvam ai":               "sarvam",
	"z ai":                    "zhipu",
	"zhipu ai":                "zhipu",
}

func mergeModels(llm, aa []Model) []Model {
	out := append([]Model(nil), llm...)
	llmByIdentity := make(map[string][]int, len(out))
	for i := range out {
		out[i].IdentityMatch = IdentityMatchUnmatched
		if identity, ok := externalIdentity(out[i]); ok {
			llmByIdentity[identity] = append(llmByIdentity[identity], i)
		}
	}
	aaByIdentity := make(map[string][]int, len(aa))
	for i := range aa {
		if identity, ok := externalIdentity(aa[i]); ok {
			aaByIdentity[identity] = append(aaByIdentity[identity], i)
		}
	}

	for identity, indices := range llmByIdentity {
		if len(indices) > 1 || len(aaByIdentity[identity]) > 1 {
			for _, index := range indices {
				out[index].IdentityMatch = IdentityMatchAmbiguous
			}
		}
	}
	for identity, indices := range aaByIdentity {
		if len(indices) > 1 || len(llmByIdentity[identity]) > 1 {
			for _, index := range indices {
				aa[index].IdentityMatch = IdentityMatchAmbiguous
			}
		}
	}

	for i, model := range aa {
		if model.AA != nil {
			model.AA.sourceName = model.Name
			model.AA.sourceOrganization = model.Organization
		}
		identity, hasIdentity := externalIdentity(model)
		llmIndices := llmByIdentity[identity]
		aaIndices := aaByIdentity[identity]
		if hasIdentity && len(llmIndices) == 1 && len(aaIndices) == 1 {
			index := llmIndices[0]
			out[index].AA = model.AA
			out[index].IdentityMatch = IdentityMatchNormalized
			if normalizeIdentityText(out[index].Key, true) == normalizeIdentityText(model.Key, true) {
				out[index].IdentityMatch = IdentityMatchExact
			}
			continue
		}
		if model.IdentityMatch == "" {
			model.IdentityMatch = IdentityMatchUnmatched
		}
		model.Key = "aa:" + canonicalKey(model.Key)
		aa[i] = model
		out = append(out, model)
	}
	return out
}

// mergeWritingBenchModels only attaches source-native scores when both sides
// have one conservative identity match. Ambiguous or unmatched rows stay out
// of the capability report instead of guessing an identity.
func mergeWritingBenchModels(models, writingBench []Model) []Model {
	byIdentity := make(map[string][]int, len(models))
	for i, model := range models {
		if identity, ok := externalIdentity(model); ok {
			byIdentity[identity] = append(byIdentity[identity], i)
		}
	}
	benchByIdentity := make(map[string][]Model, len(writingBench))
	for _, model := range writingBench {
		if identity, ok := externalIdentity(model); ok {
			benchByIdentity[identity] = append(benchByIdentity[identity], model)
		}
	}
	for identity, benchmarks := range benchByIdentity {
		indices := byIdentity[identity]
		if len(indices) != 1 || len(benchmarks) != 1 {
			continue
		}
		if models[indices[0]].Benchmarks == nil {
			models[indices[0]].Benchmarks = make(map[string]BenchmarkResult)
		}
		for name, result := range benchmarks[0].Benchmarks {
			result.Identity = benchmarkIdentityMatch(models[indices[0]], benchmarks[0])
			storeBenchmarkResult(models[indices[0]].Benchmarks, name, result)
		}
	}
	return models
}

func mergeWritingEvidenceModels(models, writingBench []Model, corroborators ...[]Model) []Model {
	models = mergeWritingBenchModels(models, writingBench)
	for _, source := range corroborators {
		models = mergeEQBenchCreativeModels(models, source)
	}
	return models
}

// mergeEQBenchCreativeModels permits name-only matching only when the source
// omits organization and both source and destination have exactly one normalized
// name. This is the narrowest safe reconciliation for the official CSV.
func mergeEQBenchCreativeModels(models, eqBench []Model) []Model {
	byIdentity := make(map[string][]int, len(models))
	byName := make(map[string][]int, len(models))
	byKey := make(map[string]int, len(models))
	for i, model := range models {
		byKey[model.Key] = i
		if identity, ok := externalIdentity(model); ok {
			byIdentity[identity] = append(byIdentity[identity], i)
		}
		if name := normalizeIdentityText(model.Name, true); name != "" {
			byName[name] = append(byName[name], i)
		}
	}
	benchIdentity := make(map[string][]Model, len(eqBench))
	benchName := make(map[string][]Model, len(eqBench))
	for _, model := range eqBench {
		if alias, ok := officialCardKeyAlias(model.Name); ok {
			if index, found := byKey[alias]; found {
				attachBenchmark(&models[index], model, IdentityMatchExact)
				continue
			}
		}
		if identity, ok := externalIdentity(model); ok {
			benchIdentity[identity] = append(benchIdentity[identity], model)
		}
		if name := normalizeIdentityText(model.Name, true); name != "" {
			benchName[name] = append(benchName[name], model)
		}
	}
	for identity, rows := range benchIdentity {
		if indices := byIdentity[identity]; len(rows) == 1 && len(indices) == 1 {
			attachBenchmark(&models[indices[0]], rows[0], benchmarkIdentityMatch(models[indices[0]], rows[0]))
		}
	}
	for name, rows := range benchName {
		if rows[0].Organization != "" || len(rows) != 1 || len(byName[name]) != 1 {
			continue
		}
		if existing := models[byName[name][0]].Benchmarks; existing != nil {
			if _, ok := existing["eqbench-creative-v3"]; ok {
				continue
			}
		}
		attachBenchmark(&models[byName[name][0]], rows[0], IdentityMatchNormalized)
	}
	return models
}

func attachBenchmark(target *Model, source Model, identity IdentityMatch) {
	if target.Benchmarks == nil {
		target.Benchmarks = make(map[string]BenchmarkResult)
	}
	for name, result := range source.Benchmarks {
		result.Identity = identity
		storeBenchmarkResult(target.Benchmarks, name, result)
	}
}

func storeBenchmarkResult(results map[string]BenchmarkResult, benchmark string, candidate BenchmarkResult) {
	current, exists := results[benchmark]
	if !exists {
		results[benchmark] = candidate
		return
	}
	currentRank, candidateRank := sourceAuthority(current.SourceClass), sourceAuthority(candidate.SourceClass)
	if candidateRank > currentRank {
		candidate.Mirrors = append(candidate.Mirrors, benchmarkMirror(current))
		results[benchmark] = candidate
		return
	}
	if candidateRank < currentRank {
		current.Mirrors = append(current.Mirrors, benchmarkMirror(candidate))
		results[benchmark] = current
		return
	}
	if benchmarkValuesConflict(current, candidate) {
		current.Quarantined = true
		current.Conflict = fmt.Sprintf("equal-authority conflict between %s and %s", benchmarkSourceLabel(current), benchmarkSourceLabel(candidate))
		current.Mirrors = append(current.Mirrors, benchmarkMirror(candidate))
		results[benchmark] = current
		return
	}
	// Deterministic precedence within an equal-authority mirror set keeps one
	// authoritative value while retaining the other as corroborating provenance.
	if benchmarkSourceLabel(candidate) < benchmarkSourceLabel(current) {
		candidate.Mirrors = append(candidate.Mirrors, benchmarkMirror(current))
		results[benchmark] = candidate
		return
	}
	current.Mirrors = append(current.Mirrors, benchmarkMirror(candidate))
	results[benchmark] = current
}

func sourceAuthority(class string) int {
	switch SourceClass(class) {
	case SourceOwnerResult:
		return 4
	case SourceFirstPartyResult, SourceOfficialJudgment:
		return 3
	case SourceAggregatorResult:
		return 2
	default:
		return 1
	}
}

func benchmarkValuesConflict(left, right BenchmarkResult) bool {
	if left.Score == nil || right.Score == nil {
		return left.Score != right.Score
	}
	return *left.Score != *right.Score || left.Version != right.Version || left.Direction != right.Direction
}

func benchmarkMirror(result BenchmarkResult) BenchmarkMirror {
	return BenchmarkMirror{SourceID: result.SourceID, SourceClass: result.SourceClass, SourceRevision: result.SourceRevision, ContentSHA: result.ContentSHA, Locator: result.Locator}
}

func benchmarkSourceLabel(result BenchmarkResult) string {
	if strings.TrimSpace(result.SourceID) != "" {
		return result.SourceID
	}
	if strings.TrimSpace(result.Locator) != "" {
		return result.Locator
	}
	return strings.TrimSpace(result.SourceClass)
}

func benchmarkIdentityMatch(target, source Model) IdentityMatch {
	if strings.TrimSpace(target.Name) == strings.TrimSpace(source.Name) && strings.TrimSpace(target.Organization) == strings.TrimSpace(source.Organization) {
		return IdentityMatchExact
	}
	return IdentityMatchNormalized
}

func externalIdentity(model Model) (string, bool) {
	name := normalizeIdentityText(model.Name, true)
	organization := canonicalOrganizationFamily(model.Organization)
	if name == "" || organization == "" {
		return "", false
	}
	return organization + "\x00" + name, true
}

func canonicalOrganizationFamily(organization string) string {
	normalized := normalizeIdentityText(organization, false)
	if normalized == "" {
		return ""
	}
	if family, ok := organizationFamilies[normalized]; ok {
		return family
	}
	return normalized
}

// normalizeIdentityText normalizes formatting differences without erasing
// semantic qualifiers. In particular, '+' remains part of a model name so
// variants such as Command A and Command A+ cannot collide.
func normalizeIdentityText(value string, preservePlus bool) string {
	var b strings.Builder
	separator := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || (preservePlus && r == '+') {
			if separator && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			separator = false
			continue
		}
		separator = true
	}
	return b.String()
}

func canonicalKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", "-")
	if alias, ok := modelAliases[s]; ok {
		return alias
	}
	return s
}
