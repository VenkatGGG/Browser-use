package pool

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrNodeNotFound = errors.New("node not found")

type RegisterInput struct {
	NodeID  string
	Address string
	Version string
	Booted  time.Time
}

type HeartbeatInput struct {
	NodeID string
	State  NodeState
	At     time.Time
}

type Registry interface {
	Register(ctx context.Context, input RegisterInput) (Node, error)
	Heartbeat(ctx context.Context, input HeartbeatInput) (Node, error)
	List(ctx context.Context) ([]Node, error)
}

type InMemoryRegistry struct {
	mu    sync.RWMutex
	nodes map[string]Node
}

func NewInMemoryRegistry() *InMemoryRegistry {
	return &InMemoryRegistry{
		nodes: make(map[string]Node),
	}
}

func (r *InMemoryRegistry) Register(_ context.Context, input RegisterInput) (Node, error) {
	nodeID := strings.TrimSpace(input.NodeID)
	if nodeID == "" {
		return Node{}, errors.New("node_id is required")
	}
	address := strings.TrimSpace(input.Address)
	if address == "" {
		return Node{}, errors.New("address is required")
	}

	now := time.Now().UTC()
	bootedAt := input.Booted.UTC()
	if bootedAt.IsZero() {
		bootedAt = now
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.nodes[nodeID]
	if !ok {
		existing = Node{
			ID:        nodeID,
			CreatedAt: now,
		}
	}

	existing.Address = address
	existing.Version = strings.TrimSpace(input.Version)
	existing.BootedAt = bootedAt
	existing.State = NodeStateReady
	existing.LastHeartbeat = now
	existing.UpdatedAt = now
	r.nodes[nodeID] = existing

	return existing, nil
}

func (r *InMemoryRegistry) Heartbeat(_ context.Context, input HeartbeatInput) (Node, error) {
	nodeID := strings.TrimSpace(input.NodeID)
	if nodeID == "" {
		return Node{}, errors.New("node_id is required")
	}

	state := input.State
	if state == "" {
		state = NodeStateReady
	}

	at := input.At.UTC()
	if at.IsZero() {
		at = time.Now().UTC()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.nodes[nodeID]
	if !ok {
		return Node{}, ErrNodeNotFound
	}

	existing.State = state
	existing.LastHeartbeat = at
	existing.UpdatedAt = at
	r.nodes[nodeID] = existing

	return existing, nil
}

func (r *InMemoryRegistry) List(_ context.Context) ([]Node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nodes := make([]Node, 0, len(r.nodes))
	for _, node := range r.nodes {
		nodes = append(nodes, node)
	}

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].CreatedAt.Before(nodes[j].CreatedAt)
	})

	return nodes, nil
}

func ParseNodeState(value string) (NodeState, error) {
	raw := strings.TrimSpace(strings.ToLower(value))
	if raw == "" {
		return NodeStateReady, nil
	}
	state := NodeState(raw)
	switch state {
	case NodeStateWarming, NodeStateReady, NodeStateLeased, NodeStateDraining, NodeStateDead:
		return state, nil
	default:
		return "", errors.New("invalid node state")
	}
}
