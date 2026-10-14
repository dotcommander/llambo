package evals

import (
	"fmt"
	"strings"
	"unicode"
)

// Repeated string literals consolidated by goconst.
const (
	alibaba1         = "alibaba"
	bytedance1       = "bytedance"
	gemmaX4X26bA4bIt = "gemma-4-26b-a4b-it"
	mistral1         = "mistral"
	sarvam1          = "sarvam"
)

var modelAliases = map[string]string{
	"qwen3-6-27b":            "qwen3.6-27b",
	"gemma-4-26b-a4b":        gemmaX4X26bA4bIt,
	"gpt-oss-20b":            "gpt-oss-20b",
	"gemini-2-5-flash":       "gemini-2.5-flash",
	"gemini-2-5-flash-lite":  "gemini-2.5-flash-lite",
	"gemini-2-5-pro":         "gemini-2.5-pro",
	"gemini-3-flash-preview": "gemini-3-flash-preview",
	"gemini-3-1-flash-lite":  "gemini-3.1-flash-lite",
	"gemini-3-1-pro-preview": "gemini-3.1-pro-preview",
	"gemini-3-5-flash":       "gemini-3.5-flash",
	gemmaX4X26bA4bIt:         gemmaX4X26bA4bIt,
}

// officialCardKeyAlias resolves only the reviewed external Gemma IDs. Generic
// gpt-oss IDs must not be promoted to the card's high-reasoning configuration.
func officialCardKeyAlias(key string) (string, bool) {
	switch key {
	case "google/gemma-4-26B-A4B-it":
		return gemmaX4X26bA4bIt, true
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
	alibaba1:                  alibaba1,
	"alibaba cloud qwen team": alibaba1,
	bytedance1:                bytedance1,
	"bytedance seed":          bytedance1,
	"kimi":                    "moonshot",
	"moonshot ai":             "moonshot",
	mistral1:                  mistral1,
	"mistral ai":              mistral1,
	sarvam1:                   sarvam1,
	"sarvam ai":               sarvam1,
	"z ai":                    "zhipu",
	"zhipu ai":                "zhipu",
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
	case SourceAggregatorResult, SourceAggregatorLead:
		return 2
	case SourceDefinitionOnly:
		return 1
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
