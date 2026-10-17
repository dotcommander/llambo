package providers

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

// RoutingIntent classifies a request by task shape for intelligent routing.
type RoutingIntent string

// RoutingMode selects the optimization objective for routing.
type RoutingMode string

// Intent values classify requests; Mode values select routing objectives.
const (
	IntentChat       RoutingIntent = "chat"
	IntentCode       RoutingIntent = "code"
	IntentExtraction RoutingIntent = "extraction"
	IntentLongCtx    RoutingIntent = "long_context"
	IntentEmbeddings RoutingIntent = "embeddings"

	RoutingModeFastest  RoutingMode = "fastest"
	RoutingModeCheapest RoutingMode = "cheapest"
	RoutingModeBalanced RoutingMode = "balanced"
	RoutingModeQuality  RoutingMode = "quality"
)

// CandidateScore is one routed provider's score and reason.
type CandidateScore struct {
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
	Score    float64 `json:"score"`
	Reason   string  `json:"reason"`
}

// RouteDecision records the chosen backend and the scored candidates behind
// it.
type RouteDecision struct {
	Mode         string           `json:"mode"`
	Intent       RoutingIntent    `json:"intent"`
	IntentDetail string           `json:"intent_detail,omitempty"`
	Chosen       string           `json:"chosen"`
	Reason       string           `json:"reason"`
	Candidates   []CandidateScore `json:"candidates,omitempty"`
	IsCanary     bool             `json:"is_canary,omitempty"`
}

// longContextByteThreshold preserves DetectIntent's structural long-context
// rule: prompts whose user content exceeds this byte length route as
// long_context before any embedding work happens.
const longContextByteThreshold = 8000

// DetectIntent infers a routing intent from prompt content.
func DetectIntent(systemPrompt, userContent string) RoutingIntent {
	intent, _ := detectIntentDetailed(systemPrompt, userContent)
	return intent
}

// detectIntentDetailed returns the lexical intent and whether any lexical
// rule actually fired. matched=false means the returned chat intent is the
// inconclusive default, not a positive classification.
func detectIntentDetailed(systemPrompt, userContent string) (RoutingIntent, bool) {
	text := strings.ToLower(systemPrompt + "\n" + userContent)
	if strings.Contains(text, "```") || strings.Contains(text, "function") || strings.Contains(text, "golang") || strings.Contains(text, "python") || strings.Contains(text, "typescript") || strings.Contains(text, "debug") {
		return IntentCode, true
	}
	if strings.Contains(text, "extract") || strings.Contains(text, "summary") || strings.Contains(text, "evidence") {
		return IntentExtraction, true
	}
	if len(userContent) > longContextByteThreshold {
		return IntentLongCtx, true
	}
	return IntentChat, false
}

// IntentSource records which routing layer produced an intent.
type IntentSource uint8

// Intent resolution layers, cheapest first.
const (
	// IntentLexical is the keyword detector (L0).
	IntentLexical IntentSource = iota
	// IntentStatic is the static-embedding centroid match (L1).
	IntentStatic
	// IntentFallback marks the inconclusive default: no lexical rule fired
	// and no confident static match existed.
	IntentFallback
)

// IntentResolution is one resolved routing intent plus the confidence
// signals behind it. Ambiguous resolutions must degrade neutrally in
// capability scoring, never punitively.
type IntentResolution struct {
	Intent    RoutingIntent
	Top1      float32
	Margin    float32
	Source    IntentSource
	Ambiguous bool
}

