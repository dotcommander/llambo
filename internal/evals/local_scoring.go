package evals

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
)

type ToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}
type ASRCase struct {
	ID         string `json:"id"`
	Transcript string `json:"transcript"`
	WAVPath    string `json:"wav_path"`
	WAVSHA256  string `json:"wav_sha256"`
}
type TTSCase struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
}
type EmbeddingCase struct {
	ID        string           `json:"id"`
	Documents []EmbeddingText  `json:"documents"`
	Queries   []EmbeddingQuery `json:"queries"`
}
type EmbeddingText struct{ ID, Text string }
type EmbeddingQuery struct {
	ID, Text string
	Relevant []string `json:"relevant"`
}
type AccelerationCase struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
	Oracle string `json:"oracle"`
}
type ToolCase struct {
	ID       string           `json:"id"`
	Prompt   string           `json:"prompt"`
	Tools    []ToolDefinition `json:"tools"`
	Expected *ToolCall        `json:"expected,omitempty"`
	Calls    []ToolCall       `json:"calls"`
}

func ScoreToolCase(c ToolCase) (bool, error) {
	if c.ID == "" {
		return false, fmt.Errorf("tool case id is required")
	}
	if c.Expected == nil {
		return len(c.Calls) == 0, nil
	}
	if len(c.Calls) != 1 || c.Calls[0].Name != c.Expected.Name {
		return false, nil
	}
	a, e := canonicalJSON(c.Calls[0].Arguments)
	if e != nil {
		return false, e
	}
	b, e := canonicalJSON(c.Expected.Arguments)
	if e != nil {
		return false, e
	}
	return bytes.Equal(a, b), nil
}
func ScoreTools(cases []ToolCase) (float64, error) {
	if len(cases) == 0 {
		return 0, fmt.Errorf("no tool cases")
	}
	pass := 0
	for _, c := range cases {
		ok, e := ScoreToolCase(c)
		if e != nil {
			return 0, e
		}
		if ok {
			pass++
		}
	}
	return 100 * float64(pass) / float64(len(cases)), nil
}

func NormalizeTranscript(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSpace(r) }), " ")
}
func WordErrorRate(reference, hypothesis string) float64 {
	r := strings.Fields(NormalizeTranscript(reference))
	h := strings.Fields(NormalizeTranscript(hypothesis))
	return float64(editDistance(r, h)) / float64(max(1, len(r)))
}
func CharErrorRate(reference, hypothesis string) float64 {
	r := []rune(strings.ReplaceAll(NormalizeTranscript(reference), " ", ""))
	h := []rune(strings.ReplaceAll(NormalizeTranscript(hypothesis), " ", ""))
	return float64(editDistance(r, h)) / float64(max(1, len(r)))
}
func editDistance[T comparable](a, b []T) int {
	d := make([]int, len(b)+1)
	for j := range d {
		d[j] = j
	}
	for i, x := range a {
		prior := d[0]
		d[0] = i + 1
		for j, y := range b {
			old := d[j+1]
			cost := 0
			if x != y {
				cost = 1
			}
			d[j+1] = min(d[j+1]+1, d[j]+1, prior+cost)
			prior = old
		}
	}
	return d[len(b)]
}

type WAVMetadata struct {
	SampleRate      uint32
	Channels        uint16
	BitsPerSample   uint16
	DurationSeconds float64
	DataBytes       uint32
}

func ValidateWAV(data []byte) (WAVMetadata, error) {
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return WAVMetadata{}, fmt.Errorf("not a RIFF/WAVE file")
	}
	var f WAVMetadata
	foundFmt, foundData := false, false
	for p := 12; p+8 <= len(data); {
		id := string(data[p : p+4])
		n := int(binary.LittleEndian.Uint32(data[p+4 : p+8]))
		p += 8
		if n < 0 || p+n > len(data) {
			return WAVMetadata{}, fmt.Errorf("invalid WAV chunk")
		}
		chunk := data[p : p+n]
		switch id {
		case "fmt ":
			if len(chunk) < 16 {
				return WAVMetadata{}, fmt.Errorf("short WAV fmt chunk")
			}
			f.Channels = binary.LittleEndian.Uint16(chunk[2:4])
			f.SampleRate = binary.LittleEndian.Uint32(chunk[4:8])
			f.BitsPerSample = binary.LittleEndian.Uint16(chunk[14:16])
			foundFmt = true
		case "data":
			f.DataBytes = uint32(n)
			foundData = true
		}
		p += n
		if n%2 == 1 {
			p++
		}
	}
	if !foundFmt || !foundData || f.Channels == 0 || f.SampleRate == 0 || f.BitsPerSample == 0 || f.DataBytes == 0 {
		return WAVMetadata{}, fmt.Errorf("WAV requires nonempty fmt and data chunks")
	}
	bytesPerSecond := float64(f.SampleRate) * float64(f.Channels) * float64(f.BitsPerSample) / 8
	f.DurationSeconds = float64(f.DataBytes) / bytesPerSecond
	if f.DurationSeconds <= 0 || math.IsInf(f.DurationSeconds, 0) || math.IsNaN(f.DurationSeconds) {
		return WAVMetadata{}, fmt.Errorf("invalid WAV duration")
	}
	return f, nil
}

