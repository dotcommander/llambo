package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
)

func TestEncodeEvalsReport(t *testing.T) {
	report := evals.Report{GeneratedAt: time.Unix(1, 0).UTC(), FormulaVersion: "test", RankingProfile: "overall", Models: []evals.ReportModel{
		{Name: "A"},
		{Name: "B"},
		{Name: "Local A", Projection: &evals.ProjectionInfo{SourceKey: "a", Confidence: "medium"}},
	}}
	markdown, err := encodeEvalsReport(report, "markdown", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdown), "| — | A |") || strings.Contains(string(markdown), "| — | B |") {
		t.Fatalf("unexpected markdown:\n%s", markdown)
	}
	jsonData, err := encodeEvalsReport(report, "json", 1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(jsonData), `"name": "B"`) {
		t.Fatalf("limit not applied: %s", jsonData)
	}
	if !strings.Contains(string(jsonData), `"name": "Local A"`) {
		t.Fatalf("tracked projection was hidden by limit: %s", jsonData)
	}
	if _, err := encodeEvalsReport(report, "html", 1); err == nil {
		t.Fatal("expected unsupported format error")
	}
}

func TestRunEvalsRejectsOfflineRefreshCombination(t *testing.T) {
	previousOffline, previousRefresh := evalsOffline, evalsRefresh
	evalsOffline, evalsRefresh = true, true
	t.Cleanup(func() { evalsOffline, evalsRefresh = previousOffline, previousRefresh })
	if err := runEvals(&commandIO{}, nil); err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalFetchOptionsAreOfflineByDefault(t *testing.T) {
	previousRefresh := evalsRefresh
	t.Cleanup(func() { evalsRefresh = previousRefresh })

	evalsRefresh = false
	defaultOptions := evalFetchOptions("/tmp/cache")
	if !defaultOptions.Offline || defaultOptions.Refresh {
		t.Fatalf("default eval mode may access the network: %#v", defaultOptions)
	}

	evalsRefresh = true
	refreshOptions := evalFetchOptions("/tmp/cache")
	if refreshOptions.Offline || !refreshOptions.Refresh {
		t.Fatalf("refresh mode did not enable source fetching: %#v", refreshOptions)
	}
}

func TestEvalsEligibilityDefaults(t *testing.T) {
	defaults := evalsCommand{MinOverall: 40, MaxOutputPrice: 10}
	if defaults.MinOverall != 40 || defaults.MaxOutputPrice != 10 {
		t.Fatalf("unexpected eligibility defaults: %#v", defaults)
	}
}

func TestEvalsOMLXDefaults(t *testing.T) {
	defaults := evalsCommand{OMLXURL: "http://127.0.0.1:8000"}
	if defaults.OMLXURL != "http://127.0.0.1:8000" || defaults.NoOMLX {
		t.Fatalf("unexpected OMLX defaults: %#v", defaults)
	}
}

func TestValidateOMLXURL(t *testing.T) {
	for _, value := range []string{"http://127.0.0.1:8000", "http://localhost:8000/v1", "https://[::1]:8000"} {
		if err := validateOMLXURL(value); err != nil {
			t.Errorf("valid URL %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{"https://example.com", "ftp://127.0.0.1/models", "http://127.0.0.1:8000/admin", "http://user@127.0.0.1:8000", "http://127.0.0.1:8000?x=1"} {
		if err := validateOMLXURL(value); err == nil {
			t.Errorf("unsafe URL %q accepted", value)
		}
	}
}

func TestRunEvalsRejectsInvalidMinimumOverall(t *testing.T) {
	previous := evalsMinOverall
	evalsMinOverall = 101
	t.Cleanup(func() { evalsMinOverall = previous })
	if err := runEvals(&commandIO{}, nil); err == nil || !strings.Contains(err.Error(), "between -1 and 100") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunEvalsRejectsInvalidMaximumOutputPrice(t *testing.T) {
	previous := evalsMaxOutputPrice
	evalsMaxOutputPrice = -2
	t.Cleanup(func() { evalsMaxOutputPrice = previous })
	if err := runEvals(&commandIO{}, nil); err == nil || !strings.Contains(err.Error(), "-1 or greater") {
		t.Fatalf("unexpected error: %v", err)
	}
}
