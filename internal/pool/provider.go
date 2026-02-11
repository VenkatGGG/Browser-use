package pool

import "context"

type ProvisionInput struct {
	Labels map[string]string
}

type Provider interface {
	ProvisionNode(ctx context.Context, input ProvisionInput) (Node, error)
	DestroyNode(ctx context.Context, nodeID string) error
}
