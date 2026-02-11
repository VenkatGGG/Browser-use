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
	ID            string    `json:"id"`
	Address       string    `json:"address"`
	Version       string    `json:"version"`
	State         NodeState `json:"state"`
	BootedAt      time.Time `json:"booted_at"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	LeasedUntil   time.Time `json:"leased_until,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
