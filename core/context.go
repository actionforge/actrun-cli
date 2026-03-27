package core

import (
	"context"
	"maps"
	"slices"
	"sync"

	"github.com/actionforge/actrun-cli/utils"

	"github.com/google/uuid"
)

const MAX_STEP_CACHE_ITEM_SIZE = 64 * 1024 // 64KB

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

type StepCacheEntry struct {
	Conclusion string         `json:"conclusion"`
	Outputs    map[string]any `json:"outputs"`
}

// HierarchicalMap is a memory-efficient map that chains to parent contexts.
// Each context only stores its own entries; lookups traverse the parent chain.
type HierarchicalMap[K comparable, V any] struct {
	data   map[K]V
	parent *HierarchicalMap[K, V]
}

// NewHierarchicalMap creates a new map, optionally chained to a parent.
func NewHierarchicalMap[K comparable, V any](parent *HierarchicalMap[K, V]) *HierarchicalMap[K, V] {
	return &HierarchicalMap[K, V]{
		data:   make(map[K]V),
		parent: parent,
	}
}

// Get retrieves a value by key, traversing up the parent chain if not found locally.
func (m *HierarchicalMap[K, V]) Get(key K) (V, bool) {
	if val, ok := m.data[key]; ok {
		return val, true
	}
	if m.parent != nil {
		return m.parent.Get(key)
	}
	var zero V
	return zero, false
}

// GetLocal retrieves a value only from local data, not traversing parents.
func (m *HierarchicalMap[K, V]) GetLocal(key K) (V, bool) {
	val, ok := m.data[key]
	return val, ok
}

// Set stores a value in the current context only (does not affect parent).
func (m *HierarchicalMap[K, V]) Set(key K, value V) {
	m.data[key] = value
}

// All returns all entries merged from the entire chain (child entries override parent).
func (m *HierarchicalMap[K, V]) All() map[K]V {
	result := make(map[K]V)
	m.collectAll(result)
	return result
}

func (m *HierarchicalMap[K, V]) collectAll(result map[K]V) {
	if m.parent != nil {
		m.parent.collectAll(result)
	}
	maps.Copy(result, m.data)
}

// StepCache is a type alias for the step output cache.
type StepCache = HierarchicalMap[string, *StepCacheEntry]

// NewStepCache creates a new step cache, optionally chained to a parent.
func NewStepCache(parent *StepCache) *StepCache {
	return NewHierarchicalMap(parent)
}

// GetOrCreateStepEntry retrieves an existing local entry or creates a new one.
// Only checks local cache to avoid races on shared parent entries.
func GetOrCreateStepEntry(cache *StepCache, key string) *StepCacheEntry {
	if entry, ok := cache.GetLocal(key); ok {
		return entry
	}
	entry := &StepCacheEntry{Outputs: make(map[string]any)}
	cache.Set(key, entry)
	return entry
}

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
	StepCache            *StepCache     `json:"stepCache"`

	PostSteps     *PostStepQueue `json:"-"`
	JobConclusion string         `json:"jobConclusion"`

	DebugCallback DebugCallback `json:"-"`

	// NodeStateCallback is invoked when a node starts or finishes execution.
	// The started parameter is true when the node begins executing, false when done.
	NodeStateCallback func(nodeID, nodeName string, started bool) `json:"-"`

	// PendingConcurrencyLocks tracks concurrency locks that are held during
	// ExecuteImpl. The key is node id → *sync.Mutex. Released when the
	// node calls Execute to dispatch downstream node, or as a fallback when
	// ExecuteImpl returns without any dispatching
	PendingConcurrencyLocks *sync.Map `json:"-"`
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

		GhContext: c.GhContext,
		GhNeeds:   c.GhNeeds,
		GhMatrix:  c.GhMatrix,

		OutputCacheLock:      &sync.RWMutex{},
		DataOutputCache:      make(map[string]any),
		ExecutionOutputCache: make(map[string]any),
		StepCache:            NewStepCache(c.StepCache),

		PostSteps:     c.PostSteps,
		JobConclusion: c.JobConclusion,

		Visited:              visited,
		DebugCallback:        c.DebugCallback,
		NodeStateCallback:    c.NodeStateCallback,
		PendingConcurrencyLocks: &sync.Map{},
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

func (c *ExecutionState) CacheDataOutput(node NodeBaseInterface, outputId string, value any, outputType string, ct CacheType) {
	c.ContextStackLock.RLock()
	defer c.ContextStackLock.RUnlock()

	c.OutputCacheLock.Lock()
	defer c.OutputCacheLock.Unlock()

	cacheId := node.GetCacheId() + ":" + outputId
	if ct == Permanent {
		c.ExecutionOutputCache[cacheId] = value
	} else {
		c.DataOutputCache[cacheId] = value
	}

	// Only cache primitive types under 64KB
	if isPrimitiveType(outputType) && isUnderCacheLimit(value) {
		stepEntry := GetOrCreateStepEntry(c.StepCache, node.GetId())
		stepEntry.Outputs[outputId] = value
	}
}

// isPrimitiveType returns true if the output type is a primitive type that should be cached.
func isPrimitiveType(outputType string) bool {
	switch outputType {
	case "string", "number", "bool":
		return true
	default:
		return false
	}
}

// isUnderCacheLimit checks if a string value is under the cache size limit.
// Non-string primitives always return true as they are small.
func isUnderCacheLimit(value any) bool {
	if s, ok := value.(string); ok {
		return len(s) <= MAX_STEP_CACHE_ITEM_SIZE
	}
	return true
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
