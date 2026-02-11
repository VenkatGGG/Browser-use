package taskrunner

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"testing"
	"time"

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
