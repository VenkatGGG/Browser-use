package taskrunner

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net/url"
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
	QueueSize           int
	Workers             int
	NodeWaitTimeout     time.Duration
	PollInterval        time.Duration
	RetryBaseDelay      time.Duration
	RetryMaxDelay       time.Duration
	DomainBlockCooldown time.Duration
}

type Runner struct {
	tasks     task.Service
	nodes     pool.Registry
	executor  nodeclient.Client
	artifacts artifact.Store
	cfg       Config
	logger    *log.Logger

	queue    chan string
	enqueued map[string]struct{}
	enqueueM sync.Mutex
	lease    map[string]struct{}
	leaseM   sync.Mutex

	blockedDomains map[string]time.Time
	blockedM       sync.Mutex
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
	if cfg.DomainBlockCooldown <= 0 {
		cfg.DomainBlockCooldown = 3 * time.Minute
	}

	return &Runner{
		tasks:          tasks,
		nodes:          nodes,
		executor:       executor,
		artifacts:      artifacts,
		cfg:            cfg,
		logger:         logger,
		queue:          make(chan string, cfg.QueueSize),
		enqueued:       make(map[string]struct{}),
		lease:          make(map[string]struct{}),
		blockedDomains: make(map[string]time.Time),
	}
}

func (r *Runner) Start(ctx context.Context) {
	for workerID := 1; workerID <= r.cfg.Workers; workerID++ {
		id := workerID
		go r.worker(ctx, id)
	}
	go r.reconcileQueuedLoop(ctx)
}

func (r *Runner) Enqueue(ctx context.Context, taskID string) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return errors.New("task id is required")
	}

	r.enqueueM.Lock()
	if _, ok := r.enqueued[taskID]; ok {
		r.enqueueM.Unlock()
		return nil
	}
	r.enqueued[taskID] = struct{}{}
	r.enqueueM.Unlock()

	select {
	case <-ctx.Done():
		r.clearEnqueued(taskID)
		return ctx.Err()
	case r.queue <- taskID:
		return nil
	default:
		r.clearEnqueued(taskID)
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
			r.clearEnqueued(taskID)
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
		if errors.Is(err, task.ErrTaskNotQueued) {
			return
		}
		r.failTask(ctx, taskRecord.ID, "", fmt.Errorf("failed to start task: %w", err))
		return
	}

	if active, domain, until := r.isDomainBlocked(startedTask.URL, time.Now().UTC()); active {
		r.failTaskWithEvidence(ctx, startedTask.ID, "", fmt.Errorf("blocked (domain_cooldown): domain %s is in cooldown until %s", domain, until.Format(time.RFC3339)), failureEvidence{
			FinalURL:       startedTask.URL,
			BlockerType:    "domain_cooldown",
			BlockerMessage: "domain blocked after recent challenge; retry after cooldown",
		})
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

	screenshotBase64, screenshotArtifactURL := r.persistScreenshot(ctx, startedTask.ID, result.ScreenshotBase64)
	if result.BlockerType != "" {
		blockerMessage := strings.TrimSpace(result.BlockerMessage)
		if blockerMessage == "" {
			blockerMessage = "blocking challenge detected"
		}
		r.markDomainBlocked(firstNonEmpty(result.FinalURL, startedTask.URL), result.BlockerType, time.Now().UTC())
		r.failTaskWithEvidence(ctx, startedTask.ID, node.ID, fmt.Errorf("blocked (%s): %s", result.BlockerType, blockerMessage), failureEvidence{
			PageTitle:             result.PageTitle,
			FinalURL:              result.FinalURL,
			ScreenshotBase64:      screenshotBase64,
			ScreenshotArtifactURL: screenshotArtifactURL,
			BlockerType:           result.BlockerType,
			BlockerMessage:        blockerMessage,
		})
		return
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

func (r *Runner) reconcileQueuedLoop(ctx context.Context) {
	interval := r.cfg.PollInterval
	if interval < 500*time.Millisecond {
		interval = 500 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	r.reconcileQueuedOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reconcileQueuedOnce(ctx)
		}
	}
}

func (r *Runner) reconcileQueuedOnce(ctx context.Context) {
	items, err := r.tasks.ListQueued(ctx, r.cfg.QueueSize)
	if err != nil {
		r.logger.Printf("reconcile queued tasks failed: %v", err)
		return
	}
	for _, item := range items {
		enqueueCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		err := r.Enqueue(enqueueCtx, item.ID)
		cancel()
		if err != nil && !errors.Is(err, ErrQueueFull) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			r.logger.Printf("reconcile enqueue task %s failed: %v", item.ID, err)
		}
		if errors.Is(err, ErrQueueFull) {
			return
		}
	}
}

