package cmd

import "strings"

func normalizeCSVTokens(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(strings.ToLower(p))
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}
