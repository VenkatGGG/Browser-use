package taskrunner

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

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
}

type Runner struct {
	tasks    task.Service
	nodes    pool.Registry
	executor nodeclient.Client
	cfg      Config
	logger   *log.Logger

	queue  chan string
	lease  map[string]struct{}
	leaseM sync.Mutex
}

func New(tasks task.Service, nodes pool.Registry, executor nodeclient.Client, cfg Config, logger *log.Logger) *Runner {
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
	if logger == nil {
		logger = log.Default()
	}

	return &Runner{
		tasks:    tasks,
		nodes:    nodes,
		executor: executor,
		cfg:      cfg,
		logger:   logger,
		queue:    make(chan string, cfg.QueueSize),
		lease:    make(map[string]struct{}),
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

	node, err := r.acquireNode(ctx)
	if err != nil {
		r.failTask(ctx, taskRecord, "", fmt.Errorf("no node available: %w", err))
		return
	}
	defer r.releaseNode(ctx, node)

	if _, err := r.tasks.Start(ctx, task.StartInput{
		TaskID:  taskRecord.ID,
		NodeID:  node.ID,
		Started: time.Now().UTC(),
	}); err != nil {
		r.failTask(ctx, taskRecord, node.ID, fmt.Errorf("failed to start task: %w", err))
		return
	}

	result, err := r.executor.Execute(ctx, node.Address, nodeclient.ExecuteInput{
		TaskID:  taskRecord.ID,
		URL:     taskRecord.URL,
		Goal:    taskRecord.Goal,
		Actions: mapNodeActions(taskRecord.Actions),
	})
	if err != nil {
		r.failTask(ctx, taskRecord, node.ID, fmt.Errorf("node execution failed: %w", err))
		return
	}

	if _, err := r.tasks.Complete(ctx, task.CompleteInput{
		TaskID:           taskRecord.ID,
		NodeID:           node.ID,
		Completed:        time.Now().UTC(),
		PageTitle:        result.PageTitle,
		FinalURL:         result.FinalURL,
		ScreenshotBase64: result.ScreenshotBase64,
	}); err != nil {
		r.failTask(ctx, taskRecord, node.ID, fmt.Errorf("failed to complete task: %w", err))
		return
	}
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

func (r *Runner) failTask(ctx context.Context, taskRecord task.Task, nodeID string, err error) {
	r.logger.Printf("task %s failed: %v", taskRecord.ID, err)
	_, _ = r.tasks.Fail(ctx, task.FailInput{
		TaskID:    taskRecord.ID,
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
