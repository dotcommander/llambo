package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/internal/evals"
	"github.com/dotcommander/llambo/providers"
)

func TestExecuteEvalsDefaultsToReport(t *testing.T) {
	var out, errOut bytes.Buffer
	err := execute(context.Background(), []string{"evals", "--min-score", "101"}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "--min-score must be between -1 and 100") {
		t.Fatalf("bare evals did not select the ranking report: %v", err)
	}
	if strings.Contains(out.String(), `"benchmarks"`) {
		t.Fatalf("bare evals unexpectedly rendered the writing catalog: %s", out.String())
	}
}

func TestEvalsHelpAndRemovedContract(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"evals", "--help"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	help := out.String()
	for _, want := range []string{"LLAMBO-7", `--rank-by="matrix"`, "--min-score", "--validation-receipts", "cache-only", "--live-omlx", "--refresh-official-model-cards"} {
		if !strings.Contains(help, want) {
			t.Fatalf("evals help missing %q:\n%s", want, help)
		}
	}
	for _, removed := range []string{"--min-overall", "ranking profile: overall", "ranking profile: general", "ranking profile: value", "llambo-score", "LLAMBO-4", "LLAMBO-5"} {
		if strings.Contains(help, removed) {
			t.Fatalf("evals help retained %q:\n%s", removed, help)
		}
	}
	if err := execute(context.Background(), []string{"evals", "--min-overall", "0"}, &out, &errOut); err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("removed --min-overall accepted: %v", err)
	}
	for _, profile := range []string{"overall", "general", "value", "llambo-score"} {
		if err := execute(context.Background(), []string{"evals", "--rank-by", profile, "--min-score", "0"}, &out, &errOut); err == nil || !strings.Contains(err.Error(), "requires a capability") {
			t.Fatalf("removed profile %q accepted: %v", profile, err)
		}
	}
}

func TestEvalsNonReportCommandsRejectOfficialCardRefreshBeforeWork(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-read.json")
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "writing catalog",
			args: []string{"evals", "writing", "--refresh-official-model-cards"},
		},
		{
			name: "writing run",
			args: []string{
				"evals", "writing", "run", "--refresh-official-model-cards",
				"--input", missing, "--benchmark", "writingbench", "--model", "omlx/model", "--judge-model", "omlx/judge",
				"--output-dir", filepath.Join(t.TempDir(), "run"), "--campaign-ledger", filepath.Join(t.TempDir(), "campaign.json"),
			},
		},
		{
			name: "local run",
			args: []string{
				"evals", "local", "run", "--refresh-official-model-cards",
				"--suite", "tools", "--input", missing, "--model", "omlx/model",
				"--output-dir", filepath.Join(t.TempDir(), "run"), "--campaign-ledger", filepath.Join(t.TempDir(), "campaign.json"),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			err := execute(context.Background(), test.args, &out, &errOut)
			if err == nil || !strings.Contains(err.Error(), "--refresh-official-model-cards is not supported") {
				t.Fatalf("official-card refresh was not rejected before command work: %v", err)
			}
		})
	}
}

