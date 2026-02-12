package taskrunner

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/VenkatGGG/Browser-use/internal/artifact"
	"github.com/VenkatGGG/Browser-use/internal/lease"
	"github.com/VenkatGGG/Browser-use/internal/nodeclient"
	"github.com/VenkatGGG/Browser-use/internal/pool"
	"github.com/VenkatGGG/Browser-use/internal/task"
)

type flakyExecutor struct {
	mu          sync.Mutex
	failures    int
	calls       int
	failureText string
}

func (f *flakyExecutor) Execute(_ context.Context, _ string, input nodeclient.ExecuteInput) (nodeclient.ExecuteOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.failures {
		if f.failureText == "" {
			f.failureText = "timeout while executing"
		}
		return nodeclient.ExecuteOutput{}, errors.New(f.failureText)
	}
	return nodeclient.ExecuteOutput{
		PageTitle:        "ok",
		FinalURL:         input.URL,
		ScreenshotBase64: "",
	}, nil
}

func (f *flakyExecutor) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type blockerExecutor struct {
	calls int
}

func (b *blockerExecutor) Execute(_ context.Context, _ string, input nodeclient.ExecuteInput) (nodeclient.ExecuteOutput, error) {
	b.calls++
	return nodeclient.ExecuteOutput{
		PageTitle:        "Challenge",
		FinalURL:         input.URL + "?captcha=true",
		ScreenshotBase64: "ZmFrZV9zY3JlZW5zaG90",
		BlockerType:      "human_verification_required",
		BlockerMessage:   "human verification challenge detected",
	}, nil
}

func (b *blockerExecutor) CallCount() int {
	return b.calls
}

type traceErrorExecutor struct {
	calls int
}

func (e *traceErrorExecutor) Execute(_ context.Context, _ string, input nodeclient.ExecuteInput) (nodeclient.ExecuteOutput, error) {
	e.calls++
	out := nodeclient.ExecuteOutput{
		PageTitle: "Failure Page",
		FinalURL:  input.URL,
		Trace: []nodeclient.StepTrace{
			{
				Index: 1,
				Action: nodeclient.Action{
					Type:     "wait_for",
					Selector: "input[name='q']",
				},
				Status:     "succeeded",
				OutputText: "ready",
				DurationMS: 100,
			},
			{
				Index: 2,
				Action: nodeclient.Action{
					Type:     "click",
					Selector: "button.buy",
				},
				Status:           "failed",
				Error:            "click failed: not_found",
				DurationMS:       300,
				ScreenshotBase64: "c3RlcC1zaG90",
			},
		},
	}
	return out, &nodeclient.ExecutionError{
		Message: "click failed: not_found",
		Output:  out,
	}
}

type concurrencyTrackingExecutor struct {
	mu            sync.Mutex
	inFlight      int
	maxConcurrent int
	delay         time.Duration
}

func (e *concurrencyTrackingExecutor) Execute(ctx context.Context, _ string, input nodeclient.ExecuteInput) (nodeclient.ExecuteOutput, error) {
	e.mu.Lock()
	e.inFlight++
	if e.inFlight > e.maxConcurrent {
		e.maxConcurrent = e.inFlight
	}
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.inFlight--
		e.mu.Unlock()
	}()

	delay := e.delay
	if delay <= 0 {
		delay = 120 * time.Millisecond
	}
	select {
	case <-ctx.Done():
		return nodeclient.ExecuteOutput{}, ctx.Err()
	case <-time.After(delay):
	}

	return nodeclient.ExecuteOutput{
		PageTitle: "ok",
		FinalURL:  input.URL,
	}, nil
}

func (e *concurrencyTrackingExecutor) MaxConcurrent() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.maxConcurrent
}

