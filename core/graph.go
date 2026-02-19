package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/actionforge/actrun-cli/github/server"
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
	LocalGhServer   bool
	VS              *ValidationState
}

type ActionGraph struct {
	Nodes   map[string]NodeBaseInterface
	Inputs  map[InputId]InputDefinition   `yaml:"inputs" json:"inputs" bson:"inputs"`
	Outputs map[OutputId]OutputDefinition `yaml:"outputs" json:"outputs" bson:"outputs"`

	Entry string

	// ConcurrencyLocks maps node ID → *sync.Mutex. Used to serialize concurrent
	// calls to a node's ExecuteImpl when the node's _disable_concurrency input is true.
	ConcurrencyLocks *sync.Map `yaml:"-" json:"-"`
}

// ValidationState collects errors and warnings during graph validation.
// When a non-nil *ValidationState is passed to Load* functions, it signals
// validation mode: errors are accumulated instead of failing fast.
type ValidationState struct {
	Errors   []error
	Warnings []string
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
		Nodes:            make(map[string]NodeBaseInterface),
		ConcurrencyLocks: &sync.Map{},
	}
}

// collectOrReturn appends err to vs.Errors when in validation mode (vs != nil)
// and returns nil so the caller can continue. In non-validation mode it returns
// the error directly so the caller can fail fast.
func collectOrReturn(err error, vs *ValidationState) error {
	if err == nil {
		return nil
	}
	if vs != nil {
		vs.Errors = append(vs.Errors, err)
		return nil
	}
	return err
}

func LoadEntry(ag *ActionGraph, nodesYaml map[string]any, vs *ValidationState) error {
	entryAny, exists := nodesYaml["entry"]
	if !exists {
		return collectOrReturn(CreateErr(nil, nil, "entry is missing"), vs)
	}

	entry, ok := entryAny.(string)
	if !ok {
		return collectOrReturn(CreateErr(nil, nil, "entry is not a string"), vs)
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
		Graph:                   graph,
		Hierarchy:               make([]NodeBaseInterface, 0),
		ContextStackLock:        &sync.RWMutex{},
		OutputCacheLock:         &sync.RWMutex{},
		PendingConcurrencyLocks: &sync.Map{},

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
		StepCache:            NewStepCache(nil),

		PostSteps:     NewPostStepQueue(),
		JobConclusion: "success",
	}
}

