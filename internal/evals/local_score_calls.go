package evals

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// ScoreLocalCalls derives suite scores only from sealed inputs and recorded outputs.
func ScoreLocalCalls(m LocalRunManifest, calls []LocalCall) (LocalReport, error) {
	p := localScoringProgress{report: LocalReport{RunID: m.RunID, Suite: m.Identity.Suite, ScoreIdentity: string(m.Identity.Suite), Status: completeQuality, Metrics: map[string]float64{}}}
	if len(calls) != m.Identity.Limit {
		return p.report, fmt.Errorf("have %d calls, want %d", len(calls), m.Identity.Limit)
	}
	for i, call := range calls {
		if call.Error != "" {
			return p.report, fmt.Errorf("case %d: %s", i+1, call.Error)
		}
		if err := p.addCall(m.Identity.Suite, call); err != nil {
			return p.report, err
		}
	}
	if err := p.finalize(m); err != nil {
		return p.report, err
	}
	return p.report, nil
}

type localScoringProgress struct {
	report            LocalReport
	scores            []float64
	ttsRoundTripCases int
}

func (p *localScoringProgress) addCall(suite LocalSuite, call LocalCall) error {
	switch suite {
	case LocalSuiteTools:
		return p.addToolCall(call)
	case LocalSuiteASR:
		return p.addASRCall(call)
	case LocalSuiteTTS:
		return p.addTTSCall(call)
	case LocalSuiteAcceleration:
		return p.addAccelerationCall(call)
	case LocalSuiteEmbeddings:
		return p.addEmbeddingCall(call)
	default:
		return fmt.Errorf("unsupported suite %q", suite)
	}
}

func (p *localScoringProgress) addToolCall(call LocalCall) error {
	var in ToolCase
	var out struct {
		Calls []ToolCall `json:"calls"`
	}
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return err
	}
	if err := json.Unmarshal(call.Output, &out); err != nil {
		return err
	}
	in.Calls = out.Calls
	ok, err := ScoreToolCase(in)
	if err != nil {
		return err
	}
	if ok {
		p.scores = append(p.scores, 100)
	} else {
		p.scores = append(p.scores, 0)
	}
	return nil
}

func (p *localScoringProgress) addASRCall(call LocalCall) error {
	var in ASRCase
	var out struct {
		Transcript                          string `json:"transcript"`
		WER, CER, LatencyMS, RealTimeFactor float64
	}
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return err
	}
	if err := json.Unmarshal(call.Output, &out); err != nil {
		return err
	}
	wer := WordErrorRate(in.Transcript, out.Transcript)
	p.scores = append(p.scores, 100*math.Max(0, 1-math.Min(wer, 1)))
	p.report.Metrics["wer"] += wer
	p.report.Metrics["cer"] += CharErrorRate(in.Transcript, out.Transcript)
	p.report.Metrics["latency_ms"] += out.LatencyMS
	p.report.Metrics["real_time_factor"] += out.RealTimeFactor
	return nil
}

func (p *localScoringProgress) addTTSCall(call LocalCall) error {
	var out struct {
		WAVBase64          string   `json:"wav_base64"`
		IntelligibilityWER *float64 `json:"intelligibility_wer,omitempty"`
	}
	if err := json.Unmarshal(call.Output, &out); err != nil {
		return err
	}
	wav, err := base64.StdEncoding.DecodeString(out.WAVBase64)
	if err != nil {
		return err
	}
	if _, err := ValidateWAV(wav); err != nil {
		return err
	}
	p.scores = append(p.scores, 100)
	if out.IntelligibilityWER != nil {
		if math.IsNaN(*out.IntelligibilityWER) || math.IsInf(*out.IntelligibilityWER, 0) || *out.IntelligibilityWER < 0 {
			return errors.New("invalid TTS intelligibility WER")
		}
		p.ttsRoundTripCases++
		p.report.Metrics["intelligibility_wer"] += *out.IntelligibilityWER
	}
	return nil
}

