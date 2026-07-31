package providers

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var retryAfterHeaderPattern = regexp.MustCompile(`(?i)\b(?:retry-after|x-ratelimit-reset-requests)\s*[:=]\s*([^\n\r;]+)`)

// ExtractRetryAfter returns a provider-requested cooldown from retry/reset
// metadata embedded in an error message. Adapters that can read headers should
// prefer NewOpenAIErrorWithRetryAfter and pass the parsed value directly.
func ExtractRetryAfter(message string, now time.Time) time.Duration {
	for _, match := range retryAfterHeaderPattern.FindAllStringSubmatch(message, -1) {
		if len(match) < 2 {
			continue
		}
		if d := parseRetryAfterValue(match[1], now); d > 0 {
			return d
		}
	}
	return 0
}

func parseRetryAfterValue(raw string, now time.Time) time.Duration {
	value := strings.Trim(raw, ` "'`)
	if value == "" {
		return 0
	}

	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds > 0 {
		return time.Duration(seconds * float64(time.Second))
	}

	if d := parseCompactDuration(value); d > 0 {
		return d
	}

	if t, err := http.ParseTime(value); err == nil {
		if now.IsZero() {
			now = time.Now()
		}
		if d := t.Sub(now); d > 0 {
			return d
		}
	}

	return 0
}

func parseCompactDuration(value string) time.Duration {
	var total time.Duration
	var matched bool
	for _, match := range regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(ms|s|m|h)`).FindAllStringSubmatch(value, -1) {
		if len(match) < 3 {
			continue
		}
		n, err := strconv.ParseFloat(match[1], 64)
		if err != nil || n <= 0 {
			continue
		}
		matched = true
		switch strings.ToLower(match[2]) {
		case "ms":
			total += time.Duration(n * float64(time.Millisecond))
		case "s":
			total += time.Duration(n * float64(time.Second))
		case "m":
			total += time.Duration(n * float64(time.Minute))
		case "h":
			total += time.Duration(n * float64(time.Hour))
		}
	}
	if !matched {
		return 0
	}
	return total
}
