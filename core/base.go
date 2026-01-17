package core

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/actionforge/actrun-cli/build"
	"github.com/actionforge/actrun-cli/utils"

	"github.com/google/uuid"
	"go.yaml.in/yaml/v4"
)

const expiryDays = 1000

type InputId string
type OutputId string

var (
	onceIndexPortRegex sync.Once
	indexPortRegex     *regexp.Regexp
)

func getIndexPortRegex() *regexp.Regexp {
	onceIndexPortRegex.Do(func() {
		indexPortRegex = regexp.MustCompile(`^([\w-]+)\[([0-9]+)\]$`)
	})
	return indexPortRegex
}

type CredentialType int

const (
	CredentialTypeSSH CredentialType = iota
	CredentialTypeUsernamePassword
	CredentialTypeAccessKey
)

type Credentials interface {
	Type() CredentialType
}

type StorageProvider interface {
	GetName() string
}

type IndexPortInfo struct {
	IndexPortId string // my-port[1234]
	ArrayPortId string // my-port
	Index       int    // 1234
}

type AssignmentMode int64

const (
	AssignmentMode_Merge AssignmentMode = iota
	AssignmentMode_Replace
)

type SetDefsOpts struct {
	AssignmentMode
}

type GitRepository struct {
	Path string
}

type SecretValue struct {
	Secret string
}

type DataStreamFactory struct {
	SourcePath     string
	SourceProvider any
	Reader         io.Reader
	Length         int64
}

func (dsf *DataStreamFactory) CloseStream() error {
	return utils.SafeCloseReader(dsf.Reader)
}

func (dsf *DataStreamFactory) CloseStreamAndIgnoreError() {
	_ = dsf.CloseStream()
}

func GetReaderLength(r io.Reader) int64 {
	switch v := r.(type) {
	case *bytes.Buffer:
		return int64(v.Len())
	case *bytes.Reader:
		return int64(v.Len())
	case *strings.Reader:
		return int64(v.Len())
	case *os.File:
		stat, err := v.Stat()
		if err != nil {
			return 0
		}
		return stat.Size()
	default:
		return -1
	}
}

var (
	onceReIsExec sync.Once
	reIsExec     *regexp.Regexp
)

func getExecNameRegex() *regexp.Regexp {
	onceReIsExec.Do(func() {
		reIsExec = regexp.MustCompile(`^exec(-([\w-]+))?$`)
	})
	return reIsExec
}

// An interface for nodes that execute their logic.
type HasExecutionInterface interface {
	Execute(outputPort OutputId, ec *ExecutionState, err error) error
	GetExecutionTarget(outputId OutputId) (ExecutionTarget, bool)
	ExecuteImpl(c *ExecutionState, inputId InputId, prevError error) error
	GetName() string
	GetId() string

	ConnectExecutionPort(srcNode NodeBaseInterface, srcPortId OutputId, dstNode NodeBaseInterface, dstPortId InputId) error
}

// An interface for nodes that can kick off an action graph.
type NodeEntryInterface interface {
	ExecuteEntry(c *ExecutionState, outputValues map[OutputId]any, args []string) error
}

type NodeBaseInterface interface {
	SetNodeType(nodeType string)
	SetFullPath(fullPath string)
	GetFullPath() string
	SetParent(parent NodeBaseInterface)
	GetParent() NodeBaseInterface
	SetId(id string)
	GetNodeTypeId() string
	GetName() string
	GetLabel() string
	GetId() string
	GetCacheId() string
	SetName(name string)
	SetLabel(label string)
	GetGraph() *ActionGraph

	// Instead of checking for 'HasExecutionInterface',
	// use this method, as some nodes have an interface
	// but don't necessarily need to be one,
	// like the 'Group Node'.
	IsExecutionNode() bool
	SetExecutionNode(execNode bool)

	// Returns the cache type where data is stored or should be stored to
	// By default this depends on if this is an execution node or not.
	GetCacheType() CacheType
}

