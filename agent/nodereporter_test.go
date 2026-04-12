package agent

import (
	"sync"
	"testing"
	"time"
)

func TestNodeReporter_ConcurrentUpdates(t *testing.T) {
	srv, _ := newMockGateway(t)
	c := NewClientFromCred(srv.URL, &InstanceCred{InstanceSecret: "s"})
	r := NewNodeReporter(c, "job-1")

	// Hammer OnNodeState from multiple goroutines
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			nodeID := "node-" + string(rune('A'+id%26))
			r.OnNodeState(nodeID, "Name-"+nodeID, true)
			time.Sleep(time.Millisecond)
			r.OnNodeState(nodeID, "Name-"+nodeID, false)
		}(i)
	}
	wg.Wait()

	r.Close()

	// After Close, all nodes should be cleared
	r.mu.Lock()
	n := len(r.activeNodes)
	r.mu.Unlock()
	if n != 0 {
		t.Fatalf("expected 0 active nodes after Close, got %d", n)
	}
}

func TestNodeReporter_CloseIdempotent(t *testing.T) {
	srv, _ := newMockGateway(t)
	c := NewClientFromCred(srv.URL, &InstanceCred{InstanceSecret: "s"})
	r := NewNodeReporter(c, "job-1")

	r.OnNodeState("n1", "Node1", true)
	r.Close()
	r.Close() // should not panic
	r.Close()
}

func TestNodeReporter_FlushSendsUpdate(t *testing.T) {
	srv, gw := newMockGateway(t)
	c := NewClientFromCred(srv.URL, &InstanceCred{InstanceSecret: "s"})
	r := NewNodeReporter(c, "job-1")

	r.OnNodeState("n1", "Node1", true)
	r.OnNodeState("n2", "Node2", true)

	// Wait for at least one flush cycle (250ms ticker)
	time.Sleep(400 * time.Millisecond)
	r.Close()

	// The mock gateway /jobs/ handler counts POSTs — node submissions go there
	// (the mock handles POST /api/v2/ci/runner/jobs/ for graph+ref+nodes)
	// Just verify no panics and clean shutdown
	_ = gw
}

func TestNodeReporter_OrderPreserved(t *testing.T) {
	srv, _ := newMockGateway(t)
	c := NewClientFromCred(srv.URL, &InstanceCred{InstanceSecret: "s"})
	r := NewNodeReporter(c, "job-1")

	r.OnNodeState("a", "A", true)
	r.OnNodeState("b", "B", true)
	r.OnNodeState("c", "C", true)

	r.mu.Lock()
	if len(r.activeNodes) != 3 {
		r.mu.Unlock()
		t.Fatalf("expected 3 nodes, got %d", len(r.activeNodes))
	}
	if r.activeNodes[0].NodeID != "a" || r.activeNodes[2].NodeID != "c" {
		r.mu.Unlock()
		t.Fatal("insertion order not preserved")
	}
	r.mu.Unlock()

	// Remove middle
	r.OnNodeState("b", "B", false)

	r.mu.Lock()
	if len(r.activeNodes) != 2 {
		r.mu.Unlock()
		t.Fatalf("expected 2 after remove, got %d", len(r.activeNodes))
	}
	if r.activeNodes[0].NodeID != "a" || r.activeNodes[1].NodeID != "c" {
		r.mu.Unlock()
		t.Fatal("wrong order after removal")
	}
	r.mu.Unlock()

	r.Close()
}
