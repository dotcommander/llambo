package gateway

import (
	"context"
	"net"
	"net/http"
	"sync"
	"testing"

	"github.com/dotcommander/llambo/providers"
)

type shutdownCountingProvider struct {
	*handlerMockChatProvider
	mu        sync.Mutex
	shutdowns int
}

func (p *shutdownCountingProvider) Shutdown() {
	p.mu.Lock()
	p.shutdowns++
	p.mu.Unlock()
}

func (p *shutdownCountingProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.shutdowns
}

type shutdownCountingQueue struct {
	*flexibleJobQueue
	mu        sync.Mutex
	shutdowns int
}

func (q *shutdownCountingQueue) Shutdown() {
	q.mu.Lock()
	q.shutdowns++
	q.mu.Unlock()
}

func (q *shutdownCountingQueue) count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.shutdowns
}

type shutdownCountingEmbedder struct {
	*handlerMockEmbeddingProvider
	mu     sync.Mutex
	closes int
}

func (e *shutdownCountingEmbedder) Close() {
	e.mu.Lock()
	e.closes++
	e.mu.Unlock()
}

func (e *shutdownCountingEmbedder) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closes
}

func TestServer_ShutdownWaitsForHTTPAndJobDrainBeforeClosingResources(t *testing.T) {
	streamStarted := make(chan struct{})
	releaseStream := make(chan struct{})
	queue := &shutdownCountingQueue{flexibleJobQueue: &flexibleJobQueue{
		workerCount: 1,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			close(streamStarted)
			<-ctx.Done()
			<-releaseStream
		},
	}}
	provider := &shutdownCountingProvider{handlerMockChatProvider: &handlerMockChatProvider{}}
	embedder := &shutdownCountingEmbedder{handlerMockEmbeddingProvider: &handlerMockEmbeddingProvider{}}
	manager := NewJobManager(context.Background(), queue)
	t.Cleanup(manager.StopCleanup)
	server := &Server{provider: provider, queue: queue, embedder: embedder, jobManager: manager}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	server.httpServer = &http.Server{Handler: http.NewServeMux()}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.httpServer.Serve(listener) }()

	requests := []JobRequest{{ID: "req", Messages: []Message{{Role: "user", Content: "hello"}}}}
	if _, err := manager.CreateJobIfCapacity(t.Context(), requests, "system"); err != nil {
		t.Fatalf("CreateJobIfCapacity() error = %v", err)
	}
	<-streamStarted

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- server.Shutdown(context.Background()) }()
	if err := <-serveDone; err != http.ErrServerClosed {
		t.Fatalf("Serve() error = %v, want http.ErrServerClosed", err)
	}
	if queue.count() != 0 || provider.count() != 0 || embedder.count() != 0 {
		t.Fatalf("resources closed before job drain: queue=%d provider=%d embedder=%d", queue.count(), provider.count(), embedder.count())
	}

	close(releaseStream)
	if err := <-shutdownDone; err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if queue.count() != 1 || provider.count() != 1 || embedder.count() != 1 {
		t.Fatalf("resource close counts = queue=%d provider=%d embedder=%d, want 1 each", queue.count(), provider.count(), embedder.count())
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
	if queue.count() != 1 || provider.count() != 1 || embedder.count() != 1 {
		t.Fatalf("resource close repeated: queue=%d provider=%d embedder=%d", queue.count(), provider.count(), embedder.count())
	}
}

func TestServer_ShutdownDeadlineLeavesResourcesOpenForRetry(t *testing.T) {
	streamStarted := make(chan struct{})
	releaseStream := make(chan struct{})
	queue := &shutdownCountingQueue{flexibleJobQueue: &flexibleJobQueue{
		workerCount: 1,
		processStreamFunc: func(ctx context.Context, jobs <-chan providers.Job, results chan<- providers.Result, maxWorkers int, started chan<- providers.JobStarted) {
			close(streamStarted)
			<-ctx.Done()
			<-releaseStream
		},
	}}
	provider := &shutdownCountingProvider{handlerMockChatProvider: &handlerMockChatProvider{}}
	embedder := &shutdownCountingEmbedder{handlerMockEmbeddingProvider: &handlerMockEmbeddingProvider{}}
	manager := NewJobManager(context.Background(), queue)
	t.Cleanup(manager.StopCleanup)
	server := &Server{provider: provider, queue: queue, embedder: embedder, jobManager: manager}

	requests := []JobRequest{{ID: "req", Messages: []Message{{Role: "user", Content: "hello"}}}}
	if _, err := manager.CreateJobIfCapacity(t.Context(), requests, "system"); err != nil {
		t.Fatalf("CreateJobIfCapacity() error = %v", err)
	}
	<-streamStarted

	deadline, cancel := context.WithCancel(t.Context())
	cancel()
	if err := server.Shutdown(deadline); err == nil {
		t.Fatal("Shutdown() succeeded before the active job drained")
	}
	if queue.count() != 0 || provider.count() != 0 || embedder.count() != 0 {
		t.Fatalf("resources closed on deadline: queue=%d provider=%d embedder=%d", queue.count(), provider.count(), embedder.count())
	}

	close(releaseStream)
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("retry Shutdown() error = %v", err)
	}
	if queue.count() != 1 || provider.count() != 1 || embedder.count() != 1 {
		t.Fatalf("resources not closed by retry: queue=%d provider=%d embedder=%d", queue.count(), provider.count(), embedder.count())
	}
}