// ResolveIntent classifies a prompt through the cascaded intent router:
// structural long-context gate, then the static-embedding centroid match
// (when assets and config exist), then the lexical detector, then the
// inconclusive chat fallback. Per the L1 intent router spec, escalation is
// asymmetric: any failed static gate marks the resolution ambiguous so
// capability fit stays neutral.
func ResolveIntent(systemPrompt, userContent string, assets *IntentAssets, cfg *IntentRouterConfig) IntentResolution {
	if len(userContent) > longContextByteThreshold {
		return IntentResolution{Intent: IntentLongCtx, Source: IntentLexical}
	}
	lexical, matched := detectIntentDetailed(systemPrompt, userContent)
	if assets == nil || cfg == nil || !cfg.Enabled || cfg.MinTop1 <= 0 || cfg.MinMargin <= 0 {
		return lexicalResolution(lexical, matched)
	}
	if vec, ok := assets.Embed(systemPrompt + "\n" + userContent); ok {
		if idx, top1, margin := matchCentroid(vec, assets.centroids); idx >= 0 && idx < len(assets.intents) {
			if float64(top1) >= cfg.MinTop1 && float64(margin) >= cfg.MinMargin {
				return IntentResolution{Intent: assets.intents[idx], Top1: top1, Margin: margin, Source: IntentStatic}
			}
			res := lexicalResolution(lexical, matched)
			res.Top1, res.Margin, res.Ambiguous = top1, margin, true
			return res
		}
	}
	res := lexicalResolution(lexical, matched)
	res.Ambiguous = true
	return res
}

func lexicalResolution(intent RoutingIntent, matched bool) IntentResolution {
	if !matched {
		return IntentResolution{Intent: IntentChat, Source: IntentFallback, Ambiguous: true}
	}
	return IntentResolution{Intent: intent, Source: IntentLexical}
}

// ResolutionDetail renders the resolution provenance for decision records.
// It returns "" for plain confident lexical hits to keep routine events
// unchanged.
func ResolutionDetail(r IntentResolution) string {
	switch {
	case r.Source == IntentStatic:
		return fmt.Sprintf("%s (static top1=%.2f margin=%.2f)", r.Intent, r.Top1, r.Margin)
	case r.Ambiguous && r.Top1 > 0:
		return fmt.Sprintf("%s (lexical; static top1=%.2f margin=%.2f below gate)", r.Intent, r.Top1, r.Margin)
	case r.Source == IntentFallback:
		return fmt.Sprintf("%s (no lexical or static signal; neutral fit)", r.Intent)
	case r.Ambiguous:
		return fmt.Sprintf("%s (lexical; static unresolved)", r.Intent)
	default:
		return ""
	}
}

// EstimatePromptTokens approximates the token count as the larger of the
// word count and the bytes/4 character estimate. The word floor catches
// short whitespace-heavy prompts where bytes/4 badly undercounts.
func EstimatePromptTokens(systemPrompt, userContent string) int {
	words := countWords(systemPrompt) + countWords(userContent)
	charEst := (len(systemPrompt) + len(userContent) + 3) / 4
	if words > charEst {
		return words
	}
	return charEst
}

func countWords(s string) int {
	words, inWord := 0, false
	for _, r := range s {
		if unicode.IsSpace(r) {
			inWord = false
			continue
		}
		if !inWord {
			words++
			inWord = true
		}
	}
	return words
}

// NormalizeRoutingMode canonicalizes a routing mode, defaulting to balanced.
func NormalizeRoutingMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case string(RoutingModeFastest):
		return string(RoutingModeFastest)
	case string(RoutingModeCheapest):
		return string(RoutingModeCheapest)
	case string(RoutingModeQuality):
		return string(RoutingModeQuality)
	default:
		return string(RoutingModeBalanced)
	}
}

// ResolveDailyLimitInt64 resolves the provider's integer daily limit with
// global fallback.
func ResolveDailyLimitInt64(limits map[string]int64, provider string) (int64, bool) {
	return resolveDailyLimit(limits, provider)
}

// ResolveDailyLimitFloat64 resolves the provider's float daily limit with
// global fallback.
func ResolveDailyLimitFloat64(limits map[string]float64, provider string) (float64, bool) {
	return resolveDailyLimit(limits, provider)
}

func resolveDailyLimit[T ~int64 | ~float64](limits map[string]T, provider string) (T, bool) {
	if len(limits) == 0 {
		var zero T
		return zero, false
	}
	key := strings.ToLower(provider)
	v, ok := limits[key]
	if !ok {
		v, ok = limits["*"]
	}
	return v, ok && v > 0
}

// candidateScoring carries the request-wide routing inputs shared by every
// candidate evaluated in one scoring pass.
type candidateScoring struct {
	metrics         map[string]ProviderRuntimeMetrics
	mode            string
	routing         RoutingConfig
	estimatedTokens int
	intent          RoutingIntent
	intentAmbiguous bool
	allowed         map[string]struct{}
	denied          map[string]struct{}
}

