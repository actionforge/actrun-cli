package agent

import (
	"sync"
	"time"
)

// NodeReporter tracks currently active nodes and debounces updates to the orchestrator.
// Nodes are kept in insertion order so the last element is the most recently started node.
type NodeReporter struct {
	client *Client
	jobID  string

	mu          sync.Mutex
	activeNodes []ActiveNode // ordered by start time, latest last
	dirty       bool
	done        chan struct{}
	closeOnce   sync.Once
}

func NewNodeReporter(client *Client, jobID string) *NodeReporter {
	r := &NodeReporter{
		client: client,
		jobID:  jobID,
		done:   make(chan struct{}),
	}
	go r.loop()
	return r
}

// OnNodeState is the callback passed to ExecutionState.NodeStateCallback.
func (r *NodeReporter) OnNodeState(nodeID, nodeName string, started bool) {
	r.mu.Lock()
	if started {
		r.activeNodes = append(r.activeNodes, ActiveNode{NodeID: nodeID, NodeName: nodeName})
	} else {
		for i, n := range r.activeNodes {
			if n.NodeID == nodeID {
				r.activeNodes = append(r.activeNodes[:i], r.activeNodes[i+1:]...)
				break
			}
		}
	}
	r.dirty = true
	r.mu.Unlock()
}

func (r *NodeReporter) loop() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.flush()
		case <-r.done:
			r.flush()
			return
		}
	}
}

func (r *NodeReporter) flush() {
	r.mu.Lock()
	if !r.dirty {
		r.mu.Unlock()
		return
	}
	r.dirty = false
	nodes := make([]ActiveNode, len(r.activeNodes))
	copy(nodes, r.activeNodes)
	r.mu.Unlock()

	_ = r.client.SubmitActiveNodes(r.jobID, nodes)
}

// Close stops the reporter and sends a final (empty) update.
// Safe to call multiple times.
func (r *NodeReporter) Close() {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.activeNodes = nil
		r.dirty = true
		r.mu.Unlock()
		close(r.done)
	})
}
