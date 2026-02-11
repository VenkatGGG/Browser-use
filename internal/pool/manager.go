package pool

import "context"

type Manager struct {
	provider Provider
}

func NewManager(provider Provider) *Manager {
	return &Manager{provider: provider}
}

// Reconcile is a placeholder for the warm-pool control loop.
func (m *Manager) Reconcile(ctx context.Context, targetReady int) error {
	_ = ctx
	_ = targetReady
	return nil
}