// Base component for nodes that offer values from other nodes.
// The node that implements this component has outgoing connections.
type NodeBaseComponent struct {
	Name            string // Human readable name of the node
	Label           string // Label of the node shown in the graph editor
	Id              string // Unique identifier for the node
	FullPath        string // Full path of the node within the graph hierarchy
	CacheId         string // Unique identifier for the cache
	NodeType        string // Node type of the node (e.g. core/run@v1 or github.com/actions/checkout@v3)
	Graph           *ActionGraph
	Parent          NodeBaseInterface
	isExecutionNode bool
}

func (n *NodeBaseComponent) GetCacheType() CacheType {
	if n.IsExecutionNode() {
		return Permanent
	} else {
		return Ephemeral
	}
}

func (n *NodeBaseComponent) IsExecutionNode() bool {
	return n.isExecutionNode
}

func (n *NodeBaseComponent) SetExecutionNode(execNode bool) {
	n.isExecutionNode = execNode
}

func (n *NodeBaseComponent) SetId(id string) {
	n.Id = id
	n.CacheId = fmt.Sprintf("%s:%s", n.Id, uuid.New().String())
}

func (n *NodeBaseComponent) GetCacheId() string {
	return n.CacheId
}

func (n *NodeBaseComponent) GetId() string {
	return n.Id
}

func (n *NodeBaseComponent) GetNodeTypeId() string {
	return n.NodeType
}

func (n *NodeBaseComponent) SetNodeType(name string) {
	n.NodeType = name
}

func (n *NodeBaseComponent) SetFullPath(fullPath string) {
	n.FullPath = fullPath
}

func (n *NodeBaseComponent) GetFullPath() string {
	return n.FullPath
}

func (n *NodeBaseComponent) SetParent(parent NodeBaseInterface) {
	n.Parent = parent
}

func (n *NodeBaseComponent) GetParent() NodeBaseInterface {
	return n.Parent
}

func (n *NodeBaseComponent) GetGraph() *ActionGraph {
	return n.Graph
}

func (n *NodeBaseComponent) GetName() string {
	return n.Name
}

func (n *NodeBaseComponent) GetLabel() string {
	return n.Label
}

func (n *NodeBaseComponent) SetName(name string) {
	n.Name = name
}

func (n *NodeBaseComponent) SetLabel(label string) {
	n.Label = label
}

func IsValidIndexPortId(id string) (string, int, bool) {
	indexPortMatch := getIndexPortRegex().FindStringSubmatch(id)
	if len(indexPortMatch) < 3 {
		return "", -1, false
	} else {
		portId, err := strconv.Atoi(indexPortMatch[2])
		if err != nil {
			return "", -1, false
		}

		return indexPortMatch[1], portId, true
	}
}

type GetNameIdInterface interface {
	GetName() string
	GetId() string
}

func LogDebugInfoForGh(t GetNameIdInterface) {
	utils.LogOut.Infof("🟢 Execute '%s (%s)'\n",
		t.GetName(),
		t.GetId(),
	)
}