// providerNameSet lowercases a provider allow/deny list into a set.
func providerNameSet(names []string) map[string]struct{} {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[strings.ToLower(name)] = struct{}{}
	}
	return set
}

// newCandidateScoring assembles the request-wide routing inputs shared by
// every candidate in one scoring pass. The metrics snapshot is referenced,
// never copied or mutated.
func newCandidateScoring(routing RoutingConfig, metrics map[string]ProviderRuntimeMetrics, resolution IntentResolution, estimatedTokens int) candidateScoring {
	return candidateScoring{
		metrics:         metrics,
		mode:            NormalizeRoutingMode(routing.Mode),
		routing:         routing,
		estimatedTokens: estimatedTokens,
		intent:          resolution.Intent,
		intentAmbiguous: resolution.Ambiguous,
		allowed:         providerNameSet(routing.AllowedProviders),
		denied:          providerNameSet(routing.DeniedProviders),
	}
}

// scoreCandidates scores every healthy backend against one request's scoring
// inputs and returns the candidates ordered by descending score.
func scoreCandidates(configs map[string]Config, healthy []Backend, scoring candidateScoring) []CandidateScore {
	scored := make([]CandidateScore, 0, len(healthy))
	for _, b := range healthy {
		cfg, ok := configs[b.Name]
		if !ok {
			continue
		}
		if candidate, ok := scoring.score(cfg, b); ok {
			scored = append(scored, candidate)
		}
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Provider < scored[j].Provider
		}
		return scored[i].Score > scored[j].Score
	})

	return scored
}

// score evaluates one healthy backend against the routing policy, reporting
// ok=false when the candidate is excluded. The filter order — allow/deny,
// context window, cost, daily quota, latency — is part of the routing
// contract and the blended score math is unchanged.
func (s candidateScoring) score(cfg Config, b Backend) (CandidateScore, bool) {
	if !s.providerAllowed(cfg, b.Name) {
		return CandidateScore{}, false
	}

	// Context-window precheck: drop a candidate whose KNOWN context window
	// can't structurally hold the estimated prompt plus a completion reserve.
	// Unknown limits (0) always pass (graceful degradation).
	if !contextFits(cfg.ContextLength, s.estimatedTokens, contextHeadroomTokens) {
		return CandidateScore{}, false
	}

	cost := estimateCostUSD(cfg, s.estimatedTokens)
	if s.routing.MaxCostUSD > 0 && cost > s.routing.MaxCostUSD {
		return CandidateScore{}, false
	}

	m := s.metrics[b.Name]
	if exceedsDailyQuota(s.routing, b.Name, m, s.estimatedTokens, cost) {
		return CandidateScore{}, false
	}

	latencyMs := candidateLatencyMs(cfg, m)
	if s.routing.MaxLatencyMs > 0 && int(latencyMs) > s.routing.MaxLatencyMs {
		return CandidateScore{}, false
	}

	errorRate := 0.0
	if m.Requests > 0 {
		errorRate = float64(m.Failures) / float64(m.Requests)
	}

	fit := capabilityFit(cfg.Capabilities, s.intent, s.intentAmbiguous)
	score, reason := applyModeScoreNormalized(s.mode, modeScoreInputs{quality: cfg.Quality, runtimeQuality: runtimeQualityScore(m), fit: fit, latencyMs: latencyMs, costUSD: cost, errorRate: errorRate})
	return CandidateScore{Provider: b.Name, Model: b.Model, Score: score, Reason: reason}, true
}

// providerAllowed applies allow/deny membership on both the full backend name
// and its base provider name; an empty allow list admits everything.
func (s candidateScoring) providerAllowed(cfg Config, name string) bool {
	lname := strings.ToLower(name)
	lbase := strings.ToLower(BaseProviderName(name, cfg.Model))
	if len(s.allowed) > 0 {
		if _, ok := s.allowed[lname]; !ok {
			if _, ok := s.allowed[lbase]; !ok {
				return false
			}
		}
	}
	if _, blocked := s.denied[lname]; blocked {
		return false
	}
	_, blocked := s.denied[lbase]
	return !blocked
}