type RetrievalQuery struct {
	ID       string   `json:"id"`
	Relevant []string `json:"relevant"`
}

func ScoreRetrieval(queries []RetrievalQuery, scores map[string]map[string]float64) (recall1, mrr10, ndcg10 float64, error error) {
	if len(queries) == 0 {
		return 0, 0, 0, fmt.Errorf("no retrieval queries")
	}
	for _, q := range queries {
		if q.ID == "" || len(q.Relevant) == 0 {
			return 0, 0, 0, fmt.Errorf("invalid retrieval query")
		}
		ranked := rankScores(scores[q.ID])
		rel := map[string]bool{}
		for _, id := range q.Relevant {
			rel[id] = true
		}
		if len(ranked) > 0 && rel[ranked[0]] {
			recall1++
		}
		dcg := 0.0
		first := firstRank(ranked, rel)
		for i, id := range ranked {
			if i == 10 {
				break
			}
			if rel[id] {
				dcg += 1 / math.Log2(float64(i)+2)
				if first == i {
					mrr10 += 1 / float64(i+1)
				}
			}
		}
		ideal := 0.0
		for i := 0; i < min(10, len(rel)); i++ {
			ideal += 1 / math.Log2(float64(i)+2)
		}
		if ideal > 0 {
			ndcg10 += dcg / ideal
		}
	}
	n := float64(len(queries))
	return recall1 / n, mrr10 / n, ndcg10 / n, nil
}
func firstRank(ids []string, rel map[string]bool) int {
	for i, id := range ids {
		if rel[id] {
			return i
		}
	}
	return -1
}
func rankScores(m map[string]float64) []string {
	ids := make([]string, 0, len(m))
	for id, v := range m {
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return m[ids[i]] > m[ids[j]] || (m[ids[i]] == m[ids[j]] && ids[i] < ids[j]) })
	return ids
}

type PairedResult struct {
	AnswerCorrect   bool    `json:"answer_correct"`
	LatencyMS       float64 `json:"latency_ms"`
	TokensPerSecond float64 `json:"tokens_per_second"`
}
type PairedScore struct {
	PassRate             float64 `json:"pass_rate"`
	LatencyRatio         float64 `json:"latency_ratio"`
	TokensPerSecondRatio float64 `json:"tokens_per_second_ratio"`
}

func ScorePaired(off, on []PairedResult) (PairedScore, error) {
	if len(off) == 0 || len(off) != len(on) {
		return PairedScore{}, fmt.Errorf("paired results must be nonempty and equal length")
	}
	var p, lat, tps float64
	for i := range off {
		if off[i].LatencyMS <= 0 || on[i].LatencyMS <= 0 || off[i].TokensPerSecond <= 0 || on[i].TokensPerSecond <= 0 {
			return PairedScore{}, fmt.Errorf("paired timing metrics must be positive")
		}
		if on[i].AnswerCorrect {
			p++
		}
		lat += off[i].LatencyMS / on[i].LatencyMS
		tps += on[i].TokensPerSecond / off[i].TokensPerSecond
	}
	n := float64(len(off))
	return PairedScore{p / n, lat / n, tps / n}, nil
}

