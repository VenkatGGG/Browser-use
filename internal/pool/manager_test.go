package pool

import (
	"context"
	"slices"
	"testing"
	"time"
)

type fakeProvider struct {
	provisioned int
	destroyed   []string
}

func (f *fakeProvider) ProvisionNode(_ context.Context, _ ProvisionInput) (Node, error) {
	f.provisioned++
	return Node{
		ID:        "poolnode-test-" + time.Now().Format("150405.000000000"),
		Address:   "poolnode:9091",
		State:     NodeStateWarming,
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (f *fakeProvider) DestroyNode(_ context.Context, nodeID string) error {
	f.destroyed = append(f.destroyed, nodeID)
	return nil
}

func TestManagerReconcileProvisionsToTarget(t *testing.T) {
	registry := NewInMemoryRegistry()
	provider := &fakeProvider{}
	manager := NewManager(registry, provider, ManagerConfig{
		TargetReady:       2,
		HeartbeatTimeout:  30 * time.Second,
		ManagedNodePrefix: "poolnode-",
	}, nil)

	if err := manager.Reconcile(context.Background(), 2); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if provider.provisioned != 2 {
		t.Fatalf("expected 2 provisions, got %d", provider.provisioned)
	}
}

func TestManagerReconcileSkipsProvisionWhenReadyEnough(t *testing.T) {
	registry := NewInMemoryRegistry()
	_, _ = registry.Register(context.Background(), RegisterInput{
		NodeID:  "poolnode-ready-1",
		Address: "poolnode-ready-1:9091",
		Version: "dev",
	})
	_, _ = registry.Register(context.Background(), RegisterInput{
		NodeID:  "poolnode-ready-2",
		Address: "poolnode-ready-2:9091",
		Version: "dev",
	})

	provider := &fakeProvider{}
	manager := NewManager(registry, provider, ManagerConfig{
		TargetReady:       2,
		HeartbeatTimeout:  30 * time.Second,
		ManagedNodePrefix: "poolnode-",
	}, nil)

	if err := manager.Reconcile(context.Background(), 2); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if provider.provisioned != 0 {
		t.Fatalf("expected 0 provisions, got %d", provider.provisioned)
	}
}

func TestManagerReconcileReapsStaleManagedNode(t *testing.T) {
	registry := NewInMemoryRegistry()
	_, _ = registry.Register(context.Background(), RegisterInput{
		NodeID:  "poolnode-stale-1",
		Address: "poolnode-stale-1:9091",
		Version: "dev",
	})
	staleAt := time.Now().UTC().Add(-2 * time.Minute)
	_, _ = registry.Heartbeat(context.Background(), HeartbeatInput{
		NodeID: "poolnode-stale-1",
		State:  NodeStateReady,
		At:     staleAt,
	})

	provider := &fakeProvider{}
	manager := NewManager(registry, provider, ManagerConfig{
		TargetReady:       0,
		HeartbeatTimeout:  30 * time.Second,
		ManagedNodePrefix: "poolnode-",
	}, nil)

	if err := manager.Reconcile(context.Background(), 0); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(provider.destroyed) != 1 || provider.destroyed[0] != "poolnode-stale-1" {
		t.Fatalf("expected stale node to be destroyed, got %#v", provider.destroyed)
	}

	nodes, err := registry.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected one node, got %d", len(nodes))
	}
	if nodes[0].State != NodeStateDead {
		t.Fatalf("expected node state dead, got %s", nodes[0].State)
	}
}

func TestManagerReconcileScalesDownManagedReadyToTarget(t *testing.T) {
	registry := NewInMemoryRegistry()
	base := time.Now().UTC().Add(-10 * time.Minute)

	_, _ = registry.Register(context.Background(), RegisterInput{
		NodeID:  "poolnode-ready-oldest",
		Address: "poolnode-ready-oldest:9091",
		Version: "dev",
		Booted:  base,
	})
	_, _ = registry.Register(context.Background(), RegisterInput{
		NodeID:  "poolnode-ready-newer",
		Address: "poolnode-ready-newer:9091",
		Version: "dev",
		Booted:  base.Add(3 * time.Minute),
	})
	_, _ = registry.Register(context.Background(), RegisterInput{
		NodeID:  "poolnode-ready-newest",
		Address: "poolnode-ready-newest:9091",
		Version: "dev",
		Booted:  base.Add(6 * time.Minute),
	})

	provider := &fakeProvider{}
	manager := NewManager(registry, provider, ManagerConfig{
		TargetReady:       2,
		HeartbeatTimeout:  30 * time.Second,
		ManagedNodePrefix: "poolnode-",
	}, nil)

	if err := manager.Reconcile(context.Background(), 2); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if !slices.Contains(provider.destroyed, "poolnode-ready-oldest") {
		t.Fatalf("expected oldest ready node to be destroyed, got %#v", provider.destroyed)
	}

	nodes, err := registry.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	ready := 0
	for _, node := range nodes {
		if node.State == NodeStateReady {
			ready++
		}
	}
	if ready != 2 {
		t.Fatalf("expected 2 ready nodes after scale down, got %d", ready)
	}
}

func TestManagerReconcileDoesNotDestroyUnmanagedNodes(t *testing.T) {
	registry := NewInMemoryRegistry()
	_, _ = registry.Register(context.Background(), RegisterInput{
		NodeID:  "external-ready-1",
		Address: "external-ready-1:9091",
		Version: "dev",
	})
	_, _ = registry.Register(context.Background(), RegisterInput{
		NodeID:  "external-ready-2",
		Address: "external-ready-2:9091",
		Version: "dev",
	})

	provider := &fakeProvider{}
	manager := NewManager(registry, provider, ManagerConfig{
		TargetReady:       1,
		HeartbeatTimeout:  30 * time.Second,
		ManagedNodePrefix: "poolnode-",
	}, nil)

	if err := manager.Reconcile(context.Background(), 1); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(provider.destroyed) != 0 {
		t.Fatalf("expected no destroys for unmanaged nodes, got %#v", provider.destroyed)
	}
}
