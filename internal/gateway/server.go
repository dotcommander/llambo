package gateway

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dotcommander/llambo/providers"
)

// Server is the gateway HTTP server
type Server struct {
	provider           providers.ChatProvider      // chat provider with failover support
	queue              providers.JobQueue          // parallel job processing queue
	embedder           providers.EmbeddingProvider // embedding provider
	jobManager         *JobManager
	maxRequestsPerJob  int
	configs            map[string]providers.Config
	httpServer         *http.Server
	startTime          time.Time
	anthropicStrict    bool
	authTokenHash      [sha256.Size]byte
	authRequired       bool
	allowedOrigins     map[string]struct{}
	closeResourcesOnce sync.Once
}

// resolveAnthropicStrict reads LLAMBO_ANTHROPIC_STRICT env var once at startup.
func resolveAnthropicStrict() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("LLAMBO_ANTHROPIC_STRICT")))
	switch v {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// New creates a new gateway server. The provided ctx bounds the lifetime of
// the job-manager cleanup goroutine; canceling it triggers a clean shutdown.
func New(ctx context.Context, configs map[string]providers.Config, routing providers.RoutingConfig, gatewayCfg providers.GatewayConfig) (*Server, error) {
	authToken, err := gatewayCfg.ResolveAuthToken()
	if err != nil {
		return nil, err
	}
	allowedOrigins, err := normalizeAllowedOrigins(gatewayCfg.AllowedOrigins)
	if err != nil {
		return nil, err
	}

	// Create OpenAIProvider for single requests with failover
	// This creates the shared OpenAIClients instance
	provider, err := providers.NewOpenAIWithRoutingCallbacks(configs, routing, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("init openai provider: %w", err)
	}

	// Create BackendQueue using shared OpenAIClients from provider
	// This ensures both use the same circuit breaker state
	queue := providers.NewBackendQueue(provider.GetOpenAIClients(), configs)

	// Create embedding provider (optional - don't fail if not configured)
	embedder, _ := providers.NewOpenAIEmbedding()

	gatewayCfg.ApplyDefaults()

	jobManager := NewJobManager(ctx, queue)
	jobManager.SetMaxActiveJobs(gatewayCfg.MaxActiveJobs)

	server := &Server{
		provider:          provider,
		queue:             queue,
		embedder:          embedder,
		jobManager:        jobManager,
		maxRequestsPerJob: gatewayCfg.MaxRequestsPerJob,
		configs:           configs,
		startTime:         time.Now(),
		anthropicStrict:   resolveAnthropicStrict(),
		allowedOrigins:    allowedOrigins,
	}
	if authToken != "" {
		server.authRequired = true
		server.authTokenHash = sha256.Sum256([]byte(authToken))
	}
	return server, nil
}

func normalizeAllowedOrigins(origins []string) (map[string]struct{}, error) {
	allowed := make(map[string]struct{}, len(origins))
	for _, configured := range origins {
		origin := strings.TrimSpace(configured)
		parsed, err := url.Parse(origin)
		if err != nil || origin == "*" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
			parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("invalid gateway allowed origin %q", configured)
		}
		allowed[origin] = struct{}{}
	}
	return allowed, nil
}

