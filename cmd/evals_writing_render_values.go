package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
)

func writingCatalogLink(label, url string) string {
	return fmt.Sprintf("[%s](%s)", writingCatalogCell(label), url)
}

func writingCatalogCell(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.ReplaceAll(value, "|", "\\|")
}

func writingCatalogCount(value int) string {
	if value == 0 {
		return "—"
	}
	return strconv.Itoa(value)
}

func writingCatalogNumber(value int) string {
	if value == 0 {
		return "—"
	}
	return strconv.Itoa(value)
}

func writingCatalogFloat(value *float64) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f", *value)
}

func writingCatalogPercent(value *float64) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", *value)
}

func writingCatalogLicenseMatch(check evals.WritingModelStatus) string {
	if check.RemoteLicense == "" {
		return "—"
	}
	if check.LicenseMatch {
		return yes1
	}
	return no1
}

func writingCatalogTime(value time.Time) string {
	if value.IsZero() {
		return "unknown"
	}
	return value.UTC().Format(time.RFC3339)
}
