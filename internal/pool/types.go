package pool

import "time"

type NodeState string

const (
	NodeStateWarming  NodeState = "warming"
	NodeStateReady    NodeState = "ready"
	NodeStateLeased   NodeState = "leased"
	NodeStateDraining NodeState = "draining"
	NodeStateDead     NodeState = "dead"
)

type Node struct {
	ID          string
	Address     string
	State       NodeState
	LeasedUntil time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
