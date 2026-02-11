package pool

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	registry Registry
	provider Provider
	cfg      ManagerConfig
	logger   *log.Logger

	warmingMu sync.Mutex
	warming   map[string]time.Time
}

type ManagerConfig struct {
	TargetReady       int
	ReconcileInterval time.Duration
	HeartbeatTimeout  time.Duration
	NodeMaxAge        time.Duration
	ManagedNodePrefix string
	ProvisionLabels   map[string]string
}

func NewManager(registry Registry, provider Provider, cfg ManagerConfig, logger *log.Logger) *Manager {
	if cfg.TargetReady < 0 {
		cfg.TargetReady = 0
	}
	if cfg.ReconcileInterval <= 0 {
		cfg.ReconcileInterval = 5 * time.Second
	}
	if cfg.HeartbeatTimeout <= 0 {
		cfg.HeartbeatTimeout = 30 * time.Second
	}
	if cfg.NodeMaxAge <= 0 {
		cfg.NodeMaxAge = 1 * time.Hour
	}
	if logger == nil {
		logger = log.Default()
	}

	return &Manager{
		registry: registry,
		provider: provider,
		cfg:      cfg,
		logger:   logger,
		warming:  make(map[string]time.Time),
	}
}

func (m *Manager) Run(ctx context.Context) {
	if m.cfg.TargetReady <= 0 {
		m.logger.Printf("pool manager disabled: target_ready=%d", m.cfg.TargetReady)
		return
	}

	ticker := time.NewTicker(m.cfg.ReconcileInterval)
	defer ticker.Stop()

	m.logger.Printf(
		"pool manager started: target_ready=%d reconcile=%s heartbeat_timeout=%s node_max_age=%s prefix=%q",
		m.cfg.TargetReady,
		m.cfg.ReconcileInterval,
		m.cfg.HeartbeatTimeout,
		m.cfg.NodeMaxAge,
		m.cfg.ManagedNodePrefix,
	)

	m.reconcile(ctx, m.cfg.TargetReady)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.reconcile(ctx, m.cfg.TargetReady)
		}
	}
}

func (m *Manager) Reconcile(ctx context.Context, targetReady int) error {
	if targetReady < 0 {
		targetReady = 0
	}
	return m.reconcile(ctx, targetReady)
}

func (m *Manager) reconcile(ctx context.Context, targetReady int) error {
	now := time.Now().UTC()
	nodes, err := m.registry.List(ctx)
	if err != nil {
		return err
	}

	knownManaged := make(map[string]struct{}, len(nodes))
	readyCount := 0
	for _, node := range nodes {
		if !m.isManagedNode(node.ID) {
			if node.State == NodeStateReady {
				readyCount++
			}
			continue
		}
		knownManaged[node.ID] = struct{}{}

		if node.State == NodeStateReady {
			readyCount++
		}

		if m.shouldReapByHeartbeat(node, now) {
			m.logger.Printf("pool manager reaping stale node by heartbeat timeout: node_id=%s", node.ID)
			m.destroyNode(ctx, node.ID, now)
			continue
		}
		if m.shouldReapByAge(node, now) {
			m.logger.Printf("pool manager reaping node by max age: node_id=%s", node.ID)
			m.destroyNode(ctx, node.ID, now)
			continue
		}
	}

	m.pruneWarmingKnown(knownManaged)
	warmingCount := m.reapTimedOutWarming(ctx, now)
	needed := targetReady - readyCount - warmingCount
	if needed <= 0 {
		return nil
	}

	var provisionErr error
	for i := 0; i < needed; i++ {
		node, err := m.provider.ProvisionNode(ctx, ProvisionInput{
			Labels: m.cfg.ProvisionLabels,
		})
		if err != nil {
			if provisionErr == nil {
				provisionErr = err
			}
			m.logger.Printf("pool manager provision failed: %v", err)
			continue
		}
		m.setWarming(node.ID, now)
		m.logger.Printf("pool manager provisioned node: node_id=%s address=%s", node.ID, node.Address)
	}
	return provisionErr
}

func (m *Manager) shouldReapByHeartbeat(node Node, now time.Time) bool {
	if node.State == NodeStateDead {
		return false
	}
	if node.LastHeartbeat.IsZero() {
		return false
	}
	return now.Sub(node.LastHeartbeat.UTC()) > m.cfg.HeartbeatTimeout
}

func (m *Manager) shouldReapByAge(node Node, now time.Time) bool {
	if node.State == NodeStateLeased || node.BootedAt.IsZero() {
		return false
	}
	return now.Sub(node.BootedAt.UTC()) > m.cfg.NodeMaxAge
}

func (m *Manager) destroyNode(ctx context.Context, nodeID string, at time.Time) {
	if err := m.provider.DestroyNode(ctx, nodeID); err != nil {
		m.logger.Printf("pool manager destroy failed: node_id=%s err=%v", nodeID, err)
	}
	if _, err := m.registry.Heartbeat(ctx, HeartbeatInput{
		NodeID: nodeID,
		State:  NodeStateDead,
		At:     at,
	}); err != nil && !errors.Is(err, ErrNodeNotFound) {
		m.logger.Printf("pool manager mark dead failed: node_id=%s err=%v", nodeID, err)
	}
	m.deleteWarming(nodeID)
}

func (m *Manager) isManagedNode(nodeID string) bool {
	prefix := strings.TrimSpace(m.cfg.ManagedNodePrefix)
	if prefix == "" {
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(nodeID), prefix)
}

func (m *Manager) setWarming(nodeID string, at time.Time) {
	m.warmingMu.Lock()
	defer m.warmingMu.Unlock()
	m.warming[nodeID] = at.UTC()
}

func (m *Manager) deleteWarming(nodeID string) {
	m.warmingMu.Lock()
	defer m.warmingMu.Unlock()
	delete(m.warming, nodeID)
}

func (m *Manager) pruneWarmingKnown(known map[string]struct{}) {
	m.warmingMu.Lock()
	defer m.warmingMu.Unlock()
	for nodeID := range m.warming {
		if _, ok := known[nodeID]; ok {
			delete(m.warming, nodeID)
		}
	}
}

func (m *Manager) reapTimedOutWarming(ctx context.Context, now time.Time) int {
	timeout := m.cfg.HeartbeatTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	m.warmingMu.Lock()
	type staleNode struct {
		id string
	}
	stale := make([]staleNode, 0)
	active := 0
	for nodeID, createdAt := range m.warming {
		if now.Sub(createdAt.UTC()) > timeout {
			stale = append(stale, staleNode{id: nodeID})
			delete(m.warming, nodeID)
			continue
		}
		active++
	}
	m.warmingMu.Unlock()

	for _, item := range stale {
		m.logger.Printf("pool manager reaping warming timeout: node_id=%s", item.id)
		if err := m.provider.DestroyNode(ctx, item.id); err != nil {
			m.logger.Printf("pool manager warming destroy failed: node_id=%s err=%v", item.id, err)
		}
	}
	return active
}
