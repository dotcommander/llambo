package gateway

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/llambo/providers"
)

// handlerMockChatProvider implements providers.ChatProvider for handler testing
type handlerMockChatProvider struct {
	chatResult  providers.ChatResult
	chatErr     error
	lastCtx     context.Context
	lastSystem  string
	lastUser    string
	lastRequest providers.StructuredChatRequest
}

func (m *handlerMockChatProvider) Name() string   { return "mock" }
func (m *handlerMockChatProvider) MaxTokens() int { return 4096 }
func (m *handlerMockChatProvider) Shutdown()      {}
func (m *handlerMockChatProvider) GetOpenAIClients() *providers.OpenAIClients {
	return &providers.OpenAIClients{
		CircuitBreaker: providers.NewCircuitBreaker([]string{"mock"}, nil),
		CostTracker:    providers.NewCostTracker([]string{"mock"}),
	}
}
func (m *handlerMockChatProvider) Chat(_ context.Context, systemPrompt, userContent string) (string, error) {
	return m.chatResult.Content, m.chatErr
}
func (m *handlerMockChatProvider) ChatWithInfo(_ context.Context, systemPrompt, userContent string) (providers.ChatResult, error) {
	return m.chatResult, m.chatErr
}
func (m *handlerMockChatProvider) ChatWithInfoContext(ctx context.Context, systemPrompt, userContent string) (providers.ChatResult, error) {
	m.lastCtx = ctx
	m.lastSystem = systemPrompt
	m.lastUser = userContent
	return m.chatResult, m.chatErr
}

// handlerMockJobQueue implements providers.JobQueue for handler testing
// This is a simpler mock than the one in jobs_test.go, focused on handler testing
type handlerMockJobQueue struct {
	backends       []string
	circuitBreaker *providers.CircuitBreaker
}

func newHandlerMockJobQueue(backends []string) *handlerMockJobQueue {
	return &handlerMockJobQueue{
		backends:       backends,
		circuitBreaker: providers.NewCircuitBreaker(backends, nil),
	}
}

func (m *handlerMockJobQueue) Process(ctx context.Context, jobs []providers.Job) []providers.Result {
	results := make([]providers.Result, len(jobs))
	for i, j := range jobs {
		results[i] = providers.Result{
			ID:       j.ID,
			Content:  "response for " + j.ID,
			Backend:  "mock",
			Model:    "mock-model",
			Duration: 100 * time.Millisecond,
		}
	}
	return results
}

func (m *handlerMockJobQueue) ProcessStream(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
	for job := range jobs {
		results <- providers.Result{
			ID:       job.ID,
			Content:  "response for " + job.ID,
			Backend:  "mock",
			Model:    "mock-model",
			Duration: 100 * time.Millisecond,
		}
	}
}

func (m *handlerMockJobQueue) WorkerCount() int                             { return 2 }
func (m *handlerMockJobQueue) BackendNames() []string                       { return m.backends }
func (m *handlerMockJobQueue) Shutdown()                                    {}
func (m *handlerMockJobQueue) GetCircuitBreaker() *providers.CircuitBreaker { return m.circuitBreaker }

// handlerMockEmbeddingProvider implements providers.EmbeddingProvider for handler testing
type handlerMockEmbeddingProvider struct {
	embeddings [][]float32
	embedErr   error
	model      string
	dims       int
}

func (m *handlerMockEmbeddingProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if m.embedErr != nil {
		return nil, m.embedErr
	}
	if m.embeddings != nil {
		return m.embeddings, nil
	}
	// Generate dummy embeddings
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = make([]float32, m.dims)
		for j := range result[i] {
			result[i][j] = float32(i) * 0.1
		}
	}
	return result, nil
}

func (m *handlerMockEmbeddingProvider) Dimensions() int   { return m.dims }
func (m *handlerMockEmbeddingProvider) ModelName() string { return m.model }
func (m *handlerMockEmbeddingProvider) Close()            {}

// newHandlerTestServer creates a test server with mock providers for handler testing
func newHandlerTestServer(t *testing.T) (*Server, *handlerMockChatProvider, *handlerMockJobQueue) {
	t.Helper()

	mockChat := &handlerMockChatProvider{
		chatResult: providers.ChatResult{
			Content:  "Hello! How can I help you?",
			Provider: "mock-provider",
			Model:    "mock-model",
		},
	}

	mockQueue := newHandlerMockJobQueue([]string{"mock-backend"})

	server := &Server{
		provider:   mockChat,
		queue:      mockQueue,
		jobManager: NewJobManager(context.Background(), mockQueue),
		configs: map[string]providers.Config{
			"mock-backend": {Enabled: true, Model: "mock-model", Models: []string{"test", "requested-model", "test-model", "claude-3-5-sonnet", "gemini-2.5-pro", "gemini-2.5-flash", "text-embedding-test"}, Workers: 2},
		},
		startTime:       time.Now(),
		anthropicStrict: resolveAnthropicStrict(),
	}

	// Clean up job manager's cleanup goroutine after test
	t.Cleanup(func() {
		server.jobManager.StopCleanup()
	})

	return server, mockChat, mockQueue
}

// newHandlerTestServerWithEmbeddings creates a test server with embedding support
func newHandlerTestServerWithEmbeddings(t *testing.T) (*Server, *handlerMockEmbeddingProvider) {
	t.Helper()

	server, _, _ := newHandlerTestServer(t)
	mockEmbed := &handlerMockEmbeddingProvider{
		model: "text-embedding-test",
		dims:  1536,
	}
	server.embedder = mockEmbed

	return server, mockEmbed
}

func (m *handlerMockChatProvider) ChatStructuredWithInfoContext(ctx context.Context, req providers.StructuredChatRequest, target providers.ResolvedTarget) (providers.ChatResult, error) {
	m.lastRequest = req
	var system, user []string
	for _, message := range req.Messages {
		text, _ := message.GetContent().(string)
		if message.GetRole() == "system" {
			system = append(system, text)
		} else {
			user = append(user, text)
		}
	}
	return m.ChatWithInfoContext(ctx, strings.Join(system, "\n"), strings.Join(user, "\n"))
}
func (m *handlerMockEmbeddingProvider) EmbedResolved(ctx context.Context, texts []string, target providers.ResolvedTarget) (providers.EmbeddingResult, error) {
	vectors, err := m.Embed(ctx, texts)
	model := m.model
	if target.Model() != "" {
		model = target.Model()
	}
	return providers.EmbeddingResult{Vectors: vectors, Model: model}, err
}
