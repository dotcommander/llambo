package cmd

import "time"

// invocationOptions belongs to one parsed command and its execution helpers.
// Options are never shared between CLI invocations or mutated package-wide.
type invocationOptions struct {
	pingPrompt                    string
	pingOutput                    string
	pingProviders                 string
	pingModels                    string
	pingIncludeQuarantine         bool
	pingFreeOnly                  bool
	pingIncludeUnknownCost        bool
	pingTimeout                   int
	pingMaxOutputCost             float64
	pingRecordMetrics             bool
	systemPrompt                  string
	outputFile                    string
	timeoutSeconds                int
	promptModels                  string
	promptProviders               string
	promptIncludeQuarantine       bool
	promptMaxOutputCost           float64
	promptFreeOnly                bool
	promptIncludeUnknownCost      bool
	promptSmart                   bool
	promptFuse                    bool
	promptFuseModels              string
	promptFuseControl             bool
	routeEventsPath               string
	routeModes                    string
	routeLimit                    int
	routeReportPath               string
	canaryStartProvider           string
	canaryStartTrafficPct         float64
	canaryStartPromoteAfter       int
	canaryStartBaseline           string
	servePort                     string
	serveHost                     string
	serveAllowRemote              bool
	evalsRefresh                  bool
	evalsRefreshOfficialCards     bool
	evalsFormat                   string
	evalsOutput                   string
	evalsLimit                    int
	evalsPartial                  bool
	evalsRankBy                   string
	evalsOffline                  bool
	evalsProjections              string
	evalsValidationReceipts       string
	evalsMinScore                 float64
	evalsMaxOutputPrice           float64
	evalsOMLXURL                  string
	evalsNoOMLX                   bool
	evalsCacheDir                 string
	runCount                      int
	runPrompt                     string
	runSystem                     string
	runServer                     string
	runWait                       bool
	runPoll                       time.Duration
	runTimeout                    time.Duration
	runVerify                     bool
	stressJobs                    int
	stressRequestsPerJob          int
	stressPrompt                  string
	stressServer                  string
	modelsGrouped                 bool
	modelsAll                     bool
	modelsCSV                     bool
	modelsAvailable               bool
	modelsTimeoutSec              int
	modelsProviderFilter          string
	modelsMetrics                 bool
	catalogFilterPinned           bool
	catalogFilterAvoid            bool
	catalogFilterNew              bool
	catalogFilterFree             bool
	catalogFilterTag              string
	catalogShowMetadata           bool
	catalogOutputJSON             bool
	avoidReason                   string
	discoverFreePin               bool
	discoverFreeIncludeQuarantine bool
	discoverFreeProviders         string
	discoverFreeTimeout           int
	modelsSyncPricingDryRun       bool
	modelsSyncPricingProviders    []string
}

func defaultInvocationOptions() *invocationOptions {
	return &invocationOptions{modelsGrouped: true}
}