func (p *localScoringProgress) addAccelerationCall(call LocalCall) error {
	var out struct{ Off, On []PairedResult }
	if err := json.Unmarshal(call.Output, &out); err != nil {
		return err
	}
	paired, err := ScorePaired(out.Off, out.On)
	if err != nil {
		return err
	}
	p.scores = append(p.scores, 100*paired.PassRate)
	for _, result := range out.Off {
		if result.AnswerCorrect {
			p.report.Metrics["off_pass_rate"]++
		}
		p.report.Metrics["off_latency_ms"] += result.LatencyMS
		p.report.Metrics["off_tokens_per_second"] += result.TokensPerSecond
	}
	for _, result := range out.On {
		if result.AnswerCorrect {
			p.report.Metrics["on_pass_rate"]++
		}
		p.report.Metrics["on_latency_ms"] += result.LatencyMS
		p.report.Metrics["on_tokens_per_second"] += result.TokensPerSecond
	}
	p.report.Metrics["latency_ratio"] += paired.LatencyRatio
	p.report.Metrics["tokens_per_second_ratio"] += paired.TokensPerSecondRatio
	p.report.Status = "complete_paired"
	p.report.ScoreIdentity = "acceleration/exact-answer-pass-rate"
	return nil
}

func (p *localScoringProgress) addEmbeddingCall(call LocalCall) error {
	var in struct {
		Queries []RetrievalQuery `json:"queries"`
	}
	var out struct {
		Scores map[string]map[string]float64 `json:"scores"`
	}
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return err
	}
	if err := json.Unmarshal(call.Output, &out); err != nil {
		return err
	}
	metrics, err := ScoreRetrieval(in.Queries, out.Scores)
	if err != nil {
		return err
	}
	p.scores = append(p.scores, 100*metrics.NDCGAt10)
	p.report.Metrics["recall_at_1"] = metrics.RecallAt1
	p.report.Metrics["mrr_at_10"] = metrics.MRRAt10
	p.report.Metrics["ndcg_at_10"] = metrics.NDCGAt10
	p.report.ScoreIdentity = "embeddings/ndcg@10"
	return nil
}

func (p *localScoringProgress) finalize(m LocalRunManifest) error {
	for _, score := range p.scores {
		p.report.Score += score
	}
	p.report.Score /= float64(len(p.scores))
	if m.Identity.Suite == LocalSuiteASR {
		for _, key := range []string{"wer", "cer", "latency_ms", "real_time_factor"} {
			p.report.Metrics[key] /= float64(len(p.scores))
		}
	}
	if m.Identity.Suite == LocalSuiteTools {
		p.report.Metrics["pass_percent"] = p.report.Score
	}
	if m.Identity.Suite == LocalSuiteAcceleration {
		for _, key := range []string{"off_pass_rate", "on_pass_rate", "off_latency_ms", "on_latency_ms", "off_tokens_per_second", "on_tokens_per_second", "latency_ratio", "tokens_per_second_ratio"} {
			p.report.Metrics[key] /= float64(len(p.scores))
		}
	}
	if m.Identity.Suite == LocalSuiteTTS {
		p.report.Metrics["technical_validity"] = 1
		switch {
		case p.ttsRoundTripCases == 0:
			p.report.ScoreIdentity = "tts/technical-validity-only"
		case p.ttsRoundTripCases != len(p.scores):
			return fmt.Errorf("TTS round-trip intelligibility is incomplete: %d of %d cases", p.ttsRoundTripCases, len(p.scores))
		default:
			p.report.Metrics["intelligibility_wer"] /= float64(p.ttsRoundTripCases)
			p.report.ScoreIdentity = "tts/technical-validity+roundtrip-intelligibility"
		}
	}
	if m.Identity.Limit != requiredLocalCaseCount(m.Identity.Suite) {
		p.report.Status = partial1
		p.report.Errors = append(p.report.Errors, fmt.Sprintf("canary or incomplete suite: %d cases; coverage requires %d", m.Identity.Limit, requiredLocalCaseCount(m.Identity.Suite)))
	}
	return nil
}

func requiredLocalCaseCount(s LocalSuite) int {
	switch s {
	case LocalSuiteTools, LocalSuiteASR:
		return 10
	case LocalSuiteTTS, LocalSuiteAcceleration:
		return 5
	case LocalSuiteEmbeddings:
		return 1
	default:
		return -1
	}
}