// ScoreLocalCalls deterministically derives the suite report from sealed inputs
// and recorded outputs. Provider executors never supply their own final score.
func ScoreLocalCalls(m LocalRunManifest, calls []LocalCall) (LocalReport, error) {
	report := LocalReport{RunID: m.RunID, Suite: m.Identity.Suite, ScoreIdentity: string(m.Identity.Suite), Status: "complete_quality", Metrics: map[string]float64{}}
	if len(calls) != m.Identity.Limit {
		return report, fmt.Errorf("have %d calls, want %d", len(calls), m.Identity.Limit)
	}
	var scores []float64
	ttsRoundTripCases := 0
	for i, call := range calls {
		if call.Error != "" {
			return report, fmt.Errorf("case %d: %s", i+1, call.Error)
		}
		switch m.Identity.Suite {
		case LocalSuiteTools:
			var in ToolCase
			var out struct {
				Calls []ToolCall `json:"calls"`
			}
			if err := json.Unmarshal(call.Input, &in); err != nil {
				return report, err
			}
			if err := json.Unmarshal(call.Output, &out); err != nil {
				return report, err
			}
			in.Calls = out.Calls
			ok, err := ScoreToolCase(in)
			if err != nil {
				return report, err
			}
			if ok {
				scores = append(scores, 100)
			} else {
				scores = append(scores, 0)
			}
		case LocalSuiteASR:
			var in ASRCase
			var out struct {
				Transcript                          string `json:"transcript"`
				WER, CER, LatencyMS, RealTimeFactor float64
			}
			if err := json.Unmarshal(call.Input, &in); err != nil {
				return report, err
			}
			if err := json.Unmarshal(call.Output, &out); err != nil {
				return report, err
			}
			wer := WordErrorRate(in.Transcript, out.Transcript)
			scores = append(scores, 100*math.Max(0, 1-math.Min(wer, 1)))
			report.Metrics["wer"] += wer
			report.Metrics["cer"] += CharErrorRate(in.Transcript, out.Transcript)
			report.Metrics["latency_ms"] += out.LatencyMS
			report.Metrics["real_time_factor"] += out.RealTimeFactor
		case LocalSuiteTTS:
			var out struct {
				WAVBase64          string   `json:"wav_base64"`
				IntelligibilityWER *float64 `json:"intelligibility_wer,omitempty"`
			}
			if err := json.Unmarshal(call.Output, &out); err != nil {
				return report, err
			}
			wav, err := base64.StdEncoding.DecodeString(out.WAVBase64)
			if err != nil {
				return report, err
			}
			if _, err := ValidateWAV(wav); err != nil {
				return report, err
			}
			scores = append(scores, 100)
			if out.IntelligibilityWER != nil {
				if math.IsNaN(*out.IntelligibilityWER) || math.IsInf(*out.IntelligibilityWER, 0) || *out.IntelligibilityWER < 0 {
					return report, fmt.Errorf("invalid TTS intelligibility WER")
				}
				ttsRoundTripCases++
				report.Metrics["intelligibility_wer"] += *out.IntelligibilityWER
			}
		case LocalSuiteAcceleration:
			var out struct{ Off, On []PairedResult }
			if err := json.Unmarshal(call.Output, &out); err != nil {
				return report, err
			}
			paired, err := ScorePaired(out.Off, out.On)
			if err != nil {
				return report, err
			}
			scores = append(scores, 100*paired.PassRate)
			for _, result := range out.Off {
				if result.AnswerCorrect {
					report.Metrics["off_pass_rate"]++
				}
				report.Metrics["off_latency_ms"] += result.LatencyMS
				report.Metrics["off_tokens_per_second"] += result.TokensPerSecond
			}
			for _, result := range out.On {
				if result.AnswerCorrect {
					report.Metrics["on_pass_rate"]++
				}
				report.Metrics["on_latency_ms"] += result.LatencyMS
				report.Metrics["on_tokens_per_second"] += result.TokensPerSecond
			}
			report.Metrics["latency_ratio"] += paired.LatencyRatio
			report.Metrics["tokens_per_second_ratio"] += paired.TokensPerSecondRatio
			report.Status = "complete_paired"
			report.ScoreIdentity = "acceleration/exact-answer-pass-rate"
		case LocalSuiteEmbeddings:
			var in struct {
				Queries []RetrievalQuery `json:"queries"`
			}
			var out struct {
				Scores map[string]map[string]float64 `json:"scores"`
			}
			if err := json.Unmarshal(call.Input, &in); err != nil {
				return report, err
			}
			if err := json.Unmarshal(call.Output, &out); err != nil {
				return report, err
			}
			recall, mrr, ndcg, err := ScoreRetrieval(in.Queries, out.Scores)
			if err != nil {
				return report, err
			}
			scores = append(scores, 100*ndcg)
			report.Metrics["recall_at_1"] = recall
			report.Metrics["mrr_at_10"] = mrr
			report.Metrics["ndcg_at_10"] = ndcg
			report.ScoreIdentity = "embeddings/ndcg@10"
		default:
			return report, fmt.Errorf("unsupported suite %q", m.Identity.Suite)
		}
	}
	for _, score := range scores {
		report.Score += score
	}
	report.Score /= float64(len(scores))
	if m.Identity.Suite == LocalSuiteASR {
		for _, key := range []string{"wer", "cer", "latency_ms", "real_time_factor"} {
			report.Metrics[key] /= float64(len(scores))
		}
	}
	if m.Identity.Suite == LocalSuiteTools {
		report.Metrics["pass_percent"] = report.Score
	}
	if m.Identity.Suite == LocalSuiteAcceleration {
		for _, key := range []string{"off_pass_rate", "on_pass_rate", "off_latency_ms", "on_latency_ms", "off_tokens_per_second", "on_tokens_per_second", "latency_ratio", "tokens_per_second_ratio"} {
			report.Metrics[key] /= float64(len(scores))
		}
	}
	if m.Identity.Suite == LocalSuiteTTS {
		report.Metrics["technical_validity"] = 1
		if ttsRoundTripCases == 0 {
			report.ScoreIdentity = "tts/technical-validity-only"
		} else if ttsRoundTripCases != len(scores) {
			return report, fmt.Errorf("TTS round-trip intelligibility is incomplete: %d of %d cases", ttsRoundTripCases, len(scores))
		} else {
			report.Metrics["intelligibility_wer"] /= float64(ttsRoundTripCases)
			report.ScoreIdentity = "tts/technical-validity+roundtrip-intelligibility"
		}
	}
	if m.Identity.Limit != requiredLocalCaseCount(m.Identity.Suite) {
		report.Status = "partial"
		report.Errors = append(report.Errors, fmt.Sprintf("canary or incomplete suite: %d cases; coverage requires %d", m.Identity.Limit, requiredLocalCaseCount(m.Identity.Suite)))
	}
	return report, nil
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
