package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/actionforge/actrun-cli/utils"
	"github.com/google/uuid"

	"go.yaml.in/yaml/v4"
)

type DebugCallback func(ec *ExecutionState, nodeVisit ContextVisit)

type RunOpts struct {
	ConfigFile      string
	OverrideSecrets map[string]string
	OverrideInputs  map[string]any
	OverrideEnv     map[string]string
	Args            []string
}

type ActionGraph struct {
	Nodes   map[string]NodeBaseInterface
	Inputs  map[InputId]InputDefinition   `yaml:"inputs" json:"inputs" bson:"inputs"`
	Outputs map[OutputId]OutputDefinition `yaml:"outputs" json:"outputs" bson:"outputs"`

	Entry string
}

func (ag *ActionGraph) AddNode(nodeId string, node NodeBaseInterface) {
	ag.Nodes[nodeId] = node
}

func (ag *ActionGraph) FindNode(nodeId string) (NodeBaseInterface, bool) {
	node, exists := ag.Nodes[nodeId]
	if !exists {
		return nil, false
	}
	return node, true
}

func (ag *ActionGraph) GetNodes() map[string]NodeBaseInterface {
	return ag.Nodes
}

func (ag *ActionGraph) SetEntry(entryName string) {
	ag.Entry = entryName
}

func (ag *ActionGraph) GetEntry() (NodeEntryInterface, error) {
	node, exists := ag.Nodes[ag.Entry]
	if !exists {
		return nil, fmt.Errorf("entry '%s' not found", ag.Entry)
	}

	execNode, ok := node.(NodeEntryInterface)
	if !ok {
		return nil, fmt.Errorf("entry '%s' is not an entry node", ag.Entry)
	}

	return execNode, nil
}

func NewActionGraph() ActionGraph {
	return ActionGraph{
		Nodes: make(map[string]NodeBaseInterface),
	}
}

// helper to handle error collection
func collectOrReturn(err error, validate bool, errList *[]error) error {
	if err == nil {
		return nil
	}
	if validate {
		*errList = append(*errList, err)
		return nil
	}
	return err
}

func LoadEntry(ag *ActionGraph, nodesYaml map[string]any, validate bool, errs *[]error) error {
	entryAny, exists := nodesYaml["entry"]
	if !exists {
		return collectOrReturn(CreateErr(nil, nil, "entry is missing"), validate, errs)
	}

	entry, ok := entryAny.(string)
	if !ok {
		return collectOrReturn(CreateErr(nil, nil, "entry is not a string"), validate, errs)
	}

	ag.SetEntry(entry)
	return nil
}

type trackedValue[T any] struct {
	Key        string
	Value      T
	Source     string
	Category   string
	IsExplicit bool
}
type valueMap[T any] struct {
	data     map[string]trackedValue[T]
	category string
}

func newValueMap[T any](category string) valueMap[T] {
	return valueMap[T]{
		data:     make(map[string]trackedValue[T]),
		category: category,
	}
}

func (m valueMap[T]) set(source map[string]T, sourceName string, explicit bool, hideValue bool) {
	keys := make([]string, 0, len(m.data))
	for k := range source {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := source[k]
		m.setSingle(k, v, sourceName, explicit, hideValue)
	}
}

func (m valueMap[T]) setSingle(key string, value T, sourceName string, explicit bool, hideValue bool) {
	newVal := formatValue(key, value, hideValue)

	if !IsTestE2eRunning() {
		if existing, exists := m.data[key]; exists {
			oldVal := formatValue(existing.Key, existing.Value, hideValue)
			utils.LogOut.Debugf("overwriting %s '%s=%s' (from %s) -> '%s' (from %s)\n",
				m.category, key, oldVal, existing.Source, newVal, sourceName)
		} else {
			utils.LogOut.Debugf("setting %s '%s=%s' (from %s)\n",
				m.category, key, newVal, sourceName)
		}
	}

	m.data[key] = trackedValue[T]{
		Key:        key,
		Value:      value,
		Source:     sourceName,
		Category:   m.category,
		IsExplicit: explicit,
	}
}