func (r *Runner) clearEnqueued(taskID string) {
	r.enqueueM.Lock()
	delete(r.enqueued, taskID)
	r.enqueueM.Unlock()
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
	nonRetriableSignals := []string{
		"captcha",
		"verify you are human",
		"human verification required",
		"confirm this search was made by a human",
		"please fill out this field",
		"timeout waiting for url to contain",
		"unsupported action type",
		"selector is required",
		"url is required",
		"invalid selector syntax",
	}
	for _, signal := range nonRetriableSignals {
		if strings.Contains(msg, signal) {
			return false
		}
	}

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
	r.failTaskWithEvidence(ctx, taskID, nodeID, err, failureEvidence{})
}

type failureEvidence struct {
	PageTitle             string
	FinalURL              string
	ScreenshotBase64      string
	ScreenshotArtifactURL string
	BlockerType           string
	BlockerMessage        string
}

func (r *Runner) failTaskWithEvidence(ctx context.Context, taskID, nodeID string, err error, evidence failureEvidence) {
	r.logger.Printf("task %s failed: %v", taskID, err)
	_, _ = r.tasks.Fail(ctx, task.FailInput{
		TaskID:                taskID,
		NodeID:                nodeID,
		Completed:             time.Now().UTC(),
		Error:                 err.Error(),
		PageTitle:             evidence.PageTitle,
		FinalURL:              evidence.FinalURL,
		Screenshot:            evidence.ScreenshotBase64,
		ScreenshotArtifactURL: evidence.ScreenshotArtifactURL,
		BlockerType:           evidence.BlockerType,
		BlockerMessage:        evidence.BlockerMessage,
	})
}

func (r *Runner) persistScreenshot(ctx context.Context, taskID, rawBase64 string) (string, string) {
	screenshotBase64 := strings.TrimSpace(rawBase64)
	screenshotArtifactURL := ""
	if r.artifacts != nil && screenshotBase64 != "" {
		url, saveErr := r.artifacts.SaveScreenshotBase64(ctx, taskID, screenshotBase64)
		if saveErr != nil {
			r.logger.Printf("task %s artifact save failed (falling back to inline screenshot): %v", taskID, saveErr)
		} else {
			screenshotArtifactURL = url
			screenshotBase64 = ""
		}
	}
	return screenshotBase64, screenshotArtifactURL
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

func (r *Runner) isDomainBlocked(rawURL string, now time.Time) (bool, string, time.Time) {
	host := normalizedHost(rawURL)
	if host == "" {
		return false, "", time.Time{}
	}
	r.blockedM.Lock()
	defer r.blockedM.Unlock()

	until, ok := r.blockedDomains[host]
	if !ok {
		return false, "", time.Time{}
	}
	if now.After(until) {
		delete(r.blockedDomains, host)
		return false, "", time.Time{}
	}
	return true, host, until
}

func (r *Runner) markDomainBlocked(rawURL, blockerType string, now time.Time) {
	blocker := strings.TrimSpace(strings.ToLower(blockerType))
	if blocker != "human_verification_required" && blocker != "bot_blocked" {
		return
	}
	host := normalizedHost(rawURL)
	if host == "" {
		return
	}

	until := now.Add(r.cfg.DomainBlockCooldown)
	r.blockedM.Lock()
	existing, ok := r.blockedDomains[host]
	if !ok || existing.Before(until) {
		r.blockedDomains[host] = until
	}
	r.blockedM.Unlock()
}

func normalizedHost(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	host := strings.TrimSpace(strings.ToLower(parsed.Hostname()))
	return host
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
