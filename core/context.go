package core

import (
	"context"
	"maps"
	"slices"
	"sync"

	"github.com/actionforge/actrun-cli/utils"

	"github.com/google/uuid"
)

type ContextVisit struct {
	Node     NodeBaseInterface `json:"-"`
	NodeID   string            `json:"node_id"`
	FullPath string            `json:"full_path"`

	// If true, the node was executed, otherwise it was visited.
	Execute bool `json:"execute"`
}

type CacheType int

const (
	// Cache type used for outputs of execution nodes
	Permanent CacheType = iota

	// Cache type used for outputs of data nodes
	Ephemeral
)

// ExecutionState is a structure whose main purpose is to provide the correct output values
// and environment variables requested by nodes that were executed in subsequent goroutines.
//
// Basically, it's a linear sequence of ids, each representing a goroutine in the execution stack
//
//							/-> AB ---> AB'		<--- An execution state
//	    ---> A ---> A ---> A'
//							\-> AC ---> AC'	    <--- Another execution state
//
// Each item in the graph above represents a node and the corresponding state id. The key (A) represents
// the main routine from which the execution began. After A' each goroutine gets its own state object with an additional
// id. Now AB can fetch data from AB and A, whereas AC' might fetch a different value from A'.
//
// An example is the 'Concurrent For' node where each iteration runs in a separate goroutine.
// Nodes within these new executions can fetch their respective iteration index they are associated with.
// Without this approach, all nodes in subsequent goroutines would fetch the same value, which is the last.
//
// Note that group nodes do *currently* NOT create a new execution state. I have added some more context why in `GroupNode`.
type ExecutionState struct {
	Graph *ActionGraph `json:"-"`
	// The hierarchy slice represents the full stack of execution states from root to current.
	Hierarchy []NodeBaseInterface `json:"-"`

	// Array of nodes that were visited during the current execution state.
	// Each subcontext has its own list of visited nodes as they are just stacked.
	Visited []ContextVisit `json:"visited"`

	// The parent execution state. `nil` for root.
	ParentExecution *ExecutionState `json:"-"`

	// The node that created this execution state. `nil` for root.
	CreatedBy NodeBaseInterface `json:"-"`

	ContextStackLock *sync.RWMutex      `json:"-"`
	Ctx              context.Context    `json:"-"`
	CtxCancel        context.CancelFunc `json:"-"`
	IsGitHubWorkflow bool               `json:"isGitHubWorkflow"`
	IsDebugSession   bool               `json:"isDebugMode"`
	GraphFile        string             `json:"graphFile"`

	Id      string            `json:"id"`
	Env     map[string]string `json:"env"`
	Inputs  map[string]any    `json:"inputs"`
	Secrets map[string]string `json:"secrets"`

	// this is the map of 'github.xyz' gh context variables, if provided
	GhContext map[string]any `json:"ghContext"`
	// this is the map of 'needs.xyz' gh context variables, if provided
	GhNeeds map[string]any `json:"ghNeeds"`
	// this is the matrix for the current job, if provided
	GhMatrix map[string]any `json:"ghMatrix"`

	OutputCacheLock      *sync.RWMutex  `json:"-"`
	DataOutputCache      map[string]any `json:"dataOutputCache"`
	ExecutionOutputCache map[string]any `json:"executionOutputCache"`

	DebugCallback DebugCallback `json:"-"`
}

type ExecutionStateOptions struct {
	IsGitHubWorkflow bool
	IsLiveSession    bool
	Env              map[string]string
	Secrets          map[string]string
	Inputs           map[string]string
	GraphName        string
	Ctx              context.Context
	DebugCallback    DebugCallback
}

func (c *ExecutionState) IsCancelled() bool {
	return c.Ctx != nil && c.Ctx.Err() != nil
}

func (c *ExecutionState) Cancel() {
	c.CtxCancel()
}