func (m valueMap[T]) toSimpleMap() map[string]T {
	res := make(map[string]T)
	for k, v := range m.data {
		res[k] = v.Value
	}
	return res
}

func (m valueMap[T]) toSimpleMapWithLowercaseKeys() map[string]T {
	res := make(map[string]T)
	for k, v := range m.data {
		res[strings.ToLower(k)] = v.Value
	}
	return res
}

func printExplicit[T any](m valueMap[T], hideValue bool) {
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := m.data[k]

		// we dont flood logs with shell values, there can be too many of them
		if !v.IsExplicit || v.Source == utils.ORIGIN_ENV_SHELL {
			continue
		}

		displayValue := formatValue(v.Key, v.Value, hideValue)
		utils.LogOut.Debugf("final %s '%s=%s' set by %s\n",
			v.Category, k, displayValue, v.Source)
	}
}

func formatValue[T any](key string, val T, hide bool) string {
	str := fmt.Sprintf("%v", val)

	if str == "" {
		return "(empty)"
	}

	filterWords := []string{"key", "access", "secret", "token", "password"}
	lowerStr := strings.ToLower(key)
	for _, word := range filterWords {
		if strings.Contains(lowerStr, word) {
			hide = true
			break
		}
	}

	if hide {
		return strings.Repeat("*", len(str))
	}

	const maxLen = 256
	if len(str) > maxLen {
		return str[:maxLen] + "..."
	}

	return str
}

func NewExecutionState(
	ctx context.Context,
	graph *ActionGraph,
	graphName string,
	isGitHubWorkflow bool,
	debugCb DebugCallback,
	env map[string]string,
	inputs map[string]any,
	secrets map[string]string,
	ghContext map[string]any,
	ghMatrix map[string]any,
	ghNeeds map[string]any,
) *ExecutionState {
	ctx, cancel := context.WithCancel(ctx)

	return &ExecutionState{
		Graph:            graph,
		Hierarchy:        make([]NodeBaseInterface, 0),
		ContextStackLock: &sync.RWMutex{},
		OutputCacheLock:  &sync.RWMutex{},

		IsDebugSession: debugCb != nil,
		DebugCallback:  debugCb,

		IsGitHubWorkflow: isGitHubWorkflow,
		Ctx:              ctx,
		CtxCancel:        cancel,
		GraphFile:        graphName,
		Id:               uuid.New().String(),

		Env:     env,
		Inputs:  inputs,
		Secrets: secrets,

		GhContext: ghContext,
		GhMatrix:  ghMatrix,
		GhNeeds:   ghNeeds,

		DataOutputCache:      make(map[string]any),
		ExecutionOutputCache: make(map[string]any),
	}
}

