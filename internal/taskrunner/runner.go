package taskrunner

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/VenkatGGG/Browser-use/internal/artifact"
	"github.com/VenkatGGG/Browser-use/internal/nodeclient"
	"github.com/VenkatGGG/Browser-use/internal/pool"
	"github.com/VenkatGGG/Browser-use/internal/task"
)

var ErrQueueFull = errors.New("task queue is full")

type Config struct {
	QueueSize       int
	Workers         int
	NodeWaitTimeout time.Duration
	PollInterval    time.Duration
	RetryBaseDelay  time.Duration
	RetryMaxDelay   time.Duration
}

type Runner struct {
	tasks     task.Service
	nodes     pool.Registry
	executor  nodeclient.Client
	artifacts artifact.Store
	cfg       Config
	logger    *log.Logger

	queue  chan string
	lease  map[string]struct{}
	leaseM sync.Mutex
}

func New(tasks task.Service, nodes pool.Registry, executor nodeclient.Client, artifacts artifact.Store, cfg Config, logger *log.Logger) *Runner {
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 256
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.NodeWaitTimeout <= 0 {
		cfg.NodeWaitTimeout = 30 * time.Second
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 250 * time.Millisecond
	}
	if cfg.RetryBaseDelay <= 0 {
		cfg.RetryBaseDelay = 1 * time.Second
	}
	if cfg.RetryMaxDelay <= 0 {
		cfg.RetryMaxDelay = 20 * time.Second
	}
	if cfg.RetryMaxDelay < cfg.RetryBaseDelay {
		cfg.RetryMaxDelay = cfg.RetryBaseDelay
	}
	if logger == nil {
		logger = log.Default()
	}

	return &Runner{
		tasks:     tasks,
		nodes:     nodes,
		executor:  executor,
		artifacts: artifacts,
		cfg:       cfg,
		logger:    logger,
		queue:     make(chan string, cfg.QueueSize),
		lease:     make(map[string]struct{}),
	}
}

func (r *Runner) Start(ctx context.Context) {
	for workerID := 1; workerID <= r.cfg.Workers; workerID++ {
		id := workerID
		go r.worker(ctx, id)
	}
}

func (r *Runner) Enqueue(ctx context.Context, taskID string) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return errors.New("task id is required")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case r.queue <- taskID:
		return nil
	default:
		return ErrQueueFull
	}
}

func (r *Runner) worker(ctx context.Context, workerID int) {
	r.logger.Printf("taskrunner worker %d started", workerID)
	for {
		select {
		case <-ctx.Done():
			r.logger.Printf("taskrunner worker %d stopping", workerID)
			return
		case taskID := <-r.queue:
			r.processTask(ctx, workerID, taskID)
		}
	}
}

func (r *Runner) processTask(ctx context.Context, workerID int, taskID string) {
	taskRecord, err := r.tasks.Get(ctx, taskID)
	if err != nil {
		r.logger.Printf("worker %d unable to load task %s: %v", workerID, taskID, err)
		return
	}

	if taskRecord.Status != task.StatusQueued {
		return
	}

	startedTask, err := r.tasks.Start(ctx, task.StartInput{
		TaskID:  taskRecord.ID,
		NodeID:  "",
		Started: time.Now().UTC(),
	})
	if err != nil {
		r.failTask(ctx, taskRecord.ID, "", fmt.Errorf("failed to start task: %w", err))
		return
	}

	node, err := r.acquireNode(ctx)
	if err != nil {
		r.handleExecutionFailure(ctx, startedTask, "", fmt.Errorf("no node available: %w", err))
		return
	}
	defer r.releaseNode(ctx, node)

	result, err := r.executor.Execute(ctx, node.Address, nodeclient.ExecuteInput{
		TaskID:  startedTask.ID,
		URL:     startedTask.URL,
		Goal:    startedTask.Goal,
		Actions: mapNodeActions(startedTask.Actions),
	})
	if err != nil {
		r.handleExecutionFailure(ctx, startedTask, node.ID, fmt.Errorf("node execution failed: %w", err))
		return
	}

	screenshotBase64 := result.ScreenshotBase64
	screenshotArtifactURL := ""
	if r.artifacts != nil && strings.TrimSpace(result.ScreenshotBase64) != "" {
		url, saveErr := r.artifacts.SaveScreenshotBase64(ctx, startedTask.ID, result.ScreenshotBase64)
		if saveErr != nil {
			r.logger.Printf("task %s artifact save failed (falling back to inline screenshot): %v", startedTask.ID, saveErr)
		} else {
			screenshotArtifactURL = url
			screenshotBase64 = ""
		}
	}

	if _, err := r.tasks.Complete(ctx, task.CompleteInput{
		TaskID:                startedTask.ID,
		NodeID:                node.ID,
		Completed:             time.Now().UTC(),
		PageTitle:             result.PageTitle,
		FinalURL:              result.FinalURL,
		ScreenshotBase64:      screenshotBase64,
		ScreenshotArtifactURL: screenshotArtifactURL,
	}); err != nil {
		r.failTask(ctx, startedTask.ID, node.ID, fmt.Errorf("failed to complete task: %w", err))
		return
	}
}