func TestRunnerRetriesTransientFailures(t *testing.T) {
	taskSvc := task.NewInMemoryService()
	nodes := pool.NewInMemoryRegistry()
	_, err := nodes.Register(context.Background(), pool.RegisterInput{NodeID: "node-1", Address: "node-1:8091", Version: "dev"})
	if err != nil {
		t.Fatalf("register node: %v", err)
	}

	exec := &flakyExecutor{failures: 1, failureText: "timeout while executing"}
	runner := New(taskSvc, nodes, exec, nil, Config{
		QueueSize:       8,
		Workers:         1,
		NodeWaitTimeout: 1 * time.Second,
		PollInterval:    30 * time.Millisecond,
		RetryBaseDelay:  20 * time.Millisecond,
		RetryMaxDelay:   100 * time.Millisecond,
	}, log.New(io.Discard, "", 0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)

	created, err := taskSvc.Create(ctx, task.CreateInput{
		SessionID:  "sess_1",
		URL:        "https://example.com",
		Goal:       "open",
		MaxRetries: 2,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := runner.Enqueue(ctx, created.ID); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	completed := waitForTerminalTask(t, ctx, taskSvc, created.ID, 4*time.Second)
	if completed.Status != task.StatusCompleted {
		t.Fatalf("expected completed status, got %s error=%s", completed.Status, completed.ErrorMessage)
	}
	if completed.Attempt != 2 {
		t.Fatalf("expected 2 attempts, got %d", completed.Attempt)
	}
	if exec.CallCount() != 2 {
		t.Fatalf("expected 2 executor calls, got %d", exec.CallCount())
	}
}

func TestRunnerDoesNotRetryNonRetriableErrors(t *testing.T) {
	taskSvc := task.NewInMemoryService()
	nodes := pool.NewInMemoryRegistry()
	_, err := nodes.Register(context.Background(), pool.RegisterInput{NodeID: "node-1", Address: "node-1:8091", Version: "dev"})
	if err != nil {
		t.Fatalf("register node: %v", err)
	}

	exec := &flakyExecutor{failures: 10, failureText: "invalid selector syntax"}
	runner := New(taskSvc, nodes, exec, nil, Config{
		QueueSize:       8,
		Workers:         1,
		NodeWaitTimeout: 1 * time.Second,
		PollInterval:    30 * time.Millisecond,
		RetryBaseDelay:  20 * time.Millisecond,
		RetryMaxDelay:   100 * time.Millisecond,
	}, log.New(io.Discard, "", 0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)

	created, err := taskSvc.Create(ctx, task.CreateInput{
		SessionID:  "sess_1",
		URL:        "https://example.com",
		Goal:       "open",
		MaxRetries: 3,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := runner.Enqueue(ctx, created.ID); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	failed := waitForTerminalTask(t, ctx, taskSvc, created.ID, 4*time.Second)
	if failed.Status != task.StatusFailed {
		t.Fatalf("expected failed status, got %s", failed.Status)
	}
	if failed.Attempt != 1 {
		t.Fatalf("expected 1 attempt, got %d", failed.Attempt)
	}
	if exec.CallCount() != 1 {
		t.Fatalf("expected 1 executor call, got %d", exec.CallCount())
	}
}

func TestRunnerDoesNotRetryURLAssertionTimeout(t *testing.T) {
	taskSvc := task.NewInMemoryService()
	nodes := pool.NewInMemoryRegistry()
	_, err := nodes.Register(context.Background(), pool.RegisterInput{NodeID: "node-1", Address: "node-1:8091", Version: "dev"})
	if err != nil {
		t.Fatalf("register node: %v", err)
	}

	exec := &flakyExecutor{failures: 10, failureText: `timeout waiting for URL to contain "browser use"`}
	runner := New(taskSvc, nodes, exec, nil, Config{
		QueueSize:       8,
		Workers:         1,
		NodeWaitTimeout: 1 * time.Second,
		PollInterval:    30 * time.Millisecond,
		RetryBaseDelay:  20 * time.Millisecond,
		RetryMaxDelay:   100 * time.Millisecond,
	}, log.New(io.Discard, "", 0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)

	created, err := taskSvc.Create(ctx, task.CreateInput{
		SessionID:  "sess_1",
		URL:        "https://example.com",
		Goal:       "open",
		MaxRetries: 3,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := runner.Enqueue(ctx, created.ID); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	failed := waitForTerminalTask(t, ctx, taskSvc, created.ID, 4*time.Second)
	if failed.Status != task.StatusFailed {
		t.Fatalf("expected failed status, got %s", failed.Status)
	}
	if failed.Attempt != 1 {
		t.Fatalf("expected 1 attempt, got %d", failed.Attempt)
	}
	if exec.CallCount() != 1 {
		t.Fatalf("expected 1 executor call, got %d", exec.CallCount())
	}
}

func TestRunnerDoesNotRetrySelectorTimeout(t *testing.T) {
	taskSvc := task.NewInMemoryService()
	nodes := pool.NewInMemoryRegistry()
	_, err := nodes.Register(context.Background(), pool.RegisterInput{NodeID: "node-1", Address: "node-1:8091", Version: "dev"})
	if err != nil {
		t.Fatalf("register node: %v", err)
	}

	exec := &flakyExecutor{failures: 10, failureText: `timeout waiting for selector "input[name='q']"`}
	runner := New(taskSvc, nodes, exec, nil, Config{
		QueueSize:       8,
		Workers:         1,
		NodeWaitTimeout: 1 * time.Second,
		PollInterval:    30 * time.Millisecond,
		RetryBaseDelay:  20 * time.Millisecond,
		RetryMaxDelay:   100 * time.Millisecond,
	}, log.New(io.Discard, "", 0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)

	created, err := taskSvc.Create(ctx, task.CreateInput{
		SessionID:  "sess_1",
		URL:        "https://example.com",
		Goal:       "open",
		MaxRetries: 3,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := runner.Enqueue(ctx, created.ID); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	failed := waitForTerminalTask(t, ctx, taskSvc, created.ID, 4*time.Second)
	if failed.Status != task.StatusFailed {
		t.Fatalf("expected failed status, got %s", failed.Status)
	}
	if failed.Attempt != 1 {
		t.Fatalf("expected 1 attempt, got %d", failed.Attempt)
	}
	if exec.CallCount() != 1 {
		t.Fatalf("expected 1 executor call, got %d", exec.CallCount())
	}
}

func TestRunnerDoesNotRetryNotFoundActionFailures(t *testing.T) {
	taskSvc := task.NewInMemoryService()
	nodes := pool.NewInMemoryRegistry()
	_, err := nodes.Register(context.Background(), pool.RegisterInput{NodeID: "node-1", Address: "node-1:8091", Version: "dev"})
	if err != nil {
		t.Fatalf("register node: %v", err)
	}

	exec := &flakyExecutor{failures: 10, failureText: "click failed: not_found"}
	runner := New(taskSvc, nodes, exec, nil, Config{
		QueueSize:       8,
		Workers:         1,
		NodeWaitTimeout: 1 * time.Second,
		PollInterval:    30 * time.Millisecond,
		RetryBaseDelay:  20 * time.Millisecond,
		RetryMaxDelay:   100 * time.Millisecond,
	}, log.New(io.Discard, "", 0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)

	created, err := taskSvc.Create(ctx, task.CreateInput{
		SessionID:  "sess_1",
		URL:        "https://example.com",
		Goal:       "open",
		MaxRetries: 3,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := runner.Enqueue(ctx, created.ID); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	failed := waitForTerminalTask(t, ctx, taskSvc, created.ID, 4*time.Second)
	if failed.Status != task.StatusFailed {
		t.Fatalf("expected failed status, got %s", failed.Status)
	}
	if failed.Attempt != 1 {
		t.Fatalf("expected 1 attempt, got %d", failed.Attempt)
	}
	if exec.CallCount() != 1 {
		t.Fatalf("expected 1 executor call, got %d", exec.CallCount())
	}
}

func TestRunnerDoesNotRetryExtractTextNotFoundFailures(t *testing.T) {
	taskSvc := task.NewInMemoryService()
	nodes := pool.NewInMemoryRegistry()
	_, err := nodes.Register(context.Background(), pool.RegisterInput{NodeID: "node-1", Address: "node-1:8091", Version: "dev"})
	if err != nil {
		t.Fatalf("register node: %v", err)
	}

	exec := &flakyExecutor{failures: 10, failureText: "extract_text failed: not_found"}
	runner := New(taskSvc, nodes, exec, nil, Config{
		QueueSize:       8,
		Workers:         1,
		NodeWaitTimeout: 1 * time.Second,
		PollInterval:    30 * time.Millisecond,
		RetryBaseDelay:  20 * time.Millisecond,
		RetryMaxDelay:   100 * time.Millisecond,
	}, log.New(io.Discard, "", 0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)

	created, err := taskSvc.Create(ctx, task.CreateInput{
		SessionID:  "sess_1",
		URL:        "https://example.com",
		Goal:       "open",
		MaxRetries: 3,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := runner.Enqueue(ctx, created.ID); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	failed := waitForTerminalTask(t, ctx, taskSvc, created.ID, 4*time.Second)
	if failed.Status != task.StatusFailed {
		t.Fatalf("expected failed status, got %s", failed.Status)
	}
	if failed.Attempt != 1 {
		t.Fatalf("expected 1 attempt, got %d", failed.Attempt)
	}
	if exec.CallCount() != 1 {
		t.Fatalf("expected 1 executor call, got %d", exec.CallCount())
	}
}

func TestRunnerFailsFastOnBlockerResponse(t *testing.T) {
	taskSvc := task.NewInMemoryService()
	nodes := pool.NewInMemoryRegistry()
	_, err := nodes.Register(context.Background(), pool.RegisterInput{NodeID: "node-1", Address: "node-1:8091", Version: "dev"})
	if err != nil {
		t.Fatalf("register node: %v", err)
	}

	exec := &blockerExecutor{}
	runner := New(taskSvc, nodes, exec, nil, Config{
		QueueSize:       8,
		Workers:         1,
		NodeWaitTimeout: 1 * time.Second,
		PollInterval:    30 * time.Millisecond,
		RetryBaseDelay:  20 * time.Millisecond,
		RetryMaxDelay:   100 * time.Millisecond,
	}, log.New(io.Discard, "", 0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)

	created, err := taskSvc.Create(ctx, task.CreateInput{
		SessionID:  "sess_1",
		URL:        "https://example.com",
		Goal:       "search",
		MaxRetries: 2,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := runner.Enqueue(ctx, created.ID); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	failed := waitForTerminalTask(t, ctx, taskSvc, created.ID, 4*time.Second)
	if failed.Status != task.StatusFailed {
		t.Fatalf("expected failed status, got %s", failed.Status)
	}
	if failed.Attempt != 1 {
		t.Fatalf("expected 1 attempt, got %d", failed.Attempt)
	}
	if !strings.Contains(failed.ErrorMessage, "human_verification_required") {
		t.Fatalf("expected blocker type in error, got %q", failed.ErrorMessage)
	}
	if failed.PageTitle != "Challenge" {
		t.Fatalf("expected page title evidence, got %q", failed.PageTitle)
	}
	if failed.FinalURL == "" {
		t.Fatalf("expected final url evidence")
	}
	if failed.ScreenshotBase64 == "" {
		t.Fatalf("expected screenshot evidence")
	}
	if failed.BlockerType != "human_verification_required" {
		t.Fatalf("expected blocker type persisted, got %q", failed.BlockerType)
	}
	if failed.BlockerMessage == "" {
		t.Fatalf("expected blocker message persisted")
	}
}

func TestRunnerPersistsTraceFromExecutionErrorMetadata(t *testing.T) {
	taskSvc := task.NewInMemoryService()
	nodes := pool.NewInMemoryRegistry()
	_, err := nodes.Register(context.Background(), pool.RegisterInput{NodeID: "node-1", Address: "node-1:8091", Version: "dev"})
	if err != nil {
		t.Fatalf("register node: %v", err)
	}

	exec := &traceErrorExecutor{}
	runner := New(taskSvc, nodes, exec, nil, Config{
		QueueSize:       8,
		Workers:         1,
		NodeWaitTimeout: 1 * time.Second,
		PollInterval:    30 * time.Millisecond,
		RetryBaseDelay:  20 * time.Millisecond,
		RetryMaxDelay:   100 * time.Millisecond,
	}, log.New(io.Discard, "", 0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)

	created, err := taskSvc.Create(ctx, task.CreateInput{
		SessionID:  "sess_1",
		URL:        "https://example.com",
		Goal:       "buy",
		MaxRetries: 2,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := runner.Enqueue(ctx, created.ID); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	failed := waitForTerminalTask(t, ctx, taskSvc, created.ID, 4*time.Second)
	if failed.Status != task.StatusFailed {
		t.Fatalf("expected failed status, got %s", failed.Status)
	}
	if failed.Attempt != 1 {
		t.Fatalf("expected 1 attempt, got %d", failed.Attempt)
	}
	if len(failed.Trace) != 2 {
		t.Fatalf("expected persisted trace with 2 steps, got %d", len(failed.Trace))
	}
	if failed.Trace[1].Status != "failed" {
		t.Fatalf("expected second trace step failed, got %q", failed.Trace[1].Status)
	}
	if failed.Trace[1].Action.Type != "click" {
		t.Fatalf("expected second trace action click, got %q", failed.Trace[1].Action.Type)
	}
	if failed.Trace[0].OutputText != "ready" {
		t.Fatalf("expected first trace step output_text to persist, got %q", failed.Trace[0].OutputText)
	}
	if failed.Trace[1].ScreenshotBase64 == "" {
		t.Fatalf("expected step screenshot inline data when artifact store is not configured")
	}
}

func TestRunnerPersistsTraceScreenshotArtifactsWhenStoreConfigured(t *testing.T) {
	taskSvc := task.NewInMemoryService()
	nodes := pool.NewInMemoryRegistry()
	_, err := nodes.Register(context.Background(), pool.RegisterInput{NodeID: "node-1", Address: "node-1:8091", Version: "dev"})
	if err != nil {
		t.Fatalf("register node: %v", err)
	}

	tempDir := t.TempDir()
	store, err := artifact.NewLocalStore(tempDir, "/artifacts")
	if err != nil {
		t.Fatalf("new local artifact store: %v", err)
	}

	exec := &traceErrorExecutor{}
	runner := New(taskSvc, nodes, exec, store, Config{
		QueueSize:       8,
		Workers:         1,
		NodeWaitTimeout: 1 * time.Second,
		PollInterval:    30 * time.Millisecond,
		RetryBaseDelay:  20 * time.Millisecond,
		RetryMaxDelay:   100 * time.Millisecond,
	}, log.New(io.Discard, "", 0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)

	created, err := taskSvc.Create(ctx, task.CreateInput{
		SessionID:  "sess_1",
		URL:        "https://example.com",
		Goal:       "buy",
		MaxRetries: 2,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := runner.Enqueue(ctx, created.ID); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	failed := waitForTerminalTask(t, ctx, taskSvc, created.ID, 4*time.Second)
	if failed.Status != task.StatusFailed {
		t.Fatalf("expected failed status, got %s", failed.Status)
	}
	if len(failed.Trace) != 2 {
		t.Fatalf("expected persisted trace with 2 steps, got %d", len(failed.Trace))
	}

	step := failed.Trace[1]
	if step.ScreenshotArtifactURL == "" {
		t.Fatalf("expected trace screenshot artifact url to be populated")
	}
	if step.ScreenshotBase64 != "" {
		t.Fatalf("expected inline trace screenshot to be cleared after artifact save")
	}

	entries, err := os.ReadDir(filepath.Join(tempDir, "screenshots"))
	if err != nil {
		t.Fatalf("read artifact screenshots dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected screenshot artifacts to be written")
	}
}

func TestRunnerAppliesDomainCooldownAfterBlocker(t *testing.T) {
	taskSvc := task.NewInMemoryService()
	nodes := pool.NewInMemoryRegistry()
	_, err := nodes.Register(context.Background(), pool.RegisterInput{NodeID: "node-1", Address: "node-1:8091", Version: "dev"})
	if err != nil {
		t.Fatalf("register node: %v", err)
	}

	exec := &blockerExecutor{}
	runner := New(taskSvc, nodes, exec, nil, Config{
		QueueSize:           8,
		Workers:             1,
		NodeWaitTimeout:     1 * time.Second,
		PollInterval:        30 * time.Millisecond,
		RetryBaseDelay:      20 * time.Millisecond,
		RetryMaxDelay:       100 * time.Millisecond,
		DomainBlockCooldown: 2 * time.Second,
	}, log.New(io.Discard, "", 0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)

	first, err := taskSvc.Create(ctx, task.CreateInput{
		SessionID:  "sess_1",
		URL:        "https://example.com/search",
		Goal:       "search one",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("create first task: %v", err)
	}
	if err := runner.Enqueue(ctx, first.ID); err != nil {
		t.Fatalf("enqueue first task: %v", err)
	}

	firstResult := waitForTerminalTask(t, ctx, taskSvc, first.ID, 4*time.Second)
	if firstResult.Status != task.StatusFailed {
		t.Fatalf("expected first task failed, got %s", firstResult.Status)
	}
	if firstResult.BlockerType != "human_verification_required" {
		t.Fatalf("expected first blocker type human_verification_required, got %q", firstResult.BlockerType)
	}

	second, err := taskSvc.Create(ctx, task.CreateInput{
		SessionID:  "sess_2",
		URL:        "https://example.com/another",
		Goal:       "search two",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("create second task: %v", err)
	}
	if err := runner.Enqueue(ctx, second.ID); err != nil {
		t.Fatalf("enqueue second task: %v", err)
	}

	secondResult := waitForTerminalTask(t, ctx, taskSvc, second.ID, 4*time.Second)
	if secondResult.Status != task.StatusFailed {
		t.Fatalf("expected second task failed, got %s", secondResult.Status)
	}
	if secondResult.BlockerType != "domain_cooldown" {
		t.Fatalf("expected second blocker type domain_cooldown, got %q", secondResult.BlockerType)
	}
	if exec.CallCount() != 1 {
		t.Fatalf("expected only 1 executor call due to domain cooldown, got %d", exec.CallCount())
	}
}

func TestRunnerReconcilesQueuedTasksOnStart(t *testing.T) {
	taskSvc := task.NewInMemoryService()
	nodes := pool.NewInMemoryRegistry()
	_, err := nodes.Register(context.Background(), pool.RegisterInput{NodeID: "node-1", Address: "node-1:8091", Version: "dev"})
	if err != nil {
		t.Fatalf("register node: %v", err)
	}

	created, err := taskSvc.Create(context.Background(), task.CreateInput{
		SessionID:  "sess_1",
		URL:        "https://example.com",
		Goal:       "open",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	exec := &flakyExecutor{}
	runner := New(taskSvc, nodes, exec, nil, Config{
		QueueSize:       8,
		Workers:         1,
		NodeWaitTimeout: 1 * time.Second,
		PollInterval:    30 * time.Millisecond,
		RetryBaseDelay:  20 * time.Millisecond,
		RetryMaxDelay:   100 * time.Millisecond,
	}, log.New(io.Discard, "", 0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)

	completed := waitForTerminalTask(t, ctx, taskSvc, created.ID, 4*time.Second)
	if completed.Status != task.StatusCompleted {
		t.Fatalf("expected completed status, got %s error=%s", completed.Status, completed.ErrorMessage)
	}
	if completed.Attempt != 1 {
		t.Fatalf("expected 1 attempt, got %d", completed.Attempt)
	}
	if exec.CallCount() != 1 {
		t.Fatalf("expected 1 executor call, got %d", exec.CallCount())
	}
}

func TestRunnerSharedLeasePreventsConcurrentExecutionOnSingleNode(t *testing.T) {
	taskSvc := task.NewInMemoryService()
	nodes := pool.NewInMemoryRegistry()
	_, err := nodes.Register(context.Background(), pool.RegisterInput{NodeID: "node-1", Address: "node-1:8091", Version: "dev"})
	if err != nil {
		t.Fatalf("register node: %v", err)
	}

	sharedLeaser := lease.NewInMemoryManager()
	exec := &concurrencyTrackingExecutor{delay: 140 * time.Millisecond}

	runner1 := New(taskSvc, nodes, exec, nil, Config{
		QueueSize:           8,
		Workers:             1,
		NodeWaitTimeout:     2 * time.Second,
		PollInterval:        20 * time.Millisecond,
		RetryBaseDelay:      20 * time.Millisecond,
		RetryMaxDelay:       100 * time.Millisecond,
		Leaser:              sharedLeaser,
		RunnerInstanceID:    "runner-a",
		DomainBlockCooldown: 2 * time.Second,
	}, log.New(io.Discard, "", 0))
	runner2 := New(taskSvc, nodes, exec, nil, Config{
		QueueSize:           8,
		Workers:             1,
		NodeWaitTimeout:     2 * time.Second,
		PollInterval:        20 * time.Millisecond,
		RetryBaseDelay:      20 * time.Millisecond,
		RetryMaxDelay:       100 * time.Millisecond,
		Leaser:              sharedLeaser,
		RunnerInstanceID:    "runner-b",
		DomainBlockCooldown: 2 * time.Second,
	}, log.New(io.Discard, "", 0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner1.Start(ctx)
	runner2.Start(ctx)

	first, err := taskSvc.Create(ctx, task.CreateInput{
		SessionID:  "sess_1",
		URL:        "https://example.com/1",
		Goal:       "one",
		MaxRetries: 0,
	})
	if err != nil {
		t.Fatalf("create first task: %v", err)
	}
	second, err := taskSvc.Create(ctx, task.CreateInput{
		SessionID:  "sess_2",
		URL:        "https://example.com/2",
		Goal:       "two",
		MaxRetries: 0,
	})
	if err != nil {
		t.Fatalf("create second task: %v", err)
	}

	if err := runner1.Enqueue(ctx, first.ID); err != nil {
		t.Fatalf("enqueue first task: %v", err)
	}
	if err := runner2.Enqueue(ctx, second.ID); err != nil {
		t.Fatalf("enqueue second task: %v", err)
	}

	firstResult := waitForTerminalTask(t, ctx, taskSvc, first.ID, 5*time.Second)
	secondResult := waitForTerminalTask(t, ctx, taskSvc, second.ID, 5*time.Second)
	if firstResult.Status != task.StatusCompleted {
		t.Fatalf("expected first task completed, got %s", firstResult.Status)
	}
	if secondResult.Status != task.StatusCompleted {
		t.Fatalf("expected second task completed, got %s", secondResult.Status)
	}

	if got := exec.MaxConcurrent(); got != 1 {
		t.Fatalf("expected max executor concurrency 1 with shared lease on single node, got %d", got)
	}
}

func waitForTerminalTask(t *testing.T, ctx context.Context, svc task.Service, id string, timeout time.Duration) task.Task {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		found, err := svc.Get(ctx, id)
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		if found.Status == task.StatusCompleted || found.Status == task.StatusFailed {
			return found
		}
		time.Sleep(30 * time.Millisecond)
	}
	last, _ := svc.Get(ctx, id)
	t.Fatalf("task did not reach terminal state in %s (current=%s)", timeout, last.Status)
	return task.Task{}
}