func RunGraph(ctx context.Context, graphName string, graphContent []byte, opts RunOpts, debugCb DebugCallback) error {
	graphYaml := make(map[string]any)
	if err := yaml.Unmarshal(graphContent, &graphYaml); err != nil {
		return CreateErr(nil, err, "failed to load yaml")
	}

	ag, errs := LoadGraph(graphYaml, nil, "", false)
	if len(errs) > 0 {
		return CreateErr(nil, errs[0], "failed to load graph")
	}

	entry, err := ag.GetEntry()
	if err != nil {
		return CreateErr(nil, err, "failed to load graph")
	}

	entryNode, isBaseNode := entry.(NodeBaseInterface)
	isGitHubWorkflow := os.Getenv("GITHUB_ACTIONS") == "true" || (isBaseNode && entryNode.GetNodeTypeId() == "core/gh-start@v1")

	// Initialize trackers with their respective categories
	envTracker := newValueMap[string]("env")
	inputTracker := newValueMap[any]("input")
	secretTracker := newValueMap[string]("secret")
	matrixTracker := newValueMap[any]("matrix")
	needsTracker := newValueMap[any]("needs")

	// Priority 1 (Lowest): Config file
	if opts.ConfigFile != "" {
		if _, err := os.Stat(opts.ConfigFile); err == nil {
			localConfig, err := utils.LoadConfig(opts.ConfigFile)
			if err == nil {
				configName := filepath.Base(opts.ConfigFile)
				envTracker.set(localConfig.Env, configName, true, false)
				inputTracker.set(localConfig.Inputs, configName, true, false)
				secretTracker.set(localConfig.Secrets, configName, true, true)
			}
		}
	}

	rawEnv := utils.GetAllEnvMapCopy()

	// normalize all inputs/secrets with ACT_* iif we're in GitHub
	if isGitHubWorkflow {
		prefixedRawEnv := make(map[string]utils.EnvKV)
		for k, v := range rawEnv {
			prefixedKey := k
			if strings.HasPrefix(k, "INPUT_") {
				prefixedKey = "ACT_" + k
			}
			prefixedRawEnv[prefixedKey] = v
		}
		rawEnv = prefixedRawEnv
	}

	// prio 2: bulk json from env (has a lower precedence than individual inputs/secrets)
	for k, v := range rawEnv {
		source := "shell"
		if v.DotEnvFile {
			source = ".env"
		}

		switch k {
		case "ACT_INPUT_INPUTS":
			if m, err := decodeJsonFromEnvValue[any](v.Value); err == nil {
				inputTracker.set(m, fmt.Sprintf("%s (%s)", source, k), true, false)
			}
		case "ACT_INPUT_SECRETS":
			if m, err := decodeJsonFromEnvValue[string](v.Value); err == nil {
				secretTracker.set(m, fmt.Sprintf("%s (%s)", source, k), true, true)
			}
		}
	}

	// prio 3: individual env vars & GitHub contexts
	for k, v := range rawEnv {
		source := "shell"
		if v.DotEnvFile {
			source = ".env"
		}

		switch {
		// Skip bulk values processed in Priority 2
		case k == "ACT_INPUT_INPUTS" || k == "ACT_INPUT_SECRETS":
			continue

		// individual inputs/secrets (High precedence: will overwrite bulk if key matches)
		case strings.HasPrefix(k, "ACT_INPUT_INPUT_"):
			key := strings.TrimPrefix(k, "ACT_INPUT_INPUT_")
			inputTracker.setSingle(key, v.Value, fmt.Sprintf("%s (%s)", source, k), true, false)

		case strings.HasPrefix(k, "ACT_INPUT_SECRET_"):
			key := strings.TrimPrefix(k, "ACT_INPUT_SECRET_")
			secretTracker.setSingle(key, v.Value, fmt.Sprintf("%s (%s)", source, k), true, true)

		// GitHub specifics
		case isGitHubWorkflow && k == "ACT_INPUT_MATRIX":
			if m, err := decodeJsonFromEnvValue[any](v.Value); err == nil {
				matrixTracker.set(m, source, true, true)
			}
		case isGitHubWorkflow && k == "ACT_INPUT_NEEDS":
			if m, err := decodeJsonFromEnvValue[any](v.Value); err == nil {
				needsTracker.set(m, source, true, true)
			}
		case isGitHubWorkflow && k == "ACT_INPUT_TOKEN":
			secretTracker.setSingle("GITHUB_TOKEN", v.Value, source, true, true)

		default:
			envTracker.setSingle(k, v.Value, source, v.DotEnvFile, false)
		}
	}

	// prio 4 (highest): explicit overrides (eg from the web app)
	envTracker.set(opts.OverrideEnv, "override", true, false)
	inputTracker.set(opts.OverrideInputs, "override", true, false)
	secretTracker.set(opts.OverrideSecrets, "override", true, true)

	finalEnv := envTracker.toSimpleMap()
	finalInputs := inputTracker.toSimpleMapWithLowercaseKeys()
	finalSecrets := secretTracker.toSimpleMap()

	// some debug printing the final values
	if !IsTestE2eRunning() {
		printExplicit(inputTracker, false)
		printExplicit(secretTracker, true)
		printExplicit(matrixTracker, true)
		printExplicit(needsTracker, true)
		printExplicit(envTracker, false)
	}

	// construct the `github` context
	var ghContext map[string]any
	var errGh error
	if isGitHubWorkflow {
		ghContext, errGh = LoadGitHubContext(finalEnv, finalInputs, finalSecrets)
		if errGh != nil {
			return CreateErr(nil, errGh, "failed to load github context")
		}
	}

	c := NewExecutionState(
		ctx,
		&ag,
		graphName,
		isGitHubWorkflow,
		debugCb,
		finalEnv,
		finalInputs,
		finalSecrets,
		ghContext,
		matrixTracker.toSimpleMap(),
		needsTracker.toSimpleMap(),
	)

	if isBaseNode {
		c.PushNodeVisit(entryNode, true)
	}

	return entry.ExecuteEntry(c, nil, opts.Args)
}

