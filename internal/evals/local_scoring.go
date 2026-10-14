package evals

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
)

// ToolCall is one function invocation observed in a tool-use response.
type ToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ASRCase is one speech-recognition fixture: reference transcript plus the
// sealed WAV input and its digest.
type ASRCase struct {
	ID         string `json:"id"`
	Transcript string `json:"transcript"`
	WAVPath    string `json:"wav_path"`
	WAVSHA256  string `json:"wav_sha256"`
}

// TTSCase is one text-to-speech generation prompt.
type TTSCase struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
}

// EmbeddingCase is one retrieval fixture holding documents and queries.
type EmbeddingCase struct {
	ID        string           `json:"id"`
	Documents []EmbeddingText  `json:"documents"`
	Queries   []EmbeddingQuery `json:"queries"`
}

// EmbeddingText is one indexed document in an embedding case.
type EmbeddingText struct{ ID, Text string }

// EmbeddingQuery is one query with its relevant document IDs.
type EmbeddingQuery struct {
	ID, Text string
	Relevant []string `json:"relevant"`
}

// AccelerationCase is one speculative-decoding probe with its oracle answer.
type AccelerationCase struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
	Oracle string `json:"oracle"`
}

// ToolCase is one tool-use evaluation case with optional expected call.
type ToolCase struct {
	ID       string           `json:"id"`
	Prompt   string           `json:"prompt"`
	Tools    []ToolDefinition `json:"tools"`
	Expected *ToolCall        `json:"expected,omitempty"`
	Calls    []ToolCall       `json:"calls"`
}

// ScoreToolCase reports whether the observed tool calls satisfy the case
// expectation.
func ScoreToolCase(c ToolCase) (bool, error) {
	if c.ID == "" {
		return false, errors.New("tool case id is required")
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

// ScoreTools returns the pass fraction across tool-use cases.
func ScoreTools(cases []ToolCase) (float64, error) {
	if len(cases) == 0 {
		return 0, errors.New("no tool cases")
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

// NormalizeTranscript canonicalizes a transcript for error-rate comparison.
func NormalizeTranscript(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSpace(r) }), " ")
}

// WordErrorRate computes WER as edit distance over reference word count.
func WordErrorRate(reference, hypothesis string) float64 {
	r := strings.Fields(NormalizeTranscript(reference))
	h := strings.Fields(NormalizeTranscript(hypothesis))
	return float64(editDistance(r, h)) / float64(max(1, len(r)))
}

// CharErrorRate computes CER as edit distance over reference rune count.
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

// PairedResult is one observed answer in an acceleration A/B pair.
type PairedResult struct {
	AnswerCorrect   bool    `json:"answer_correct"`
	LatencyMS       float64 `json:"latency_ms"`
	TokensPerSecond float64 `json:"tokens_per_second"`
}

// PairedScore compares off/on acceleration arms as pass-rate and speed ratios.
type PairedScore struct {
	PassRate             float64 `json:"pass_rate"`
	LatencyRatio         float64 `json:"latency_ratio"`
	TokensPerSecondRatio float64 `json:"tokens_per_second_ratio"`
}

// ScorePaired computes paired acceleration deltas between MTP-off and MTP-on
// arms.
func ScorePaired(off, on []PairedResult) (PairedScore, error) {
	if len(off) == 0 || len(off) != len(on) {
		return PairedScore{}, errors.New("paired results must be nonempty and equal length")
	}
	var p, lat, tps float64
	for i := range off {
		if off[i].LatencyMS <= 0 || on[i].LatencyMS <= 0 || off[i].TokensPerSecond <= 0 || on[i].TokensPerSecond <= 0 {
			return PairedScore{}, errors.New("paired timing metrics must be positive")
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
