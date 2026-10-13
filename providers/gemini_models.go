package providers

import (
	"regexp"
	"strconv"
	"strings"
)

var geminiMajorVersionPattern = regexp.MustCompile(`^gemini-(\d+)`)

// isGemini3SeriesModel reports whether model is a Gemini 3-or-later release.
// Those models reject temperature and control reasoning through
// thinkingConfig.thinkingLevel.
// Source: https://aistudio.google.com/docs/thinking#thinking-levels
func isGemini3SeriesModel(model string) bool {
	base := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(model)), "models/")
	match := geminiMajorVersionPattern.FindStringSubmatch(base)
	if match == nil {
		return false
	}
	major, err := strconv.Atoi(match[1])
	return err == nil && major >= 3
}

// rejectsGeminiTemperature reports whether cfg targets a Gemini 3-or-later
// model through the native Gemini provider. Those models reject temperature,
// so the request must leave it unset instead of erroring.
func rejectsGeminiTemperature(cfg Config) bool {
	return cfg.GetProviderType() == "gemini" && isGemini3SeriesModel(cfg.Model)
}