func LoadGraph(graphYaml map[string]any, parent NodeBaseInterface, parentId string, validate bool) (ActionGraph, []error) {

	var (
		collectedErrors []error
		err             error
	)

	ag := NewActionGraph()

	ag.Inputs, err = LoadGraphInputs(graphYaml)
	if err != nil {
		if !validate {
			return ActionGraph{}, []error{err}
		}
		collectedErrors = append(collectedErrors, err)
	}

	ag.Outputs, err = LoadGraphOutputs(graphYaml)
	if err != nil {
		if !validate {
			return ActionGraph{}, []error{err}
		}
		collectedErrors = append(collectedErrors, err)
	}

	err = LoadNodes(&ag, parent, parentId, graphYaml, validate, &collectedErrors)
	if err != nil && !validate {
		return ActionGraph{}, []error{err}
	}

	err = LoadExecutions(&ag, graphYaml, validate, &collectedErrors)
	if err != nil && !validate {
		return ActionGraph{}, []error{err}
	}

	err = LoadConnections(&ag, graphYaml, parent, validate, &collectedErrors)
	if err != nil && !validate {
		return ActionGraph{}, []error{err}
	}

	err = LoadEntry(&ag, graphYaml, validate, &collectedErrors)
	if err != nil && !validate {
		return ActionGraph{}, []error{err}
	}

	return ag, collectedErrors
}

func LoadGraphInputs(graphYaml map[string]any) (map[InputId]InputDefinition, error) {
	inputs, ok := graphYaml["inputs"]
	if !ok {
		return nil, nil
	}

	idefs := make(map[InputId]InputDefinition)
	for k, v := range inputs.(map[string]any) {
		idef, err := anyToPortDefinition[InputDefinition](v)
		if err != nil {
			return nil, err
		}

		idefs[InputId(k)] = idef
	}

	return idefs, nil
}

func LoadGraphOutputs(graphYaml map[string]any) (map[OutputId]OutputDefinition, error) {
	outputs, ok := graphYaml["outputs"]
	if !ok {
		return nil, nil
	}

	odefs := make(map[OutputId]OutputDefinition)
	for k, v := range outputs.(map[string]any) {
		odef, err := anyToPortDefinition[OutputDefinition](v)
		if err != nil {
			return nil, err
		}

		odefs[OutputId(k)] = odef
	}

	return odefs, nil
}

func anyToPortDefinition[T any](o any) (T, error) {
	var (
		tmp bytes.Buffer
		ret T
	)
	err := yaml.NewEncoder(&tmp).Encode(o)
	if err != nil {
		return ret, err
	}

	err = yaml.NewDecoder(&tmp).Decode(&ret)
	if err != nil {
		return ret, err
	}
	return ret, err
}