type nodeFactoryFunc func(ctx any, parent NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (NodeBaseInterface, []error)

var registries = make(map[string]NodeTypeDefinitionFull)

func GetRegistries() map[string]NodeTypeDefinitionFull {
	return registries
}

type InputOption struct {
	Name  string `yaml:"name" json:"name" bson:"name"`
	Value string `yaml:"value" json:"value" bson:"value"`
}

type PortDefinition struct {
	Name  string `yaml:"name" json:"name" bson:"name"`
	Type  string `yaml:"type" json:"type" bson:"type"`
	Desc  string `yaml:"desc,omitempty" json:"desc,omitempty" bson:"desc,omitempty"`
	Index int    `yaml:"index" json:"index" bson:"index"`
	Array bool   `yaml:"array,omitempty" json:"array,omitempty" bson:"array,omitempty"`

	ArrayInitialCount int  `yaml:"array_initial_count,omitempty" json:"array_initial_count,omitempty" bson:"array_initial_count,omitempty"`
	Exec              bool `yaml:"exec,omitempty" json:"exec,omitempty" bson:"exec,omitempty"`
}

type OutputDefinition struct {
	PortDefinition `yaml:",inline" json:",inline" bson:",inline"`
}

type InputDefinition struct {
	PortDefinition `yaml:",inline" json:",inline" bson:",inline"`

	HideSocket bool `yaml:"hide_socket,omitempty" json:"hide_socket,omitempty" bson:"hide_socket,omitempty"`

	// Default value used during execution if there is no connection nor a user input value.
	// User defined input strings are explicit and have higher precedence than default values.
	Default any `yaml:"default,omitempty" json:"default,omitempty" bson:"default,omitempty"`
	// Value used by the graph editor to prefill the field. Has no effect or usage during the execution
	Initial  any  `yaml:"initial,omitempty" json:"initial,omitempty" bson:"initial,omitempty"`
	Required bool `yaml:"required,omitempty" json:"required,omitempty" bson:"required,omitempty"`

	// for type "option"
	Options []InputOption `yaml:"options,omitempty" json:"options,omitempty" bson:"options,omitempty"`

	// for type "string"
	Multiline  bool     `yaml:"multiline,omitempty" json:"multiline,omitempty" bson:"multiline,omitempty"`
	Hint       string   `yaml:"hint,omitempty" json:"hint,omitempty" bson:"hint,omitempty"`
	ArrayHints []string `yaml:"array_hints,omitempty" json:"array_hints,omitempty" bson:"array_hints,omitempty"`

	// for type "number"
	Step float64 `yaml:"step,omitempty" json:"step,omitempty" bson:"step,omitempty"`
}

type GhMetadata struct {
	Type string `yaml:"type" json:"type" bson:"type"` // eg node20, composite, docker
	Icon string `yaml:"icon" json:"icon" bson:"icon"`
}

type NodeTypeDefinitionBasic struct {
	Id          string     `yaml:"id" json:"id" bson:"_id"`
	RequestedId string     `yaml:"requested_id" json:"requested_id" bson:"requested_id"`
	Name        string     `yaml:"name" json:"name" bson:"name"`
	ShowInDocs  *bool      `yaml:"show_in_docs" json:"show_in_docs" bson:"show_in_docs"`
	Version     string     `yaml:"version" json:"version" bson:"version"`
	ShortDesc   string     `yaml:"short_desc" json:"short_desc" bson:"short_desc"`
	Category    string     `yaml:"category" json:"category" bson:"category"`
	Entry       bool       `yaml:"entry" json:"entry" bson:"entry"`
	Icon        string     `yaml:"icon" json:"icon" bson:"icon"`
	Label       string     `yaml:"label,omitempty" json:"label,omitempty" bson:"label,omitempty"`
	Compact     bool       `yaml:"compact,omitempty" json:"compact,omitempty" bson:"compact,omitempty"`
	GhMeta      GhMetadata `yaml:"gh_meta" json:"gh_meta" bson:"gh_meta"`

	// Used by the gateway to indicate errors when being retreived
	Error string `yaml:"error,omitempty" json:"error,omitempty" bson:"error,omitempty"`
}

type NodeTypeDefinitionFull struct {
	NodeTypeDefinitionBasic `yaml:",inline" json:",inline" bson:",inline"`

	LongDesc    string `yaml:"long_desc" json:"long_desc" bson:"long_desc"`
	Addendum    string `yaml:"addendum" json:"addendum" bson:"addendum"`
	LllmContext string `yaml:"llm_context,omitempty" json:"llm_context,omitempty" bson:"llm_context,omitempty"`

	Inputs  map[InputId]InputDefinition   `yaml:"inputs" json:"inputs" bson:"inputs"`
	Outputs map[OutputId]OutputDefinition `yaml:"outputs" json:"outputs" bson:"outputs"`

	Style *NodeStyle `yaml:"style,omitempty" json:"style,omitempty" bson:"style,omitempty"`

	// Factory function for creating a new node instance
	// Not part of the yaml definition
	FactoryFn nodeFactoryFunc `yaml:"-" json:"-" bson:"-"`
}

type NodeStyle struct {
	Header NodeStyleHeader `yaml:"header" json:"header" bson:"header"`
	Body   NodeStyleBody   `yaml:"body" json:"body" bson:"body"`
}

type NodeStyleHeader struct {
	Background string `yaml:"background" json:"background" bson:"background"`
}

type NodeStyleBody struct {
	Background string `yaml:"background" json:"background" bson:"background"`
}

func (n *NodeTypeDefinitionFull) IsValid() error {
	if n.Id == "" {
		return CreateErr(nil, nil, "id is missing")
	} else if n.Name == "" {
		return CreateErr(nil, nil, "name is missing in %v", n.Id)
	} else if n.Version == "" {
		return CreateErr(nil, nil, "version is missing in %v", n.Id)
	} else if n.Name[0] != strings.ToUpper(n.Name)[0] {
		return CreateErr(nil, nil, "name must start with an upper case letter in %v", n.Id)
	}
	return nil
}

func PortDefValidation(portId string, portDef PortDefinition) error {
	if portId == "" {
		return errors.New("port id is missing")
	}

	// Important: In our own node definitions we always use `hyphens`, while inputs and outputs can technically
	// have one or the other since inputs and outputs of GitHub Actions have no limitations on the naming.
	m := getExecNameRegex().FindStringSubmatch(portId)
	if portDef.Exec {
		if m == nil {
			return CreateErr(nil, nil, `port '%v' is flagged as exec but does not match '%s'`, portId, getExecNameRegex().String())
		} else if len(m) == 3 && m[2] != "" {
			// [0]: "exec"
			// [1]: ""
			// [2]: ""
			if strings.Contains(m[2], "-") {
				return CreateErr(nil, nil, "execution port '%v' must not contain hyphens", portId)
			}
		}
	} else if !portDef.Exec {
		if m != nil {
			return CreateErr(nil, nil, "port '%v' starts with 'exec-' but is not flagged as exec", portId)
		} else if strings.Contains(portId, "-") {
			return CreateErr(nil, nil, "port '%v' must not contain hyphens", portId)
		}
	}

	return nil
}

func RegisterNodeFactory(nodeDefStr string, fn nodeFactoryFunc) error {

	var nodeDef NodeTypeDefinitionFull
	err := yaml.Unmarshal([]byte(nodeDefStr), &nodeDef)
	if err != nil {
		return err
	}

	if strings.Contains(nodeDef.Id, "_") {
		return CreateErr(nil, nil, "id '%v' must not contain underscores", nodeDef.Id)
	}

	err = nodeDef.IsValid()
	if err != nil {
		return err
	}

	outputIndexes := make(map[int]string)
	inputIndexes := make(map[int]string)

	// count the execution inputs. If there is more than one, the required
	// field must be manually defined, otherwise its automatically set
	// to all exec inputs.
	countExec := 0
	requiredExecFound := false
	for _, inputDef := range nodeDef.Inputs {
		if inputDef.Exec {
			countExec++
			if inputDef.Required {
				requiredExecFound = true
			}
		}
	}
	if countExec == 0 || countExec == 1 {
		for inputId, inputDef := range nodeDef.Inputs {
			if inputDef.Exec {
				inputDef.Required = true
				nodeDef.Inputs[inputId] = inputDef
			}
		}
	} else if countExec > 1 && !requiredExecFound {
		return CreateErr(nil, nil, "node '%v' has multiple execution inputs but none is required, please mark at least one as required", nodeDef.Id)
	}

	// Increase the index gap between the input ports
	// to make space for sub ports
	for inputId, inputDef := range nodeDef.Inputs {
		prev, exists := inputIndexes[inputDef.Index]
		if exists {
			return CreateErr(nil, nil, "duplicate input index in %v at '%v' / '%v'", nodeDef.Name, inputId, prev)
		}

		// The rule is that certain inputs like numbers can never be empty due to their input fields which have at least the value `0`.
		// I also excluded arrays for now but it could make sense to visually mark them as required as well in the editor sometime in the future.
		if !inputDef.Exec && inputDef.Required && (inputDef.Type == "number" || strings.HasPrefix(inputDef.Type, "[]") || inputDef.Array) {
			return CreateErr(nil, nil, "the following input '%v.%v' cannot be marked as required. Please remove the required flag.", nodeDef.Name, inputId)
		}

		if nodeDef.Id != "core/test" {
			err = PortDefValidation(string(inputId), inputDef.PortDefinition)
			if err != nil {
				return CreateErr(nil, err, "input '%v' is invalid", inputId)
			}
		}

		inputIndexes[inputDef.Index] = string(inputId)

		if inputDef.Required && inputDef.Default != nil {
			// not allowed, make a decision if the input has a default value or is required
			return CreateErr(nil, nil, "input '%v' is flagged as required but has a default value", inputId)
		}

		switch inputDef.Type {
		case "boolean":
			return CreateErr(nil, nil, "input '%v' has type 'boolean', use 'bool' instead", inputId)
		case "option":
			if inputDef.Default == nil {
				return CreateErr(nil, nil, "input '%v' must have a default value", inputId)
			}
			for _, option := range inputDef.Options {
				if option.Name == "" {
					return CreateErr(nil, nil, "option name is missing in input '%v'", inputId)
				} else if option.Value == "" {
					return CreateErr(nil, nil, "option value is missing in input '%v'", inputId)
				} else if strings.ToLower(option.Value) != option.Value {
					return CreateErr(nil, nil, "option value must be lowercase in input '%v'", inputId)
				}
			}

		}

		nodeDef.Inputs[inputId] = inputDef
	}

	// Increase the gap between the output ports
	// to make space for sub ports
	for outputId, outputDef := range nodeDef.Outputs {
		prev, exists := outputIndexes[outputDef.Index]
		if exists {
			return CreateErr(nil, nil, "duplicate output index in %v at '%v' / '%v'", nodeDef.Name, outputId, prev)
		}

		outputIndexes[outputDef.Index] = string(outputId)

		if nodeDef.Id != "core/test" {
			err = PortDefValidation(string(outputId), outputDef.PortDefinition)
			if err != nil {
				return CreateErr(nil, err, "input '%v' is invalid", outputId)
			}
		}

		switch outputDef.Type {
		case "indexable", "iterable":
			return CreateErr(nil, nil, "output cannot be of type '%s', use 'unknown' or use the correct array type (e.g. []string) instead", outputDef.Type)
		case "any":
			return CreateErr(nil, nil, "output cannot be of type 'any', use 'unknown' instead")
		}

		nodeDef.Outputs[outputId] = outputDef
	}

	id := fmt.Sprintf("%v@v%v", nodeDef.Id, nodeDef.Version)
	_, ok := registries[id]
	if ok {
		return CreateErr(nil, nil, "node definition '%v' already registered", nodeDefStr)
	}

	nodeDef.FactoryFn = fn
	registries[id] = nodeDef

	return nil
}

func NewGhActionNode(nodeType string, parent NodeBaseInterface, parentId string, validate bool) (NodeBaseInterface, []error) {
	factoryEntry, exists := registries["core/gh-action@v1"]
	if !exists {
		return nil, []error{CreateErr(nil, nil, "node type '%v' not registered", nodeType)}
	}

	node, errs := factoryEntry.FactoryFn(nodeType, parent, parentId, nil, validate)
	if len(errs) > 0 {
		return nil, errs
	}

	utils.InitMapAndSliceInStructRecursively(reflect.ValueOf(node))

	return node, nil
}

func NewNodeInstance(nodeType string, parent NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (NodeBaseInterface, []error) {
	var (
		node NodeBaseInterface
		errs []error
	)

	// first versions of actionforge had no prefix `core/`
	matched, _ := regexp.MatchString(`^[\w-]+@v1$`, nodeType)
	if matched {
		nodeType = "core/" + nodeType
	}

	factoryEntry, exists := registries[nodeType]
	if exists {
		// Pass 'validate' to the factory function
		node, errs = factoryEntry.FactoryFn(nil, parent, parentId, nodeDef, validate)
		if len(errs) > 0 {
			// If the factory failed to produce a node (or found errors), return them.
			return nil, errs
		}
	} else {
		return nil, []error{CreateErr(nil, nil, "unknown node type '%v'", nodeType)}
	}

	if factoryEntry.Inputs != nil {
		inputs, ok := node.(HasInputsInterface)
		if ok {
			inputs.SetInputDefs(factoryEntry.Inputs, SetDefsOpts{
				// the factory creator may have defined some input definitions
				// dynamically, so we need to preserve them and instead merge
				// them with the ones defined in the node definition
				AssignmentMode_Merge,
			})
		}
	}

	if factoryEntry.Outputs != nil {
		outputs, ok := node.(HasOutputsInterface)
		if ok {
			outputs.SetOutputDefs(factoryEntry.Outputs, SetDefsOpts{
				// the factory creator may have defined some input definitions
				// dynamically, so we need to preserve them and instead merge
				// them with the ones defined in the node definition
				AssignmentMode_Merge,
			})
		}
	}

	// Ensure that the factory function returned a pointer
	if reflect.TypeOf(node).Kind() != reflect.Ptr {
		return nil, []error{CreateErr(nil, nil, "factory function for '%v' must return a pointer, did you forget '&' in front of the return type?", nodeType)}
	}

	utils.InitMapAndSliceInStructRecursively(reflect.ValueOf(node))
	node.SetNodeType(nodeType)
	node.SetName(factoryEntry.Name)
	node.SetParent(parent)
	return node, nil
}

func GlobFilter(path string, pattern []string) (bool, error) {
	// no pattern means everything is included
	if pattern == nil {
		return true, nil
	}

	for _, p := range pattern {
		// empty pattern matches everything
		if p == "" {
			return true, nil
		}

		// filepath.Match uses backslashes on Windows, so we need to normalize the pattern first
		normalizedPattern := strings.ReplaceAll(p, "/", string(filepath.Separator))

		matched, err := filepath.Match(normalizedPattern, filepath.Base(path))
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func CheckIfBuildExpired() (bool, error) {
	buildTime := build.GetBuildTime()
	if buildTime == "" {
		utils.LogOut.Debug("no build time found, skipping expiry check\n")
		return false, nil
	}

	buildTimeParsed, err := time.Parse(time.RFC3339, buildTime)
	if err != nil {
		return false, err
	}

	expiryTime := buildTimeParsed.AddDate(0, 0, expiryDays)

	if time.Now().After(expiryTime) {
		utils.LogOut.Debugln("build has expired")
		return true, nil
	}

	utils.LogOut.Debug("build hasn't expired yet\n")

	return false, nil
}

func RecoverHandler(repanic bool) {
	err := recover()
	if err != nil {

		fmt.Println("🐛 Oops! The process crashed. Please report this issue via:")
		fmt.Println(" 	📧 hello@actionforge.dev")
		fmt.Println(" 	🔗 https://www.actionforge.dev/join-discord")
		fmt.Println()

		stack := make([]uintptr, 256)
		runtime.Callers(0, stack[:])
		fmt.Println(GetStacktrace(stack))

		if repanic {
			panic(err)
		}
	}
}

func IsTestE2eRunning() bool {
	val := strings.ToLower(os.Getenv("ACT_TESTE2E"))
	return val == "1" || val == "true"
}