// Start starts the HTTP server
func (s *Server) Start(addr string) error {
	mux := http.NewServeMux()

	// OpenAI-compatible endpoints
	mux.HandleFunc("POST /v1/chat/completions", s.handleChatCompletion)
	mux.HandleFunc("POST /v1/messages", s.handleAnthropicMessages)
	mux.HandleFunc("POST /v1/embeddings", s.handleEmbeddings)
	mux.HandleFunc("GET /v1/models", s.handleListModels)

	// Job queue endpoints
	mux.HandleFunc("POST /v1/jobs", s.handleCreateJob)
	mux.HandleFunc("GET /v1/jobs/{id}", s.handleGetJob)
	mux.HandleFunc("POST /v1/jobs/{id}/cancel", s.handleCancelJob)

	// Status endpoints
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /providers", s.handleProviders)
	mux.HandleFunc("GET /stats", s.handleStats)

	// Root endpoint
	mux.HandleFunc("GET /", s.handleRoot)

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      s.middleware(mux),
		ReadTimeout:  ServerReadTimeout,
		WriteTimeout: ServerWriteTimeout,
		IdleTimeout:  ServerIdleTimeout,
	}

	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.jobManager.BeginShutdown()

	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			return err
		}
	}

	if err := s.jobManager.WaitForDrain(ctx); err != nil {
		return err
	}

	// Close shared execution resources only after HTTP handlers and admitted
	// jobs have both drained. A deadline above returns without closing them so
	// callers can retry Shutdown with a fresh context.
	s.closeResourcesOnce.Do(func() {
		s.queue.Shutdown()
		s.provider.Shutdown()
		if s.embedder != nil {
			s.embedder.Close()
		}
	})
	return nil
}

// BackendNames returns the list of configured backend names
func (s *Server) BackendNames() []string {
	return s.queue.BackendNames()
}

// middleware wraps handlers with origin admission, bearer auth, and logging.
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		if !s.allowOrigin(w, r) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodOptions {
			if !allowedPreflight(r) {
				http.Error(w, "preflight not allowed", http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="llambo"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Wrap response writer to capture status
		wrapped := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		// Log request
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "status", wrapped.status, "duration", time.Since(start).Round(time.Millisecond))
	})
}

func allowedPreflight(r *http.Request) bool {
	method := r.Header.Get("Access-Control-Request-Method")
	if method != http.MethodGet && method != http.MethodPost {
		return false
	}
	for header := range strings.SplitSeq(r.Header.Get("Access-Control-Request-Headers"), ",") {
		header = strings.TrimSpace(header)
		if header != "" && !strings.EqualFold(header, "Content-Type") && !strings.EqualFold(header, "Authorization") {
			return false
		}
	}
	return true
}

func (s *Server) allowOrigin(w http.ResponseWriter, r *http.Request) bool {
	if len(s.allowedOrigins) > 0 {
		w.Header().Add("Vary", "Origin")
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Method != http.MethodOptions
	}
	if _, ok := s.allowedOrigins[origin]; !ok {
		return false
	}

	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	return true
}

func (s *Server) authorized(r *http.Request) bool {
	if !s.authRequired {
		return true
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}
	candidateHash := sha256.Sum256([]byte(parts[1]))
	return subtle.ConstantTimeCompare(candidateHash[:], s.authTokenHash[:]) == 1
}

// statusRecorder wraps ResponseWriter to capture status code
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// handleRoot serves a simple status page
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "Llambo Gateway\n")
	fmt.Fprintf(w, "==============\n\n")
	fmt.Fprintf(w, "Endpoints:\n")
	fmt.Fprintf(w, "  POST /v1/chat/completions  - Chat completion\n")
	fmt.Fprintf(w, "  POST /v1/messages          - Anthropic-compatible messages\n")
	fmt.Fprintf(w, "  POST /v1/embeddings        - Generate embeddings\n")
	fmt.Fprintf(w, "  GET  /v1/models            - List models\n")
	fmt.Fprintf(w, "  POST /v1/jobs              - Create batch job\n")
	fmt.Fprintf(w, "  GET  /v1/jobs/{id}         - Get job status\n")
	fmt.Fprintf(w, "  POST /v1/jobs/{id}/cancel  - Cancel job\n")
	fmt.Fprintf(w, "  GET  /health               - Health check\n")
	fmt.Fprintf(w, "  GET  /providers            - List providers\n")
	fmt.Fprintf(w, "  GET  /stats                - Usage and cost stats\n")
	fmt.Fprintf(w, "\nBackends: %v\n", s.BackendNames())
	fmt.Fprintf(w, "Uptime: %s\n", time.Since(s.startTime).Round(time.Second))
}