func LoadNodes(ag *ActionGraph, parent NodeBaseInterface, parentId string, nodesYaml map[string]any, validate bool, errs *[]error) error {
	nodesList, err := utils.GetTypedPropertyByPath[[]any](nodesYaml, "nodes")
	if err != nil {
		return collectOrReturn(err, validate, errs)
	}

	for _, nodeData := range nodesList {
		n, id, err := LoadNode(parent, parentId, nodeData, validate, errs)
		if err != nil {
			return err
		}

		// Only add to the graph if a valid node instance and ID were returned.
		// If n is nil, it means the node was invalid (e.g. missing ID, missing Type,
		// or factory failure), and errors have already been collected.
		if n != nil {
			ag.AddNode(id, n)
		}
	}
	return nil
}

func LoadNode(parent NodeBaseInterface, parentId string, nodeData any, validate bool, errs *[]error) (NodeBaseInterface, string, error) {
	nodeI, ok := nodeData.(map[string]any)
	if !ok {
		err := CreateErr(nil, nil, "node is not a map")
		if collectOrReturn(err, validate, errs) != nil {
			return nil, "", err
		}
		return nil, "", nil
	}

	// 1. Check ID
	// We attempt to get the ID. If it fails, we record the error but CONTINUE
	// processing (if validating) to check Type, Inputs, and Outputs.
	id, idErr := utils.GetTypedPropertyByPath[string](nodeI, "id")
	if idErr != nil {
		if err := collectOrReturn(idErr, validate, errs); err != nil {
			return nil, "", err
		}
	}

	// 2. Check Type
	// If Type is missing, loading "makes no sense" as we cannot select a factory.
	// We must early out here.
	nodeType, typeErr := utils.GetTypedPropertyByPath[string](nodeI, "type")
	if typeErr != nil {
		if err := collectOrReturn(typeErr, validate, errs); err != nil {
			return nil, "", err
		}
		return nil, "", nil
	}

	var (
		n           NodeBaseInterface
		factoryErrs []error
	)

	var fullPath string
	if parentId == "" {
		fullPath = id
	} else {
		fullPath = parentId + "/" + id
	}
	if strings.HasPrefix(nodeType, "github.com/") {
		n, factoryErrs = NewGhActionNode(nodeType, parent, fullPath, validate)
	} else {
		n, factoryErrs = NewNodeInstance(nodeType, parent, fullPath, nodeI, validate)
	}

	if len(factoryErrs) > 0 {
		if !validate {
			// Early out on first error if not validating
			return nil, "", factoryErrs[0]
		}
		// Collect errors and proceed IF we have a valid node instance 'n'
		*errs = append(*errs, factoryErrs...)
	}

	// If the factory failed to produce a node instance completely (n is nil),
	// we cannot proceed to check inputs/outputs.
	if n == nil {
		return nil, "", nil
	}

	if idErr == nil {
		n.SetId(id)
		if parentId != "" {
			n.SetFullPath(parentId + "/" + id)
		} else {
			n.SetFullPath(id)
		}
	}

	// We continue to check inputs/outputs even if factoryErrs occurred,
	// provided 'n' exists.
	inputErr := LoadInputValues(n, nodeI, validate, errs)
	if inputErr != nil && !validate {
		return nil, "", inputErr
	}

	// Validate Outputs
	outputErr := LoadOutputValues(n, nodeI, validate, errs)
	if outputErr != nil && !validate {
		return nil, "", outputErr
	}

	outputNode, ok := n.(HasOutputsInterface)
	if ok {
		outputNode.SetOwner(n)
	}

	// If the ID was missing (idErr != nil), we cannot return this node to be
	// added to the ActionGraph map (as the key is missing), even though we
	// successfully validated its internals.
	if idErr != nil {
		return nil, "", nil
	}

	return n, id, nil
}