func (r *Runner) handleExecutionFailure(ctx context.Context, taskRecord task.Task, nodeID string, execErr error) {
	if r.shouldRetry(taskRecord, execErr) {
		delay := r.retryDelay(taskRecord.Attempt)
		retryAt := time.Now().UTC().Add(delay)
		if _, err := r.tasks.Retry(ctx, task.RetryInput{
			TaskID:    taskRecord.ID,
			RetryAt:   retryAt,
			LastError: execErr.Error(),
		}); err != nil {
			r.failTask(ctx, taskRecord.ID, nodeID, fmt.Errorf("retry scheduling failed: %w", err))
			return
		}
		r.logger.Printf(
			"task %s attempt=%d failed; scheduling retry in %s (%d/%d retries used)",
			taskRecord.ID,
			taskRecord.Attempt,
			delay,
			taskRecord.Attempt,
			taskRecord.MaxRetries,
		)
		go r.enqueueAfterDelay(ctx, taskRecord.ID, delay)
		return
	}

	r.failTask(ctx, taskRecord.ID, nodeID, execErr)
}

func (r *Runner) shouldRetry(taskRecord task.Task, execErr error) bool {
	if taskRecord.MaxRetries <= 0 {
		return false
	}
	if taskRecord.Attempt > taskRecord.MaxRetries {
		return false
	}
	return isRetriableError(execErr)
}

func (r *Runner) retryDelay(attempt int) time.Duration {
	exponent := math.Max(0, float64(attempt-1))
	delay := float64(r.cfg.RetryBaseDelay) * math.Pow(2, exponent)
	if delay > float64(r.cfg.RetryMaxDelay) {
		delay = float64(r.cfg.RetryMaxDelay)
	}
	return time.Duration(delay)
}

func isRetriableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	msg := strings.ToLower(err.Error())
	signals := []string{
		"timeout",
		"temporarily",
		"connection refused",
		"connection reset",
		"no such host",
		"dial tcp",
		"no node available",
		"bad gateway",
		"eof",
		"unexpected status 5",
	}
	for _, signal := range signals {
		if strings.Contains(msg, signal) {
			return true
		}
	}
	return false
}

func (r *Runner) enqueueAfterDelay(ctx context.Context, taskID string, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}

	for attempts := 0; attempts < 10; attempts++ {
		enqueueCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
		err := r.Enqueue(enqueueCtx, taskID)
		cancel()
		if err == nil {
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		if !errors.Is(err, ErrQueueFull) {
			r.failTask(context.Background(), taskID, "", fmt.Errorf("re-enqueue failed: %w", err))
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(300 * time.Millisecond):
		}
	}

	r.failTask(context.Background(), taskID, "", errors.New("re-enqueue failed: queue remained full"))
}

func (r *Runner) acquireNode(ctx context.Context) (pool.Node, error) {
	waitCtx, cancel := context.WithTimeout(ctx, r.cfg.NodeWaitTimeout)
	defer cancel()

	for {
		node, ok, err := r.tryAcquireReadyNode(waitCtx)
		if err != nil {
			return pool.Node{}, err
		}
		if ok {
			return node, nil
		}

		select {
		case <-waitCtx.Done():
			return pool.Node{}, waitCtx.Err()
		case <-time.After(r.cfg.PollInterval):
		}
	}
}

func (r *Runner) tryAcquireReadyNode(ctx context.Context) (pool.Node, bool, error) {
	nodes, err := r.nodes.List(ctx)
	if err != nil {
		return pool.Node{}, false, err
	}

	r.leaseM.Lock()
	defer r.leaseM.Unlock()

	for _, node := range nodes {
		if node.State != pool.NodeStateReady {
			continue
		}
		if _, busy := r.lease[node.ID]; busy {
			continue
		}
		r.lease[node.ID] = struct{}{}
		_, _ = r.nodes.Heartbeat(context.Background(), pool.HeartbeatInput{
			NodeID: node.ID,
			State:  pool.NodeStateLeased,
			At:     time.Now().UTC(),
		})
		return node, true, nil
	}
	return pool.Node{}, false, nil
}

func (r *Runner) releaseNode(ctx context.Context, node pool.Node) {
	r.leaseM.Lock()
	delete(r.lease, node.ID)
	r.leaseM.Unlock()

	_, _ = r.nodes.Heartbeat(ctx, pool.HeartbeatInput{
		NodeID: node.ID,
		State:  pool.NodeStateReady,
		At:     time.Now().UTC(),
	})
}

func (r *Runner) failTask(ctx context.Context, taskID, nodeID string, err error) {
	r.logger.Printf("task %s failed: %v", taskID, err)
	_, _ = r.tasks.Fail(ctx, task.FailInput{
		TaskID:    taskID,
		NodeID:    nodeID,
		Completed: time.Now().UTC(),
		Error:     err.Error(),
	})
}

func mapNodeActions(actions []task.Action) []nodeclient.Action {
	mapped := make([]nodeclient.Action, 0, len(actions))
	for _, action := range actions {
		mapped = append(mapped, nodeclient.Action{
			Type:      action.Type,
			Selector:  action.Selector,
			Text:      action.Text,
			TimeoutMS: action.TimeoutMS,
			DelayMS:   action.DelayMS,
		})
	}
	return mapped
}