// PushNewExecutionState creates a new execution state and pushes it to the stack.
// Should be used right before a new goroutine is created and called.
//
//		newEc := ti.PushNewExecutionState()
//		err = n.Outputs.SetOutputValue(newEc, <output-id>, <output-value>)
//		if err != nil {
//		    return err
//		}
//		wg.Add(1)
//		go func() {
//	    	err := n.ExecBody.Execute(newEc)
//	        ...
//		}();
func (c *ExecutionState) PushNewExecutionState(parentNode NodeBaseInterface) *ExecutionState {

	c.ContextStackLock.Lock()
	defer c.ContextStackLock.Unlock()

	contextEnv := maps.Clone(c.Env)
	visited := slices.Clone(c.Visited)

	Context, Cancel := context.WithCancel(c.Ctx)

	newEc := &ExecutionState{
		Graph:           c.Graph,
		Hierarchy:       append(slices.Clone(c.Hierarchy), parentNode),
		ParentExecution: c,
		CreatedBy:       parentNode,

		ContextStackLock: &sync.RWMutex{},

		Ctx:              Context,
		CtxCancel:        Cancel,
		IsDebugSession:   c.IsDebugSession,
		IsGitHubWorkflow: c.IsGitHubWorkflow,
		GraphFile:        c.GraphFile,

		Id:      uuid.New().String(),
		Env:     contextEnv,
		Inputs:  c.Inputs,
		Secrets: c.Secrets,

		OutputCacheLock:      &sync.RWMutex{},
		DataOutputCache:      make(map[string]any),
		ExecutionOutputCache: make(map[string]any),

		Visited:       visited,
		DebugCallback: c.DebugCallback,
	}

	return newEc
}

func (c *ExecutionState) GetContextEnvironMapCopy() map[string]string {
	c.ContextStackLock.RLock()
	defer c.ContextStackLock.RUnlock()

	return maps.Clone(c.Env)
}

func (c *ExecutionState) GetDataFromOutputCache(nodeCacheId string, outputId string, ct CacheType) (any, bool) {
	c.ContextStackLock.RLock()
	defer c.ContextStackLock.RUnlock()

	contextStack := c

	for contextStack != nil {
		var (
			ds any
			ok bool
		)

		contextStack.OutputCacheLock.RLock()

		cacheId := nodeCacheId + ":" + outputId
		if ct == Permanent {
			ds, ok = contextStack.ExecutionOutputCache[cacheId]
		} else {
			ds, ok = contextStack.DataOutputCache[cacheId]
		}
		contextStack.OutputCacheLock.RUnlock()

		if ok {
			return ds, true
		}

		contextStack = contextStack.ParentExecution
	}
	return nil, false
}

func (c *ExecutionState) CacheDataOutput(nodeCacheId string, outputId string, value any, ct CacheType) {
	c.ContextStackLock.RLock()
	defer c.ContextStackLock.RUnlock()

	c.OutputCacheLock.Lock()
	defer c.OutputCacheLock.Unlock()

	cacheId := nodeCacheId + ":" + outputId
	if ct == Permanent {
		c.ExecutionOutputCache[cacheId] = value
	} else {
		c.DataOutputCache[cacheId] = value
	}
}

func (c *ExecutionState) EmptyDataOutputCache() {
	c.OutputCacheLock.Lock()
	defer c.OutputCacheLock.Unlock()

	c.DataOutputCache = make(map[string]any)
}

func (c *ExecutionState) PushNodeVisit(node NodeBaseInterface, execute bool) {
	if node == nil {
		panic("can't push nil node to visited stack")
	}

	if utils.GetLogLevel() == utils.LogLevelDebug {
		utils.LogOut.Debugf("PushNodeVisit: %s, execute: %t\n", node.GetId(), execute)
	}

	nodeVisit := ContextVisit{
		Node:     node,
		NodeID:   node.GetId(),
		FullPath: node.GetFullPath(),
		Execute:  execute,
	}

	c.ContextStackLock.Lock()
	c.Visited = append(c.Visited, nodeVisit)
	c.ContextStackLock.Unlock()

	if c.DebugCallback != nil {
		c.DebugCallback(c, nodeVisit)
	}
}

func (c *ExecutionState) PopNodeVisit() {
	c.ContextStackLock.Lock()
	defer c.ContextStackLock.Unlock()

	if len(c.Visited) > 0 {
		c.Visited = c.Visited[:len(c.Visited)-1]
	}
}

// SetContextEnvironMap sets the environment variables for the current and subsequent goroutines.
func (c *ExecutionState) SetContextEnvironMap(env map[string]string) {
	c.ContextStackLock.Lock()
	defer c.ContextStackLock.Unlock()

	c.Env = env
}