func LoadInputValues(node NodeBaseInterface, nodeI map[string]any, validate bool, errs *[]error) error {
	inputs, hasInputs := node.(HasInputsInterface)
	inputValues, err := utils.GetTypedPropertyByPath[map[string]any](nodeI, "inputs")
	if err != nil {
		if errors.Is(err, &utils.ErrPropertyNotFound{}) {
			return nil
		}
		return collectOrReturn(err, validate, errs)
	}
	if !hasInputs {
		return collectOrReturn(CreateErr(nil, nil, "dst node '%s' (%s) does not have inputs but inputs are defined", node.GetName(), node.GetId()), validate, errs)
	}

	type subInput struct {
		PortId    string
		PortIndex int
	}

	subInputs := map[string][]subInput{}

	for portId, inputValue := range inputValues {
		groupInputId, portIndex, isIndexPort := IsValidIndexPortId(portId)
		if isIndexPort {
			_, _, ok := inputs.InputDefByPortId(groupInputId)
			if !ok {
				err := CreateErr(nil, nil, "dst node '%s' (%s) has no array input '%s'", node.GetName(), node.GetId(), groupInputId)
				if collectOrReturn(err, validate, errs) != nil {
					return err
				}
				continue
			}

			subInputs[groupInputId] = append(subInputs[groupInputId], subInput{
				PortId:    portId,
				PortIndex: portIndex,
			})
		}

		err = inputs.SetInputValue(InputId(portId), inputValue)
		if err != nil {
			if collectOrReturn(err, validate, errs) != nil {
				return err
			}
			continue
		}
	}

	for _, subInputs := range subInputs {
		sort.Slice(subInputs, func(i, j int) bool {
			return subInputs[i].PortIndex < subInputs[j].PortIndex
		})
	}

	for groupInputId, subInputs := range subInputs {
		for _, subInput := range subInputs {
			err = inputs.AddSubInput(subInput.PortId, groupInputId, subInput.PortIndex)
			if err != nil {
				if collectOrReturn(err, validate, errs) != nil {
					return err
				}
			}
		}
	}
	return nil
}

func LoadOutputValues(node NodeBaseInterface, nodeI map[string]any, validate bool, errs *[]error) error {
	outputs, hasOutputs := node.(HasOutputsInterface)
	outputValues, err := utils.GetTypedPropertyByPath[map[string]any](nodeI, "outputs")
	if err != nil {
		if errors.Is(err, &utils.ErrPropertyNotFound{}) {
			return nil
		}
	}
	if !hasOutputs {
		return collectOrReturn(CreateErr(nil, nil, "node '%s' (%s) does not have outputs but outputs are defined", node.GetName(), node.GetId()), validate, errs)
	}

	type subOutput struct {
		PortId    string
		PortIndex int
	}

	subOutputs := map[string][]subOutput{}

	for portId := range outputValues {
		arrayOutputId, portIndex, isIndexPort := IsValidIndexPortId(portId)
		if isIndexPort {
			_, _, ok := outputs.OutputDefByPortId(arrayOutputId)
			if !ok {
				err := CreateErr(nil, nil, "source node '%s' (%s) has no array output '%s'", node.GetName(), node.GetId(), arrayOutputId)
				if collectOrReturn(err, validate, errs) != nil {
					return err
				}
				continue
			}

			subOutputs[arrayOutputId] = append(subOutputs[arrayOutputId], subOutput{
				PortId:    portId,
				PortIndex: portIndex,
			})
		} else {
			// at the moment output values can only be used to define an output port
			err := CreateErr(nil, nil, "source node '%s' (%s) has no output '%s'", node.GetName(), node.GetId(), portId)
			if collectOrReturn(err, validate, errs) != nil {
				return err
			}
		}
	}

	for _, subOutputs := range subOutputs {
		sort.Slice(subOutputs, func(i, j int) bool {
			return subOutputs[i].PortIndex < subOutputs[j].PortIndex
		})
	}

	for arrayOutputId, subOutputs := range subOutputs {
		for _, subOutput := range subOutputs {
			err = outputs.AddSubOutput(subOutput.PortId, arrayOutputId, subOutput.PortIndex)
			if err != nil {
				if collectOrReturn(err, validate, errs) != nil {
					return err
				}
			}
		}
	}
	return nil
}