func RunGraph(ctx context.Context, graphName string, graphContent []byte, opts RunOpts, debugCb DebugCallback) error {
	graphYaml := make(map[string]any)
	if err := yaml.Unmarshal(graphContent, &graphYaml); err != nil {
		return CreateErr(nil, err, "failed to load yaml")
	}

	// Capture GITHUB_TOKEN / INPUT_GITHUB_TOKEN from the OS environment and store in
	// OverrideSecrets so it remains available for repo cloning (gh-action) and
	// is properly surfaced as secrets.GITHUB_TOKEN / github.token. Then remove
	// from the OS environment to prevent subprocesses from extracting it via
	// /proc/<ppid>/environ or similar.
	if opts.OverrideSecrets == nil {
		opts.OverrideSecrets = make(map[string]string)
	}
	if _, exists := opts.OverrideSecrets["GITHUB_TOKEN"]; !exists {
		if ghToken, ok := opts.OverrideEnv["GITHUB_TOKEN"]; ok && ghToken != "" {
			opts.OverrideSecrets["GITHUB_TOKEN"] = ghToken
		} else if ghToken := os.Getenv("GITHUB_TOKEN"); ghToken != "" {
			opts.OverrideSecrets["GITHUB_TOKEN"] = ghToken
		} else if inputToken := os.Getenv("INPUT_GITHUB_TOKEN"); inputToken != "" {
			opts.OverrideSecrets["GITHUB_TOKEN"] = inputToken
		} else if inputToken := os.Getenv("INPUT_TOKEN"); inputToken != "" {
			opts.OverrideSecrets["GITHUB_TOKEN"] = inputToken
		}
	}
	delete(opts.OverrideEnv, "GITHUB_TOKEN")
	os.Unsetenv("GITHUB_TOKEN")
	os.Unsetenv("INPUT_GITHUB_TOKEN")
	os.Unsetenv("INPUT_TOKEN")

	ag, errs := LoadGraph(graphYaml, nil, "", nil, opts)
	if len(errs) > 0 {
		return CreateErr(nil, errs[0], "failed to load graph")
	}

	entry, err := ag.GetEntry()
	if err != nil {
		return CreateErr(nil, err, "failed to load graph")
	}

	entryNode, isBaseNode := entry.(NodeBaseInterface)

	// isGitHubWorkflow: Determines if this run should behave as a GitHub Action.
	// True when either running on actual GitHub Actions (system env), or an external
	// caller (e.g., web app) explicitly requests GitHub Actions behavior via OverrideEnv.
	// This affects input variable handling, context loading, and other GitHub-specific behavior.
	//
	// **Important** we haven't loaded the config file yet, so we can only look at overriden envs,
	// .env (already set in os.GetEnv) or shell.
	isGitHubWorkflow := false
	if opts.OverrideEnv["GITHUB_ACTIONS"] == "true" {
		isGitHubWorkflow = true
		utils.LogOut.Info("GitHub workflow detected via OverrideEnv\n")
	} else if os.Getenv("GITHUB_ACTIONS") == "true" {
		isGitHubWorkflow = true
		utils.LogOut.Info("GitHub workflow detected via GITHUB_ACTIONS environment variable (.env or shell)\n")
	} else if entryNode.GetNodeTypeId() == "core/gh-start@v1" {
		isGitHubWorkflow = true
		utils.LogOut.Info("GitHub workflow detected via entry node type: core/gh-start@v1\n")
	}

	// mimickGitHubEnv: Determines if we need to set up a simulated GitHub environment. The easiest
	// approach for now is to just check a bunch of env vars. The user may have set one or the other
	// (through .env or shell) but unlikely all of them but they are by a real GitHub Actions runner.
	mimickGitHubEnv := isGitHubWorkflow && os.Getenv("GITHUB_RUN_ID") == "" &&
		os.Getenv("RUNNER_TEMP") == "" &&
		os.Getenv("GITHUB_API_URL") == "" &&
		os.Getenv("GITHUB_RETENTION_DAYS") == ""

	// Initialize trackers with their respective categories
	envTracker := newValueMap[string]("env")
	inputTracker := newValueMap[any]("input")
	secretTracker := newValueMap[string]("secret")
	matrixTracker := newValueMap[any]("matrix")
	needsTracker := newValueMap[any]("needs")

	// Priority 1 (Lowest): Config file
	if opts.ConfigFile != "" {
		cleanConfigPath, err := utils.ValidatePath(opts.ConfigFile)
		if err != nil {
			return CreateErr(nil, err, "invalid config file path")
		}
		if _, err := os.Stat(cleanConfigPath); err == nil {
			localConfig, err := utils.LoadConfig(cleanConfigPath)
			if err != nil {
				return CreateErr(nil, err, "failed to load config file")
			}

			configName := filepath.Base(cleanConfigPath)
			envTracker.set(localConfig.Env, configName, true, false)
			inputTracker.set(localConfig.Inputs, configName, true, false)
			secretTracker.set(localConfig.Secrets, configName, true, true)
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
		case isGitHubWorkflow && (k == "ACT_INPUT_TOKEN" || k == "ACT_INPUT_GITHUB_TOKEN"):
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

	var newCwd string

	if cwd, ok := finalEnv["ACT_CWD"]; ok {
		newCwd = cwd
		utils.LogOut.Debugf("changing working directory to ACT_CWD: %s\n", newCwd)
	}

	if mimickGitHubEnv {
		// If we are running a github actions workflow, then mimic a GitHub Actions environment
		// But only do is if we are NOT already in GitHub Actions
		err = SetupGitHubActionsEnv(finalEnv)
		if err != nil {
			return CreateErr(nil, err, "failed to setup GitHub Actions environment")
		}

		if opts.LocalGhServer {
			// RUNNER_TEMP is provided by the local editor over a 127.0.0.1-only WebSocket; not an external input.
			storageDir, mkErr := os.MkdirTemp(finalEnv["RUNNER_TEMP"], "gh-server-storage-") // lgtm[go/path-injection]
			if mkErr != nil {
				return CreateErr(nil, mkErr, "failed to create storage directory for local GitHub Actions server")
			}
			rs, srvErr := server.StartServer(server.Config{StorageDir: storageDir})
			if srvErr != nil {
				return CreateErr(nil, srvErr, "failed to start GitHub Actions mock server")
			}
			defer rs.Stop()
			rs.InjectEnv(finalEnv)
			utils.LogOut.Infof("GitHub Actions mock server started at %s\n", rs.URL)
		}

		// Use the updated GITHUB_WORKSPACE as the working directory.
		// SetupGitHubActionsEnv replaces GITHUB_WORKSPACE with a fresh temp folder.
		if cwd, ok := finalEnv["GITHUB_WORKSPACE"]; ok {
			newCwd = cwd
			utils.LogOut.Debugf("changing working directory to GITHUB_WORKSPACE: %s\n", newCwd)
		}
	} else if debugCb != nil && newCwd == "" {
		// for debug sessions, always create a temp working directory if none is set
		tmpDir, tmpErr := os.MkdirTemp("", "actrun-debug-*")
		if tmpErr != nil {
			return CreateErr(nil, tmpErr, "failed to create temp working directory for debug session")
		}

		newCwd = tmpDir
		utils.LogOut.Infof("created temp working directory for debug session: %s\n", newCwd)

		defer func() {
			_ = os.RemoveAll(tmpDir)
		}()
	}

	if newCwd != "" {
		cleanCwd, err := utils.ValidatePath(newCwd)
		if err != nil {
			return CreateErr(nil, err, "invalid working directory path")
		}
		originalCwd, err := os.Getwd()
		if err != nil {
			return CreateErr(nil, err, "failed to get current working directory")
		}
		if err := os.Chdir(cleanCwd); err != nil {
			return CreateErr(nil, err, "failed to change working directory to ACT_CWD/GITHUB_WORKSPACE")
		}
		defer func() {
			_ = os.Chdir(originalCwd)
		}()
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

	mainErr := entry.ExecuteEntry(c, nil, opts.Args)
	if mainErr != nil {
		c.JobConclusion = "failure"
	}
	if c.PostSteps.Len() > 0 {
		executePostSteps(c, c.PostSteps.DrainLIFO())
	}
	return mainErr
}

func LoadGraph(graphYaml map[string]any, parent NodeBaseInterface, parentId string, vs *ValidationState, opts RunOpts) (ActionGraph, []error) {

	opts.VS = vs
	ag := NewActionGraph()

	var err error

	ag.Inputs, err = LoadGraphInputs(graphYaml)
	if err != nil {
		if collectOrReturn(err, vs) != nil {
			return ActionGraph{}, []error{err}
		}
	}

	ag.Outputs, err = LoadGraphOutputs(graphYaml)
	if err != nil {
		if collectOrReturn(err, vs) != nil {
			return ActionGraph{}, []error{err}
		}
	}

	err = LoadNodes(&ag, parent, parentId, graphYaml, vs, opts)
	if err != nil && vs == nil {
		return ActionGraph{}, []error{err}
	}

	err = LoadExecutions(&ag, graphYaml, vs)
	if err != nil && vs == nil {
		return ActionGraph{}, []error{err}
	}

	err = LoadConnections(&ag, graphYaml, parent, vs)
	if err != nil && vs == nil {
		return ActionGraph{}, []error{err}
	}

	err = LoadEntry(&ag, graphYaml, vs)
	if err != nil && vs == nil {
		return ActionGraph{}, []error{err}
	}

	if vs != nil {
		return ag, vs.Errors
	}
	return ag, nil
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

func LoadNodes(ag *ActionGraph, parent NodeBaseInterface, parentId string, nodesYaml map[string]any, vs *ValidationState, opts RunOpts) error {
	nodesList, err := utils.GetTypedPropertyByPath[[]any](nodesYaml, "nodes")
	if err != nil {
		return collectOrReturn(err, vs)
	}

	for _, nodeData := range nodesList {
		n, id, err := LoadNode(parent, parentId, nodeData, vs, opts)
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

func LoadNode(parent NodeBaseInterface, parentId string, nodeData any, vs *ValidationState, opts RunOpts) (NodeBaseInterface, string, error) {
	nodeI, ok := nodeData.(map[string]any)
	if !ok {
		err := CreateErr(nil, nil, "node is not a map")
		if collectOrReturn(err, vs) != nil {
			return nil, "", err
		}
		return nil, "", nil
	}

	validate := vs != nil

	// We attempt to get the ID. If it fails, we record the error but CONTINUE
	// processing (if validating) to check Type, Inputs, and Outputs.
	id, idErr := utils.GetTypedPropertyByPath[string](nodeI, "id")
	if idErr != nil {
		if err := collectOrReturn(idErr, vs); err != nil {
			return nil, "", err
		}
	}

	// If Type is missing, loading "makes no sense" as we cannot select a factory.
	// We must early out here.
	nodeType, typeErr := utils.GetTypedPropertyByPath[string](nodeI, "type")
	if typeErr != nil {
		if err := collectOrReturn(typeErr, vs); err != nil {
			return nil, "", err
		}
		return nil, "", nil
	}

	nodeLabel, _ := utils.GetTypedPropertyByPath[string](nodeI, "label")

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
		n, factoryErrs = NewGhActionNode(nodeType, parent, fullPath, validate, opts)
	} else {
		n, factoryErrs = NewNodeInstance(nodeType, parent, fullPath, nodeI, validate, opts)
	}

	if len(factoryErrs) > 0 {
		if !validate {
			// Early out on first error if not validating
			return nil, "", factoryErrs[0]
		}
		vs.Errors = append(vs.Errors, factoryErrs...)
	}

	// If the factory failed to produce a node instance completely (n is nil),
	// we cannot proceed to check inputs/outputs.
	if n == nil {
		return nil, "", nil
	}

	if idErr == nil {
		if nodeLabel != "" {
			n.SetLabel(nodeLabel)
		}
		n.SetId(id)
		if parentId != "" {
			n.SetFullPath(parentId + "/" + id)
		} else {
			n.SetFullPath(id)
		}
	}

	// We continue to check inputs/outputs even if factoryErrs occurred,
	// provided 'n' exists.
	inputErr := LoadInputValues(n, nodeI, vs)
	if inputErr != nil && !validate {
		return nil, "", inputErr
	}

	// Validate Outputs
	outputErr := LoadOutputValues(n, nodeI, vs)
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

func LoadInputValues(node NodeBaseInterface, nodeI map[string]any, vs *ValidationState) error {
	inputs, hasInputs := node.(HasInputsInterface)
	inputValues, err := utils.GetTypedPropertyByPath[map[string]any](nodeI, "inputs")
	if err != nil {
		if errors.Is(err, &utils.ErrPropertyNotFound{}) {
			return nil
		}
		return collectOrReturn(err, vs)
	}
	if !hasInputs {
		return collectOrReturn(CreateErr(nil, nil, "dst node '%s' (%s) does not have inputs but inputs are defined", node.GetName(), node.GetId()), vs)
	}

	type subInput struct {
		PortId    string
		PortIndex int
	}

	subInputs := map[string][]subInput{}

	// _disable_concurrency is not a regular input, its stored directly on
	// the node instance so we pull it out before processing the rest.
	if v, ok := inputValues["_disable_concurrency"]; ok {
		if b, ok := v.(bool); ok && b {
			node.SetDisableConcurrency(true)
		}
		delete(inputValues, "_disable_concurrency")
	}

	for portId, inputValue := range inputValues {
		groupInputId, portIndex, isIndexPort := IsValidIndexPortId(portId)
		if isIndexPort {
			_, _, ok := inputs.InputDefByPortId(groupInputId)
			if !ok {
				err := CreateErr(nil, nil, "dst node '%s' (%s) has no array input '%s'", node.GetName(), node.GetId(), groupInputId)
				if collectOrReturn(err, vs) != nil {
					return err
				}
				continue
			}

			subInputs[groupInputId] = append(subInputs[groupInputId], subInput{
				PortId:    portId,
				PortIndex: portIndex,
			})
		}

		// In validation mode, check that the input actually exists in the
		// node's definition. At runtime we skip this because SetInputValue
		// silently stores any key and unknown inputs are simply ignored.
		if vs != nil && !isIndexPort {
			if _, _, ok := inputs.InputDefByPortId(portId); !ok {
				err := CreateErr(nil, nil, "dst node '%s' (%s) has no input '%s'", node.GetName(), node.GetId(), portId)
				if collectOrReturn(err, vs) != nil {
					return err
				}
				continue
			}
		}

		err = inputs.SetInputValue(InputId(portId), inputValue)
		if err != nil {
			if collectOrReturn(err, vs) != nil {
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
				if collectOrReturn(err, vs) != nil {
					return err
				}
			}
		}
	}
	return nil
}

func LoadOutputValues(node NodeBaseInterface, nodeI map[string]any, vs *ValidationState) error {
	outputs, hasOutputs := node.(HasOutputsInterface)
	outputValues, err := utils.GetTypedPropertyByPath[map[string]any](nodeI, "outputs")
	if err != nil {
		if errors.Is(err, &utils.ErrPropertyNotFound{}) {
			return nil
		}
	}
	if !hasOutputs {
		return collectOrReturn(CreateErr(nil, nil, "node '%s' (%s) does not have outputs but outputs are defined", node.GetName(), node.GetId()), vs)
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
				if collectOrReturn(err, vs) != nil {
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
			if collectOrReturn(err, vs) != nil {
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
				if collectOrReturn(err, vs) != nil {
					return err
				}
			}
		}
	}
	return nil
}

func LoadExecutions(ag *ActionGraph, nodesYaml map[string]any, vs *ValidationState) error {

	executionsList, err := utils.GetTypedPropertyByPath[[]any](nodesYaml, "executions")
	if err != nil {
		return collectOrReturn(err, vs)
	}

	for _, executions := range executionsList {
		c, ok := executions.(map[string]any)
		if !ok {
			err := CreateErr(nil, nil, "execution is not a map")
			if collectOrReturn(err, vs) != nil {
				return err
			}
			continue
		}

		srcNodeId, err := utils.GetTypedPropertyByPath[string](c, "src.node")
		if err != nil {
			if collectOrReturn(err, vs) != nil {
				return err
			}
			continue
		}

		dstNodeId, err := utils.GetTypedPropertyByPath[string](c, "dst.node")
		if err != nil {
			if collectOrReturn(err, vs) != nil {
				return err
			}
			continue
		}

		srcPort, err := utils.GetTypedPropertyByPath[string](c, "src.port")
		if err != nil {
			if collectOrReturn(err, vs) != nil {
				return err
			}
			continue
		}

		dstPort, err := utils.GetTypedPropertyByPath[string](c, "dst.port")
		if err != nil {
			if collectOrReturn(err, vs) != nil {
				return err
			}
			continue
		}

		srcNode, ok := ag.FindNode(srcNodeId)
		if !ok {
			err := CreateErr(nil, nil, "src node '%s' does not exist", srcNodeId)
			if collectOrReturn(err, vs) != nil {
				return err
			}
			continue
		}

		dstNode, ok := ag.FindNode(dstNodeId)
		if !ok {
			err := CreateErr(nil, nil, "connection dst node '%s' does not exist", dstNodeId)
			if collectOrReturn(err, vs) != nil {
				return err
			}
			continue
		}

		srcExecNode, ok := srcNode.(HasExecutionInterface)
		if !ok {
			err := CreateErr(nil, err, "src node '%s' (%s) does not have an execution interface", srcNode.GetName(), srcNodeId)
			if collectOrReturn(err, vs) != nil {
				return err
			}
			continue
		}

		// group input nodes don't have any port definitions, so anything is allowed.
		if !strings.HasPrefix(srcNode.GetNodeTypeId(), "core/group-inputs@") {
			srcOutputNode, ok := srcNode.(HasOutputsInterface)
			if !ok {
				err := CreateErr(nil, err, "src node '%s' (%s) does not have an output interface", srcNode.GetName(), srcNodeId)
				if collectOrReturn(err, vs) != nil {
					return err
				}
				continue
			}

			_, _, ok = srcOutputNode.OutputDefByPortId(srcPort)
			if !ok {
				err := CreateErr(nil, nil, "src node '%s' (%s) has no execution output '%s'", srcNode.GetName(), srcNodeId, srcPort)
				if collectOrReturn(err, vs) != nil {
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
				if collectOrReturn(err, vs) != nil {
					return err
				}
				continue
			}

			_, _, ok = dstInputNode.InputDefByPortId(dstPort)
			if !ok {
				err := CreateErr(nil, nil, "dst node '%s' (%s) has no execution input '%s'", dstNode.GetName(), dstNodeId, dstPort)
				if collectOrReturn(err, vs) != nil {
					return err
				}
				continue
			}
		}

		err = srcExecNode.ConnectExecutionPort(srcNode, OutputId(srcPort), dstNode, InputId(dstPort))
		if err != nil {
			if collectOrReturn(CreateErr(nil, err, "failed to connect execution ports"), vs) != nil {
				return err
			}
			continue
		}
	}
	return nil
}

func LoadConnections(ag *ActionGraph, nodesYaml map[string]any, parent NodeBaseInterface, vs *ValidationState) error {

	connectionsList, err := utils.GetTypedPropertyByPath[[]any](nodesYaml, "connections")
	if err != nil {
		return collectOrReturn(err, vs)
	}

	for _, connection := range connectionsList {
		c, ok := connection.(map[string]any)
		if !ok {
			err := CreateErr(nil, nil, "connection is not a map")
			if collectOrReturn(err, vs) != nil {
				return err
			}
			continue
		}

		srcNodeId, err := utils.GetTypedPropertyByPath[string](c, "src.node")
		if err != nil {
			if collectOrReturn(err, vs) != nil {
				return err
			}
			continue
		}

		dstNodeId, err := utils.GetTypedPropertyByPath[string](c, "dst.node")
		if err != nil {
			if collectOrReturn(err, vs) != nil {
				return err
			}
			continue
		}

		srcPort, err := utils.GetTypedPropertyByPath[string](c, "src.port")
		if err != nil {
			if collectOrReturn(err, vs) != nil {
				return err
			}
			continue
		}

		dstPort, err := utils.GetTypedPropertyByPath[string](c, "dst.port")
		if err != nil {
			if collectOrReturn(err, vs) != nil {
				return err
			}
			continue
		}

		srcNode, ok := ag.FindNode(srcNodeId)
		if !ok {
			err := CreateErr(nil, nil, "src node '%s' does not exist", srcNodeId)
			if collectOrReturn(err, vs) != nil {
				return err
			}
			continue
		}

		dstNode, ok := ag.FindNode(dstNodeId)
		if !ok {
			err := CreateErr(nil, nil, "connection dst node '%s' does not exist", dstNodeId)
			if collectOrReturn(err, vs) != nil {
				return err
			}
			continue
		}

		dstInputNode, ok := dstNode.(HasInputsInterface)
		if !ok {
			err := CreateErr(nil, err, "dst node '%s' ('%s') does not have an input interface", dstNode.GetName(), dstNodeId)
			if collectOrReturn(err, vs) != nil {
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
			if collectOrReturn(CreateErr(nil, err, "failed to connect data ports"), vs) != nil {
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
	cleanPath, err := utils.ValidatePath(graphFile)
	if err != nil {
		return CreateErr(nil, err, "invalid graph file path")
	}
	graphContent, err := os.ReadFile(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			err = fmt.Errorf("open %s: no such file or directory", cleanPath)
		}

		return CreateErr(nil, err, "failed loading graph")
	}

	err = RunGraphFromString(ctx, graphFile, string(graphContent), opts, debugCb)
	if err != nil {
		return err
	}

	return nil
}

// Matches URLs like https://app.actionforge.dev/shared/<id>.act
var sharedURLPattern = regexp.MustCompile(`^https://app\.actionforge\.dev/shared/([a-zA-Z0-9_-]+\.act)$`)

const shareAPIURL = "https://app.actionforge.dev/api/v2/share/graph/read"

func IsSharedGraphURL(graphURL string) bool {
	return sharedURLPattern.MatchString(graphURL)
}

func ParseSharedGraphURL(graphURL string) (string, bool) {
	matches := sharedURLPattern.FindStringSubmatch(graphURL)
	if len(matches) != 2 {
		return "", false
	}
	return matches[1], true
}

// RunGraphFromURL fetches and runs a graph from a shared URL.
// Only urls from app.actionforge.dev are accepted
func RunGraphFromURL(ctx context.Context, graphURL string, opts RunOpts, debugCb DebugCallback) error {
	parsedURL, err := url.Parse(graphURL)
	if err != nil {
		return CreateErr(nil, err, "invalid URL format")
	}

	if parsedURL.Host != "app.actionforge.dev" {
		return CreateErr(nil, nil, "invalid shared graph URL: only URLs from app.actionforge.dev are accepted, got '%s'", parsedURL.Host).
			SetHint("Double-check the URL - shared graphs must be hosted on app.actionforge.dev")
	}

	shareID, ok := ParseSharedGraphURL(graphURL)
	if !ok {
		return CreateErr(nil, nil, "invalid shared graph URL: expected format https://app.actionforge.dev/shared/<id>.act")
	}

	apiURL := fmt.Sprintf("%s?id=%s", shareAPIURL, url.QueryEscape(shareID))

	utils.LogOut.Debugf("fetching shared graph from: %s\n", apiURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return CreateErr(nil, err, "failed to create request for shared graph")
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return CreateErr(nil, err, "failed to fetch shared graph")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return CreateErr(nil, nil, "failed to fetch shared graph: server returned status %d", resp.StatusCode)
	}

	graphContent, err := io.ReadAll(resp.Body)
	if err != nil {
		return CreateErr(nil, err, "failed to read shared graph content")
	}

	graphName := shareID

	return RunGraphFromString(ctx, graphName, string(graphContent), opts, debugCb)
}