// candidateLatencyMs resolves a candidate's expected latency, preferring the
// configured value, then the measured average, then a neutral default.
func candidateLatencyMs(cfg Config, m ProviderRuntimeMetrics) float64 {
	latencyMs := float64(cfg.ExpectedLatencyMs)
	if latencyMs <= 0 && m.Requests > 0 {
		latencyMs = float64(m.TotalLatencyMs) / float64(m.Requests)
	}
	if latencyMs <= 0 {
		latencyMs = 1200
	}
	return latencyMs
}

func exceedsDailyQuota(routing RoutingConfig, provider string, m ProviderRuntimeMetrics, estimatedTokens int, estimatedCost float64) bool {
	today := time.Now().UTC().Format(dailyWindowFormat)
	if m.DailyWindow != today {
		m.DailyRequests = 0
		m.DailyTokens = 0
		m.DailyCostUSD = 0
	}

	if limit, ok := ResolveDailyLimitInt64(routing.DailyMaxRequests, provider); ok {
		if m.DailyRequests >= limit {
			return true
		}
	}
	if limit, ok := ResolveDailyLimitInt64(routing.DailyMaxTokens, provider); ok {
		if m.DailyTokens+int64(estimatedTokens) > limit {
			return true
		}
	}
	if limit, ok := ResolveDailyLimitFloat64(routing.DailyMaxCostUSD, provider); ok {
		if m.DailyCostUSD+estimatedCost > limit {
			return true
		}
	}

	return false
}

// capabilityFit scores how well a backend's declared capabilities match the
// resolved intent. Ambiguous resolutions (and tag-less configs) score the
// neutral 0.5: an uncertain classification must never punish candidates.
func capabilityFit(capabilities []string, intent RoutingIntent, ambiguous bool) float64 {
	if len(capabilities) == 0 || ambiguous {
		return 0.5
	}
	needle := strings.ToLower(string(intent))
	for _, c := range capabilities {
		if strings.ToLower(c) == needle {
			return 1.0
		}
	}
	return 0.3
}

// runtimeQualityScore returns the rolling quality score for a provider.
// Returns 0.5 (neutral) when no quality data has been recorded.
func runtimeQualityScore(m ProviderRuntimeMetrics) float64 {
	if m.QualityCount == 0 {
		return 0.5
	}
	return m.QualityScore
}

// modeScoreInputs carries the normalized per-candidate measurements that
// routing-mode scoring blends into one score.
type modeScoreInputs struct {
	quality        float64
	runtimeQuality float64
	fit            float64
	latencyMs      float64
	costUSD        float64
	errorRate      float64
}

func applyModeScoreNormalized(mode string, in modeScoreInputs) (float64, string) {
	latencyScore := 1.0 / (1.0 + in.latencyMs/1000.0)
	costScore := 1.0 / (1.0 + in.costUSD*1000.0)
	reliability := 1.0 - in.errorRate
	blendedQuality := in.quality*0.6 + in.runtimeQuality*0.4
	reason := func(policy string) string {
		return fmt.Sprintf("%s: quality=%.2f runtime_quality=%.2f fit=%.2f latency=%.0fms estimated_cost=$%.6f reliability=%.2f",
			policy, in.quality, in.runtimeQuality, in.fit, in.latencyMs, in.costUSD, reliability)
	}

	switch mode {
	case string(RoutingModeFastest):
		return 0.65*latencyScore + 0.15*reliability + 0.15*in.fit + 0.05*blendedQuality, reason("lowest latency weighted")
	case string(RoutingModeCheapest):
		return 0.65*costScore + 0.15*reliability + 0.15*in.fit + 0.05*blendedQuality, reason("lowest cost weighted")
	case string(RoutingModeQuality):
		return 0.55*blendedQuality + 0.25*in.fit + 0.20*reliability, reason("highest quality weighted")
	default:
		return 0.30*blendedQuality + 0.20*in.fit + 0.20*latencyScore + 0.15*costScore + 0.15*reliability, reason("balanced policy")
	}
}