func LoadExecutions(ag *ActionGraph, nodesYaml map[string]any, validate bool, errs *[]error) error {

	executionsList, err := utils.GetTypedPropertyByPath[[]any](nodesYaml, "executions")
	if err != nil {
		return collectOrReturn(err, validate, errs)
	}

	for _, executions := range executionsList {
		c, ok := executions.(map[string]any)
		if !ok {
			err := CreateErr(nil, nil, "execution is not a map")
			if collectOrReturn(err, validate, errs) != nil {
				return err
			}
			continue
		}

		srcNodeId, err := utils.GetTypedPropertyByPath[string](c, "src.node")
		if err != nil {
			if collectOrReturn(err, validate, errs) != nil {
				return err
			}
			continue
		}

		dstNodeId, err := utils.GetTypedPropertyByPath[string](c, "dst.node")
		if err != nil {
			if collectOrReturn(err, validate, errs) != nil {
				return err
			}
			continue
		}

		srcPort, err := utils.GetTypedPropertyByPath[string](c, "src.port")
		if err != nil {
			if collectOrReturn(err, validate, errs) != nil {
				return err
			}
			continue
		}

		dstPort, err := utils.GetTypedPropertyByPath[string](c, "dst.port")
		if err != nil {
			if collectOrReturn(err, validate, errs) != nil {
				return err
			}
			continue
		}

		srcNode, ok := ag.FindNode(srcNodeId)
		if !ok {
			err := CreateErr(nil, nil, "src node '%s' does not exist", srcNodeId)
			if collectOrReturn(err, validate, errs) != nil {
				return err
			}
			continue
		}

		dstNode, ok := ag.FindNode(dstNodeId)
		if !ok {
			err := CreateErr(nil, nil, "connection dst node '%s' does not exist", dstNodeId)
			if collectOrReturn(err, validate, errs) != nil {
				return err
			}
			continue
		}

		srcExecNode, ok := srcNode.(HasExecutionInterface)
		if !ok {
			err := CreateErr(nil, err, "src node '%s' (%s) does not have an execution interface", srcNode.GetName(), srcNodeId)
			if collectOrReturn(err, validate, errs) != nil {
				return err
			}
			continue
		}

		// group input nodes don't have any port definitions, so anything is allowed.
		if !strings.HasPrefix(srcNode.GetNodeTypeId(), "core/group-inputs@") {
			srcOutputNode, ok := srcNode.(HasOutputsInterface)
			if !ok {
				err := CreateErr(nil, err, "src node '%s' (%s) does not have an output interface", srcNode.GetName(), srcNodeId)
				if collectOrReturn(err, validate, errs) != nil {
					return err
				}
				continue
			}

			_, _, ok = srcOutputNode.OutputDefByPortId(srcPort)
			if !ok {
				err := CreateErr(nil, nil, "src node '%s' (%s) has no execution output '%s'", srcNode.GetName(), srcNodeId, srcPort)
				if collectOrReturn(err, validate, errs) != nil {
					return err
				}
				continue
			}
		}

		// group output nodes don't have any port definitions, so anything is allowed.
		if !strings.HasPrefix(dstNode.GetNodeTypeId(), "core/group-outputs@") {
			dstInputNode, ok := dstNode.(HasInputsInterface)
			if !ok {
				err := CreateErr(nil, err, "dst node '%s' ('%s') does not have an input interface", dstNode.GetName(), dstNodeId)
				if collectOrReturn(err, validate, errs) != nil {
					return err
				}
				continue
			}

			_, _, ok = dstInputNode.InputDefByPortId(dstPort)
			if !ok {
				err := CreateErr(nil, nil, "dst node '%s' (%s) has no execution input '%s'", dstNode.GetName(), dstNodeId, dstPort)
				if collectOrReturn(err, validate, errs) != nil {
					return err
				}
				continue
			}
		}

		err = srcExecNode.ConnectExecutionPort(srcNode, OutputId(srcPort), dstNode, InputId(dstPort))
		if err != nil {
			if collectOrReturn(CreateErr(nil, err, "failed to connect execution ports"), validate, errs) != nil {
				return err
			}
			continue
		}
	}
	return nil
}