func TestEncodeEvalsReport(t *testing.T) {
	report := evals.Report{GeneratedAt: time.Unix(1, 0).UTC(), FormulaVersion: "test", RankingProfile: "matrix", Models: []evals.ReportModel{
		{Name: "A", Scores: map[string]*evals.ExternalScore{"speed": {Score: 75}}},
		{Name: "B"},
		{Name: "Local A", Projection: &evals.ProjectionInfo{SourceKey: "a", Confidence: "medium"}},
	}}
	markdown, err := encodeEvalsReport(report, "markdown", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdown), "| A |") || strings.Contains(string(markdown), "| B |") {
		t.Fatalf("unexpected markdown:\n%s", markdown)
	}
	jsonData, err := encodeEvalsReport(report, "json", 1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(jsonData), `"name": "B"`) {
		t.Fatalf("limit not applied: %s", jsonData)
	}
	if !strings.Contains(string(jsonData), `"operational_scores"`) || !strings.Contains(string(jsonData), `"speed"`) {
		t.Fatalf("JSON omitted separate operational scores: %s", jsonData)
	}
	if !strings.Contains(string(jsonData), `"local_projections"`) || !strings.Contains(string(jsonData), `"name": "Local A"`) {
		t.Fatalf("tracked projection was not kept in its separate JSON section: %s", jsonData)
	}
	htmlData, err := encodeEvalsReport(report, "html", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(htmlData) == 0 || !strings.Contains(string(htmlData), "<") {
		t.Fatalf("unexpected HTML report: %s", htmlData)
	}
	if strings.Contains(string(htmlData), `data-value="b">B</td>`) || !strings.Contains(string(htmlData), "Local A") || !strings.Contains(string(htmlData), "data-sortable=\"true\"") {
		t.Fatalf("HTML limit or sortable markup was not preserved: %s", htmlData)
	}
	if _, err := encodeEvalsReport(report, "yaml", 1); err == nil || !strings.Contains(err.Error(), "supported: markdown, json, html") {
		t.Fatalf("unexpected unsupported format error: %v", err)
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

func TestRunEvalsRejectsOfficialCardRefreshConflicts(t *testing.T) {
	previousOffline, previousRefresh, previousCards := evalsOffline, evalsRefresh, evalsRefreshOfficialCards
	t.Cleanup(func() {
		evalsOffline, evalsRefresh, evalsRefreshOfficialCards = previousOffline, previousRefresh, previousCards
	})
	for _, values := range []struct{ offline, refresh bool }{{refresh: true}, {offline: true}} {
		evalsOffline, evalsRefresh, evalsRefreshOfficialCards = values.offline, values.refresh, true
		if err := runEvals(&commandIO{}, nil); err == nil || !strings.Contains(err.Error(), "refresh-official-model-cards") {
			t.Fatalf("official-card refresh conflict was accepted: %#v: %v", values, err)
		}
	}
}

func TestApplyLiveOMLXDiscoveryIsExplicitAndSelectsLiveInventory(t *testing.T) {
	previousOffline, previousNoOMLX, previousURL, previousClient := evalsOffline, evalsNoOMLX, evalsOMLXURL, omlxDiscoveryHTTPClient
	t.Cleanup(func() {
		evalsOffline, evalsNoOMLX, evalsOMLXURL, omlxDiscoveryHTTPClient = previousOffline, previousNoOMLX, previousURL, previousClient
	})
	var requests atomic.Int64
	omlxDiscoveryHTTPClient = &http.Client{Transport: commandRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		if request.URL.Path != "/admin/api/models" {
			return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("not found"))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"models":[{"id":"live","model_type":"llm"}]}`))}, nil
	})}
	evalsOffline, evalsNoOMLX, evalsOMLXURL = false, false, "http://127.0.0.1:8000"
	report := evals.Report{Models: []evals.ReportModel{
		{Key: "canonical"},
		{Key: "live", Projection: &evals.ProjectionInfo{SourceKey: "source"}},
		{Key: "inactive", Projection: &evals.ProjectionInfo{SourceKey: "source"}},
	}}
	discovery, live, err := applyLiveOMLXDiscovery(&commandIO{ctx: context.Background()}, &report)
	if err != nil || !live || requests.Load() != 1 || strings.Join(discovery.Models, ",") != "live" || len(report.Models) != 2 || report.Models[1].Key != "live" {
		t.Fatalf("live OMLX discovery did not make one selecting request: requests=%d discovery=%#v report=%#v", requests.Load(), discovery, report.Models)
	}
	ids, endpoint := selectedOMLXInventory(nil, nil, discovery, true)
	if strings.Join(ids, ",") != "live" || !strings.HasSuffix(endpoint, "/admin/api/models") {
		t.Fatalf("live discovery was not forwarded to snapshot selection: ids=%#v endpoint=%q", ids, endpoint)
	}

	evalsNoOMLX = true
	before := requests.Load()
	untouched := evals.Report{Models: []evals.ReportModel{{Key: "inactive", Projection: &evals.ProjectionInfo{SourceKey: "source"}}}}
	if _, enabled, err := applyLiveOMLXDiscovery(&commandIO{ctx: context.Background()}, &untouched); err != nil || enabled || requests.Load() != before || len(untouched.Models) != 1 {
		t.Fatalf("ordinary or disabled run unexpectedly discovered OMLX: enabled=%t requests=%d report=%#v", enabled, requests.Load(), untouched.Models)
	}
}

func TestRunEvalsStopsBeforePreparationWhenExplicitLiveDiscoveryFails(t *testing.T) {
	previousRefresh, previousOffline, previousNoOMLX, previousRank := evalsRefresh, evalsOffline, evalsNoOMLX, evalsRankBy
	previousMin, previousMax, previousFormat, previousFetch, previousPrepare, previousClient := evalsMinScore, evalsMaxOutputPrice, evalsFormat, fetchEvalsReport, prepareEvalsOMLXScores, omlxDiscoveryHTTPClient
	t.Cleanup(func() {
		evalsRefresh, evalsOffline, evalsNoOMLX, evalsRankBy = previousRefresh, previousOffline, previousNoOMLX, previousRank
		evalsMinScore, evalsMaxOutputPrice, evalsFormat, fetchEvalsReport, prepareEvalsOMLXScores, omlxDiscoveryHTTPClient = previousMin, previousMax, previousFormat, previousFetch, previousPrepare, previousClient
	})
	snapshotPath := filepath.Join(t.TempDir(), "prior-snapshot.json")
	if err := os.WriteFile(snapshotPath, []byte("prior snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	fetchEvalsReport = func(context.Context, evals.Options) (evals.Result, error) { return evals.Result{}, nil }
	prepareEvalsOMLXScores = func(*evals.Report, evals.OMLXDiscovery, bool) (pendingOMLXScores, error) {
		if err := os.WriteFile(snapshotPath, []byte("replaced snapshot"), 0o600); err != nil {
			return pendingOMLXScores{}, err
		}
		return pendingOMLXScores{}, nil
	}
	omlxDiscoveryHTTPClient = &http.Client{Transport: commandRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("unavailable")
	})}
	evalsRefresh, evalsOffline, evalsNoOMLX, evalsRankBy, evalsMinScore, evalsMaxOutputPrice, evalsFormat = false, false, false, "matrix", -1, -1, "json"
	err := runEvals(&commandIO{ctx: context.Background(), stdout: io.Discard}, nil)
	if err == nil || !strings.Contains(err.Error(), "discover live OMLX inventory") {
		t.Fatalf("live discovery failure did not terminate command: %v", err)
	}
	after, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != "prior snapshot" {
		t.Fatalf("discovery failure prepared or published a replacement snapshot: %q", after)
	}
}

func TestRunEvalsStopsBeforePreparationWhenReportConstructionFails(t *testing.T) {
	previousRank, previousMin, previousMax, previousFetch, previousPrepare, previousCacheDir := evalsRankBy, evalsMinScore, evalsMaxOutputPrice, fetchEvalsReport, prepareEvalsOMLXScores, evalsCacheDir
	t.Cleanup(func() {
		evalsRankBy, evalsMinScore, evalsMaxOutputPrice, fetchEvalsReport, prepareEvalsOMLXScores, evalsCacheDir = previousRank, previousMin, previousMax, previousFetch, previousPrepare, previousCacheDir
	})
	fetchEvalsReport = func(context.Context, evals.Options) (evals.Result, error) { return evals.Result{}, nil }
	prepared := false
	prepareEvalsOMLXScores = func(*evals.Report, evals.OMLXDiscovery, bool) (pendingOMLXScores, error) {
		prepared = true
		return pendingOMLXScores{}, nil
	}
	evalsRankBy, evalsMinScore, evalsMaxOutputPrice, evalsCacheDir = "not-a-ranking-profile", -1, -1, t.TempDir()

	err := runEvals(&commandIO{ctx: context.Background(), stdout: io.Discard}, nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported ranking profile") {
		t.Fatalf("report construction failure did not terminate command: %v", err)
	}
	if prepared {
		t.Fatal("report construction failure prepared a replacement snapshot")
	}
}

type commandRoundTripFunc func(*http.Request) (*http.Response, error)

func (f commandRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestWriteEvalsThenPublishPreservesPriorSnapshotWhenOutputFails(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "llambo-scores.json")
	prior := evals.OMLXScoreSnapshot{Inventory: []string{"prior"}, FormulaVersion: evals.CategoryFormulaVersion}
	if err := evals.SaveOMLXScoreSnapshot(snapshotPath, prior); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	pending := pendingOMLXScores{path: snapshotPath, snapshot: evals.OMLXScoreSnapshot{Inventory: []string{"replacement"}, FormulaVersion: evals.CategoryFormulaVersion}}
	if err := writeEvalsThenPublish(&bytes.Buffer{}, filepath.Join(blocked, "report.md"), []byte("report"), pending); err == nil {
		t.Fatal("output failure unexpectedly published score snapshot")
	}
	loaded, err := evals.LoadOMLXScoreSnapshot(snapshotPath)
	if err != nil || loaded == nil || strings.Join(loaded.Inventory, ",") != "prior" || loaded.FormulaVersion != evals.CategoryFormulaVersion {
		t.Fatalf("prior snapshot was replaced after failed output: %#v %v", loaded, err)
	}
}

func TestEvalFetchOptionsAreOfflineByDefault(t *testing.T) {
	previousRefresh, previousCards := evalsRefresh, evalsRefreshOfficialCards
	t.Cleanup(func() { evalsRefresh, evalsRefreshOfficialCards = previousRefresh, previousCards })

	evalsRefresh, evalsRefreshOfficialCards = false, false
	defaultOptions := evalFetchOptions("/tmp/cache")
	if !defaultOptions.Offline || defaultOptions.Refresh || defaultOptions.RefreshOfficialCards {
		t.Fatalf("default eval mode may access the network: %#v", defaultOptions)
	}

	evalsRefresh = true
	refreshOptions := evalFetchOptions("/tmp/cache")
	if refreshOptions.Offline || !refreshOptions.Refresh {
		t.Fatalf("refresh mode did not enable source fetching: %#v", refreshOptions)
	}

	evalsRefresh, evalsRefreshOfficialCards = false, true
	cardOptions := evalFetchOptions("/tmp/cache")
	if cardOptions.Offline || cardOptions.Refresh || !cardOptions.RefreshOfficialCards {
		t.Fatalf("official-card refresh did not isolate source scope: %#v", cardOptions)
	}
}

func TestEvalsEligibilityDefaults(t *testing.T) {
	defaults := evalsCommand{MinScore: -1, MaxOutputPrice: 10}
	if defaults.MinScore != -1 || defaults.MaxOutputPrice != 10 {
		t.Fatalf("unexpected eligibility defaults: %#v", defaults)
	}
}

func TestEvalsOMLXDefaults(t *testing.T) {
	defaults := evalsCommand{OMLXURL: "http://127.0.0.1:8000"}
	if defaults.OMLXURL != "http://127.0.0.1:8000" || defaults.NoOMLX {
		t.Fatalf("unexpected OMLX defaults: %#v", defaults)
	}
}

func TestOMLXInventoryUsesLiveCaptureOrSealedSnapshotNotHistoricalCatalog(t *testing.T) {
	cat := &catalog.Catalog{Providers: map[string]*catalog.ProviderCatalog{
		"omlx": {Models: map[string]*catalog.ModelEntry{
			"historical":     {},
			"live-unmatched": {LastPing: catalog.PingState{Success: true, SpeedTokensPerSecond: 10, CheckedAt: time.Unix(1, 0)}},
		}},
	}}
	cfg := &providers.GlobalConfig{Providers: map[string]providers.Config{"omlx": {Model: "configured-current"}}}
	discovery := evals.OMLXDiscovery{Endpoint: "http://127.0.0.1:8000/admin/api/models", Models: []string{"live-unmatched"}}
	previous := &evals.OMLXScoreSnapshot{Inventory: []string{"sealed-current"}}
	liveIDs, endpoint := selectedOMLXInventory(cfg, previous, discovery, true)
	if strings.Join(liveIDs, ",") != "live-unmatched" || endpoint != discovery.Endpoint {
		t.Fatalf("live inventory leaked stale/configured IDs: %#v %q", liveIDs, endpoint)
	}
	offlineIDs, offlineEndpoint := selectedOMLXInventory(cfg, previous, evals.OMLXDiscovery{}, false)
	if strings.Join(offlineIDs, ",") != "configured-current,sealed-current" || offlineEndpoint != "" {
		t.Fatalf("offline inventory did not use sealed/configured IDs: %#v %q", offlineIDs, offlineEndpoint)
	}
	inputs := omlxScoreInputs(&evals.Report{}, cat, map[string]costs.ModelCost{}, liveIDs)
	if len(inputs) != 1 || inputs[0].Key != "live-unmatched" {
		t.Fatalf("unmatched live ID was not scored or stale catalog leaked: %#v", inputs)
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

func TestRunEvalsRejectsInvalidMinimumScore(t *testing.T) {
	previous, previousRank := evalsMinScore, evalsRankBy
	evalsMinScore, evalsRankBy = 101, "coding"
	t.Cleanup(func() { evalsMinScore, evalsRankBy = previous, previousRank })
	if err := runEvals(&commandIO{}, nil); err == nil || !strings.Contains(err.Error(), "between -1 and 100") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunEvalsRejectsInvalidMaximumOutputPrice(t *testing.T) {
	previous, previousScore := evalsMaxOutputPrice, evalsMinScore
	evalsMaxOutputPrice, evalsMinScore = -2, -1
	t.Cleanup(func() { evalsMaxOutputPrice, evalsMinScore = previous, previousScore })
	if err := runEvals(&commandIO{}, nil); err == nil || !strings.Contains(err.Error(), "-1 or greater") {
		t.Fatalf("unexpected error: %v", err)
	}
}
