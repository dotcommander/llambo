package providers

import (
	"strings"
)

// FormatErrorShort extracts a concise error for inline display.
func FormatErrorShort(err error) string {
	if err == nil {
		return "unknown error"
	}
	msg := err.Error()
	// Extract part after provider prefix
	if idx := strings.LastIndex(msg, ": "); idx != -1 && idx < len(msg)-2 {
		msg = msg[idx+2:]
	}
	if len(msg) > 60 {
		msg = msg[:57] + "..."
	}
	return msg
}