func LoadConnections(ag *ActionGraph, nodesYaml map[string]any, parent NodeBaseInterface, validate bool, errs *[]error) error {

	connectionsList, err := utils.GetTypedPropertyByPath[[]any](nodesYaml, "connections")
	if err != nil {
		return collectOrReturn(err, validate, errs)
	}

	for _, connection := range connectionsList {
		c, ok := connection.(map[string]any)
		if !ok {
			err := CreateErr(nil, nil, "connection is not a map")
			if collectOrReturn(err, validate, errs) != nil {
				return err
			}
			continue
		}

		srcNodeId, err := utils.GetTypedPropertyByPath[string](c, "src.node")
		if err != nil {
			if collectOrReturn(err, validate, errs) != nil {
				return err
			}
			continue
		}

		dstNodeId, err := utils.GetTypedPropertyByPath[string](c, "dst.node")
		if err != nil {
			if collectOrReturn(err, validate, errs) != nil {
				return err
			}
			continue
		}

		srcPort, err := utils.GetTypedPropertyByPath[string](c, "src.port")
		if err != nil {
			if collectOrReturn(err, validate, errs) != nil {
				return err
			}
			continue
		}

		dstPort, err := utils.GetTypedPropertyByPath[string](c, "dst.port")
		if err != nil {
			if collectOrReturn(err, validate, errs) != nil {
				return err
			}
			continue
		}

		srcNode, ok := ag.FindNode(srcNodeId)
		if !ok {
			err := CreateErr(nil, nil, "src node '%s' does not exist", srcNodeId)
			if collectOrReturn(err, validate, errs) != nil {
				return err
			}
			continue
		}

		dstNode, ok := ag.FindNode(dstNodeId)
		if !ok {
			err := CreateErr(nil, nil, "connection dst node '%s' does not exist", dstNodeId)
			if collectOrReturn(err, validate, errs) != nil {
				return err
			}
			continue
		}

		dstInputNode, ok := dstNode.(HasInputsInterface)
		if !ok {
			err := CreateErr(nil, err, "dst node '%s' ('%s') does not have an input interface", dstNode.GetName(), dstNodeId)
			if collectOrReturn(err, validate, errs) != nil {
				return err
			}
			continue
		}

		// This calls PortsAreCompatible internally (via ConnectDataPort in inputs.go)
		// If that fails, it returns an error which we collect here.
		err = dstInputNode.ConnectDataPort(srcNode, srcPort, dstNode, dstPort, parent, ConnectOpts{
			SkipValidation: strings.HasPrefix(srcNode.GetNodeTypeId(), "core/group@") || strings.HasPrefix(dstNode.GetNodeTypeId(), "core/group@"),
		})
		if err != nil {
			if collectOrReturn(CreateErr(nil, err, "failed to connect data ports"), validate, errs) != nil {
				return err
			}
			continue
		}
	}
	return nil
}

func RunGraphFromString(ctx context.Context, graphName string, graphContent string, opts RunOpts, debugCb DebugCallback) error {
	utils.ApplyLogLevel()

	if utils.GetLogLevel() == utils.LogLevelVerbose {
		t0 := time.Now()
		defer func() {
			utils.LogOut.Printf("Total time: %v\n", time.Since(t0))
		}()
	}

	err := RunGraph(ctx, graphName, []byte(graphContent), opts, debugCb)
	if err != nil {
		return err
	}

	return nil
}

func RunGraphFromFile(ctx context.Context, graphFile string, opts RunOpts, debugCb DebugCallback) error {
	graphContent, err := os.ReadFile(graphFile)
	if err != nil {
		if os.IsNotExist(err) {
			err = fmt.Errorf("open %s: no such file or directory", graphFile)
		}

		return CreateErr(nil, err, "failed loading graph")
	}

	err = RunGraphFromString(ctx, graphFile, string(graphContent), opts, debugCb)
	if err != nil {
		return err
	}

	return nil
}
