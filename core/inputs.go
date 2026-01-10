package core

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/actionforge/actrun-cli/utils"

	"golang.org/x/exp/maps"
)

var (
	// interfaces
	ioPipeReaderFactoryType = reflect.TypeOf((*io.PipeReader)(nil))
	ioReaderType            = reflect.TypeOf((*io.Reader)(nil)).Elem()
	iterableType            = reflect.TypeOf((*Iterable)(nil)).Elem()
	indexableType           = reflect.TypeOf((*Indexable)(nil)).Elem()
	storageProviderType     = reflect.TypeOf((*StorageProvider)(nil)).Elem()
	credentialsType         = reflect.TypeOf((*Credentials)(nil)).Elem()

	// structs
	secretValueType       = reflect.TypeOf(SecretValue{})
	dataStreamFactoryType = reflect.TypeOf(DataStreamFactory{})
	gitRepository         = reflect.TypeOf(GitRepository{})
	reflectValueType      = reflect.TypeOf(reflect.Value{})
)

var typeLabels = map[reflect.Type]string{
	//interfaces
	ioPipeReaderFactoryType: "Data Stream",
	ioReaderType:            "Data Stream",
	iterableType:            "Iterable",
	indexableType:           "Indexable",
	storageProviderType:     "Storage Provider",
	credentialsType:         "Credentials",

	// structs
	secretValueType:       "Secret",
	dataStreamFactoryType: "Data Stream",
	gitRepository:         "Git Repo",
	reflectValueType:      "value",
}

var defaultZeroValues = map[string]any{
	"bool":             false,
	"number":           0,
	"string":           "",
	"secret":           "",
	"credentials":      nil,
	"any":              nil,
	"stream":           nil,
	"git-repo":         nil,
	"unknown":          nil,
	"storage-provider": nil,
	"[]bool":           []bool{},
	"[]number":         []float64{},
	"[]string":         []string{},
	"map[string]any":   map[string]any{},
}

type DataSource struct {
	SrcNode            NodeBaseInterface
	DstNode            NodeBaseInterface
	SrcNodeOutputs     HasOutputsInterface
	SrcOutputId        OutputId
	SrcIndexOutputInfo *IndexPortInfo
}

type NodeWithInputs interface {
	NodeBaseInterface
	HasInputsInterface
}

type GetInputValueOpts struct {
	// If set, only request the value from the connection at the specified index.
	Index *int
}

type ConnectOpts struct {
	SkipValidation bool
}

// HasInputsInterface is a representation for all inputs of a node.
// The node that implements this interface has incoming connections.
type HasInputsInterface interface {
	InputDefsClone() map[InputId]InputDefinition
	InputDefByPortId(inputId string) (InputDefinition, *IndexPortInfo, bool)
	SetInputDefs(inputs map[InputId]InputDefinition, opts SetDefsOpts)
	GetInputDefs() map[InputId]InputDefinition
	GetInputIndexPorts() map[string]IndexPortInfo

	InputValueById(c *ExecutionState, host NodeWithInputs, inputId InputId, group *InputId) (value any, err error)
	SetInputValue(inputId InputId, value any) error
	AddSubInput(portId string, groupPortId string, portIndex int) error

	ConnectDataPort(outputNode NodeBaseInterface, outputPortId string, inputNode NodeBaseInterface, inputPortId string, parent NodeBaseInterface, opts ConnectOpts) error
}

type Inputs struct {
	inputIndexPorts        map[string]IndexPortInfo
	inputDefs              map[InputId]InputDefinition
	inputValues            map[InputId]any
	incomingDataConnection map[InputId]DataSource
}

func (n *Inputs) GetInputIndexPorts() map[string]IndexPortInfo {
	return n.inputIndexPorts
}

func (n *Inputs) GetDataSource(inputId InputId) (DataSource, bool) {
	ds, ok := n.incomingDataConnection[inputId]
	return ds, ok
}

func (n *Inputs) InputDefsClone() map[InputId]InputDefinition {
	return maps.Clone(n.inputDefs)
}

func (n *Inputs) AddSubInput(portId string, groupPortId string, portIndex int) error {

	// simple test, proper test should be done by caller by using `IsValidIndexPortId`
	if !strings.Contains(portId, "[") {
		return CreateErr(nil, nil, "port '%s' is not a sub port", portId)
	}

	if n.inputIndexPorts == nil {
		n.inputIndexPorts = make(map[string]IndexPortInfo)
	}

	groupInputDef, exists := n.inputDefs[InputId(groupPortId)]
	if !exists {
		return CreateErr(nil, nil, "port '%s' does not exist", groupPortId)
	}

	if !groupInputDef.Array {
		return CreateErr(nil, nil, "port '%s' is not an array input", groupPortId)
	}

	n.inputIndexPorts[portId] = IndexPortInfo{
		IndexPortId: portId,
		ArrayPortId: groupPortId,
		Index:       portIndex,
	}
	return nil
}

func (n *Inputs) InputDefByPortId(inputId string) (InputDefinition, *IndexPortInfo, bool) {
	indexPort, ok := n.inputIndexPorts[inputId]
	if !ok {
		inputDef, ok := n.inputDefs[InputId(inputId)]
		return inputDef, nil, ok
	}

	inputDef, ok := n.inputDefs[InputId(indexPort.ArrayPortId)]
	if !ok {
		// must never happen since `inputIndexPorts` is
		// only filled with existing output ports
		panic("array output port not found")
	}

	return inputDef, &indexPort, true
}

func (n *Inputs) ConnectDataPort(outputNode NodeBaseInterface, outputPortId string, inputNode NodeBaseInterface, inputPortId string, parent NodeBaseInterface, opts ConnectOpts) error {
	if outputNode == nil {
		panic("cannot connect data port with nil source node")
	} else if inputNode == nil {
		panic("cannot connect data port with nil destination node")
	}

	if n.incomingDataConnection == nil {
		n.incomingDataConnection = make(map[InputId]DataSource)
	}

	srcOutputNode, ok := outputNode.(HasOutputsInterface)
	if !ok {
		return CreateErr(nil, nil, "source node '%s' (%s) has no outputs", outputNode.GetName(), outputNode.GetId())
	}

	dstInputNode, ok := inputNode.(HasInputsInterface)
	if !ok {
		return CreateErr(nil, nil, "dst node '%s' (%s) has no inputs", inputNode.GetName(), inputNode.GetId())
	}

	var (
		outputDef       OutputDefinition
		inputDef        InputDefinition
		indexOutputInfo *IndexPortInfo
		indexInputInfo  *IndexPortInfo
	)

	if !opts.SkipValidation {

		var (
			si *IndexPortInfo
			ok bool
		)

		// If the source node is a core/group-inputs node, we need to get the
		// input definition from the parent (which is the group node) a level above.
		if strings.HasPrefix(outputNode.GetNodeTypeId(), "core/group-inputs@") {
			groupInputsNode := parent.(HasInputsInterface)
			inputDef, si, ok = groupInputsNode.InputDefByPortId(outputPortId)
			if ok {
				outputDef.PortDefinition = inputDef.PortDefinition
			}
		} else {
			outputDef, si, ok = srcOutputNode.OutputDefByPortId(outputPortId)
		}
		if !ok {
			return CreateErr(nil, nil, "source node '%s' (%s) has no output '%s'", outputNode.GetName(), outputNode.GetId(), outputPortId)
		}
		if si != nil {
			indexOutputInfo = si
		}

		// If the destination node is a core/group-outputs node, we need to get the
		// output definition from the parent (which is the group node) a level above.
		if strings.HasPrefix(inputNode.GetNodeTypeId(), "core/group-outputs@") {
			groupOutputsNode := parent.(HasOutputsInterface)
			outputDef, si, ok = groupOutputsNode.OutputDefByPortId(inputPortId)
			if ok {
				inputDef.PortDefinition = outputDef.PortDefinition
			}
		} else {
			inputDef, si, ok = dstInputNode.InputDefByPortId(inputPortId)
		}
		if !ok {
			return CreateErr(nil, nil, "destination node '%s' (%s) has no input '%s'", inputNode.GetName(), inputNode.GetId(), inputPortId)
		}
		if si != nil {
			indexInputInfo = si
		}

		outputPortType := PortType{PortType: outputDef.Type, Exec: outputDef.Exec}
		inputPortType := PortType{PortType: inputDef.Type, Exec: inputDef.Exec}

		if outputDef.Exec && inputDef.Exec {
			return CreateErr(nil, nil, "both ports are execution ports (%v.%v -> %v.%v)", outputNode.GetId(), outputPortId, inputNode.GetId(), inputPortId).SetHint("the connections must go to the `executions` section of the graph file")
		} else if outputDef.Exec {
			return CreateErr(nil, nil, "source port is an execution port, but destination port is a data port (%v.%v -> %v.%v)", outputNode.GetId(), outputPortId, inputNode.GetId(), inputPortId).SetHint("either the source port or the destination port must be changed to match the other")
		} else if inputDef.Exec {
			return CreateErr(nil, nil, "destination port is an execution port, but source port is a data port (%v.%v -> %v.%v)", outputNode.GetId(), outputPortId, inputNode.GetId(), inputPortId).SetHint("either the source port or the destination port must be changed to match the other")
		}

		if outputDef.Array && indexOutputInfo == nil {
			outputPortType.PortType = "[]" + outputPortType.PortType
		}

		if inputDef.Array && indexInputInfo == nil {
			inputPortType.PortType = "[]" + inputPortType.PortType
		}

		comp := PortsAreCompatible(outputPortType, inputPortType)
		if !comp {
			return CreateErr(nil, nil, "the ports between the node '%v'.'%v' and '%v'.'%v' are not compatible. (%v.%v -> %v.%v) (%v != %v)",
				outputNode.GetName(),
				outputDef.Name,
				inputNode.GetName(),
				inputDef.Name,
				outputNode.GetId(),
				outputPortId,
				inputNode.GetId(),
				inputPortId,
				outputPortType.PortType,
				inputPortType.PortType).SetHint("open the file in the graph editor and fix the connection between the two nodes")
		}
	}

	n.incomingDataConnection[InputId(inputPortId)] = DataSource{
		SrcNode:            outputNode,
		SrcNodeOutputs:     srcOutputNode,
		SrcOutputId:        OutputId(outputPortId),
		SrcIndexOutputInfo: indexOutputInfo,
		DstNode:            inputNode,
	}

	srcOutputNode.IncrementConnectionCounter(OutputId(outputPortId))

	if indexOutputInfo != nil {
		srcOutputNode.IncrementConnectionCounter(OutputId(indexOutputInfo.ArrayPortId))
	}
	return nil
}

func (n *Inputs) GetInputDefs() map[InputId]InputDefinition {
	return n.inputDefs
}

func (n *Inputs) SetInputDefs(inputDefs map[InputId]InputDefinition, opts SetDefsOpts) {
	if opts.AssignmentMode == AssignmentMode_Replace {
		n.inputDefs = inputDefs
	} else {
		if n.inputDefs == nil {
			n.inputDefs = make(map[InputId]InputDefinition)
		}

		maps.Copy(n.inputDefs, inputDefs)
	}
}

func (n *Inputs) SetInputValue(inputId InputId, value any) error {
	// TODO: (Seb) Ensure that only input values are set
	// that are defined in the node definition.

	if n.inputValues == nil {
		n.inputValues = make(map[InputId]any)
	}

	n.inputValues[inputId] = value

	return nil
}

func (n *Inputs) GetInputValues() map[InputId]any {
	return n.inputValues
}

func (n *Inputs) InputValueById(ec *ExecutionState, host NodeWithInputs, inputId InputId, inputArrayPortId *InputId) (value any, err error) {
	var finalValue any
	var inputDef InputDefinition
	var inputDefExists bool

	dataSource, connected := n.incomingDataConnection[inputId]
	if connected {
		var outputCacheIdForCache string
		if dataSource.SrcIndexOutputInfo != nil {
			outputCacheIdForCache = dataSource.SrcIndexOutputInfo.IndexPortId
		} else {
			outputCacheIdForCache = string(dataSource.SrcOutputId)
		}

		cacheType := dataSource.SrcNode.GetCacheType()
		srcIsGroupNode := strings.HasPrefix(dataSource.SrcNode.GetNodeTypeId(), "core/group@")
		srcIsGroupInputsNode := strings.HasPrefix(dataSource.SrcNode.GetNodeTypeId(), "core/group-inputs@")
		srcIsGroupOutputsNode := strings.HasPrefix(dataSource.SrcNode.GetNodeTypeId(), "core/group-outputs@")
		dstIsGroupInputsNode := strings.HasPrefix(dataSource.DstNode.GetNodeTypeId(), "core/group-inputs@")
		regardCache := !srcIsGroupNode && !srcIsGroupInputsNode && !srcIsGroupOutputsNode && !dstIsGroupInputsNode

		var ok bool
		if regardCache {
			finalValue, ok = ec.GetDataFromOutputCache(dataSource.SrcNode.GetCacheId(), outputCacheIdForCache, cacheType)
		}

		if ok {
			if utils.GetLogLevel() == utils.LogLevelDebug {
				utils.LogOut.Debugf("PushNodeVisit: (cached) %s, execute: %t\n", dataSource.SrcNode.GetId(), false)
			}
		} else {
			var outputCacheId string
			if dataSource.SrcIndexOutputInfo != nil {
				outputCacheId = dataSource.SrcIndexOutputInfo.ArrayPortId
			} else {
				outputCacheId = string(dataSource.SrcOutputId)
			}

			ec.PushNodeVisit(dataSource.SrcNode, false)
			v, err := dataSource.SrcNodeOutputs.OutputValueById(ec, OutputId(outputCacheId))
			ec.PopNodeVisit()
			if err != nil {
				return nil, err
			}

			// handle slice indexing
			if dataSource.SrcIndexOutputInfo != nil {
				slice := reflect.ValueOf(v)
				if slice.Kind() != reflect.Slice {
					return nil, CreateErr(ec, nil, "output '%v' is not a slice", dataSource.SrcOutputId)
				}

				if slice.Len() > dataSource.SrcIndexOutputInfo.Index {
					v = slice.Index(dataSource.SrcIndexOutputInfo.Index).Interface()
				} else if dataSource.SrcIndexOutputInfo.Index < 0 {
					return nil, CreateErr(ec, nil, "index '%d' is < 0", dataSource.SrcIndexOutputInfo.Index)
				} else {
					elementType := slice.Type().Elem()
					v = reflect.New(elementType).Elem().Interface()
				}
			}
			finalValue = v

			if regardCache {
				ec.CacheDataOutput(dataSource.SrcNode.GetCacheId(), outputCacheIdForCache, finalValue, cacheType)
			}
		}
	} else {
		// check first for user values, then defaults
		inputValue, userExists := n.inputValues[inputId]

		if inputArrayPortId != nil {
			inputDef, inputDefExists = n.inputDefs[*inputArrayPortId]
		} else {
			inputDef, inputDefExists = n.inputDefs[inputId]
		}

		if userExists && inputValue != nil {
			finalValue = inputValue
		} else if inputDefExists && inputDef.Default != nil {
			finalValue = inputDef.Default
		} else if inputDefExists && inputDef.Required {
			return nil, CreateErr(ec, &ErrNoInputValue{}, "no value for input '%v' (%s)", inputDef.Name, inputId)
		} else if inputDefExists {
			zeroValue, exists := defaultZeroValues[inputDef.Type]
			if exists {
				return zeroValue, nil
			}
		}
	}

	if finalValue == nil {
		if inputDefExists {
			return nil, CreateErr(ec, &ErrNoInputValue{}, "no value for input '%v' (%s)", inputDef.Name, inputId)
		}
		return nil, CreateErr(ec, &ErrNoInputValue{}, "unknown input '%v'", inputId)
	}

	// if the value is a string we have to evaluate any potential expressions `${{ ... }}`
	if strVal, ok := finalValue.(string); ok {
		finalValue, err = EvaluateToStringExpression(ec, strVal)
		if err != nil {
			return nil, CreateErr(ec, err, "unable to evaluate expression in input '%s'", inputId)
		}
	}

	if !inputDefExists {
		inputDef, inputDefExists = n.inputDefs[inputId]
	}
	if inputDefExists && inputDef.Type == "option" {
		switch c := finalValue.(type) {
		case string:
			finalValue = strings.Trim(c, " \n\r")
		case int8, int16, int32, int64, int, uint8, uint16, uint32, uint64, uint:
			nv := reflect.ValueOf(c).Int()
			if len(inputDef.Options) > 0 && int(nv) >= len(inputDef.Options) {
				return nil, CreateErr(ec, nil, "option value out of range: %v", nv)
			}
			finalValue = inputDef.Options[nv].Value
		}
	}

	if inputDef.Exec {
		return nil, CreateErr(ec, nil, "internal error because a value was requested from an execution port")
	}

	return finalValue, nil
}

// InputValueById returns the value of the input with the given id.
// An error is returned if the requested type does not match
// the type of the input value.
// For sub inputs use InputValueFromSubInputs.
func InputValueById[R any](tc *ExecutionState, n NodeWithInputs, inputId InputId) (R, error) {
	return inputValueById[R](tc, n, inputId, nil)
}

// InputValueFromSubInputs returns the value of a sub input from a port input with the given id.
// An error is returned if the requested type does not match the type of the input value.
// For non sub inputs use InputValueById.
func InputValueFromSubInputs[R any](tc *ExecutionState, n NodeWithInputs, inputId InputId, inputArrayPortId InputId) (R, error) {
	return inputValueById[R](tc, n, inputId, &inputArrayPortId)
}

func inputValueById[R any](tc *ExecutionState, n NodeWithInputs, inputId InputId, inputArrayPortId *InputId) (R, error) {
	var empty R
	v, err := n.InputValueById(tc, n, inputId, inputArrayPortId)
	if err != nil {
		return empty, err
	}

	// if no value provided, return default
	if v == nil {
		return empty, nil
	}

	typeOfRequested := reflect.TypeOf((*R)(nil)).Elem()
	typeOfValue := reflect.TypeOf(v)
	if typeOfRequested != typeOfValue {
		v, err = ConvertValue(tc, reflect.ValueOf(v), typeOfRequested)
		if err != nil {
			if typeOfRequested == secretValueType {
				// Special case for SecretValue conversion failures.
				// when the target is a SecretValue, a conversion failure usually means most likely that
				// the secret was not found. The first error line below is misleading:
				//
				// > unable to convert 'string' to 'core.SecretValue' at input 'access_key'
				// > ↳ no secret found for 'TESTE2E_DO_S3_ACCESS_KEY'
				//
				// So we simply return the last error here so the first error line is removed.
				return empty, err
			}
			return empty, CreateErr(tc, err, "unable to convert '%s' to '%s' at input '%s'", typeOfValue, typeOfRequested, inputId)
		}
	}

	casted, ok := v.(R)
	if !ok {
		if typeOfValue == nil {
			return empty, CreateErr(tc, err, "value for input '%v' is not of type '%v'", inputId, typeOfRequested)
		} else {
			return empty, CreateErr(tc, err, "value for input '%v' is not of type '%v' but '%v'", inputId, typeOfRequested, typeOfValue.String())
		}
	}
	return casted, nil
}

func InputArrayValueById[T any](ec *ExecutionState, node NodeWithInputs, inputId InputId, opts GetInputValueOpts) ([]T, error) {
	// Browse through all inputs of the node and collect all values
	// that belong to the requested input group.
	// An input belongs to the group if it has the same name and an index.
	// E.g: 'env[0]' where 'env' is the input name and '0' is the index.

	var incomingValues []T
	i, ok := node.GetInputDefs()[inputId]
	if !ok {
		return nil, CreateErr(ec, nil, "no input definition for input '%v'", inputId)
	}

	if !i.Array {
		return nil, CreateErr(ec, nil, "input '%v' is not an array input", inputId)
	}

	requestedIndexPorts := make([]IndexPortInfo, 0)

	for _, indexPortInfo := range node.GetInputIndexPorts() {
		if indexPortInfo.ArrayPortId == string(inputId) {
			requestedIndexPorts = append(requestedIndexPorts, indexPortInfo)
		}
	}

	sort.Slice(requestedIndexPorts, func(i, j int) bool {
		return requestedIndexPorts[i].Index < requestedIndexPorts[j].Index
	})

	incomingValues = make([]T, len(requestedIndexPorts))

	// TODO: (Seb) If we ever support array inputs having a socket,
	// then we need to query the value from the socket here and prefill
	// 'incomingValues' with the returned slice.

	for i, indexPortInfo := range requestedIndexPorts {
		if opts.Index == nil || *opts.Index == indexPortInfo.Index {
			v, err := InputValueFromSubInputs[T](ec, node, InputId(indexPortInfo.IndexPortId), inputId)
			if err != nil {
				ord := utils.Ordinal(indexPortInfo.Index)
				return nil, CreateErr(ec, err, "error when requesting input from '%s' (%s) %s at **%s** input port", node.GetName(), node.GetId(), inputId, ord)
			}
			incomingValues[i] = v
		}
	}

	return incomingValues, nil
}

func ConvertValueByType[T any](c *ExecutionState, v any) (T, error) {
	r, err := ConvertValue(c, reflect.ValueOf(v), reflect.TypeOf((*T)(nil)).Elem())
	if err != nil {
		var empty T
		return empty, err
	}
	return r.(T), err
}

func ConvertValue(c *ExecutionState, v reflect.Value, requestedType reflect.Type) (any, error) {
	if v.Type() == requestedType {
		return v.Interface(), nil
	}

	// dereference any pointers, e.g. for *string
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	switch requestedType.Kind() {
	case reflect.Bool:
		return convertToBool(c, v)
	case reflect.String:
		return ConvertToString(c, v)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return convertToNumber(c, v, requestedType)
	case reflect.Interface:
		if v.Type().Implements(requestedType) {
			return v.Interface(), nil
		} else if requestedType == ioReaderType {
			return convertToReader(c, v)
		} else if requestedType == iterableType {
			return convertToIterable(c, v)
		} else if requestedType == indexableType {
			return convertToIndexable(c, v)
		}
	case reflect.Struct:

		switch requestedType {
		case dataStreamFactoryType:
			reader, err := convertToReader(c, v)
			if err != nil {
				return nil, err
			}
			return DataStreamFactory{Reader: reader}, nil
		case reflectValueType:
			// It is common to request a value with the type `any`.
			// ... core.InputValueById[any](...)
			///
			// For performance and convenience reasons, also reflect.Value is supported.
			// ... core.InputValueById[reflect.Value](...)
			return v, nil
		case secretValueType:
			secretVal, ok := v.Interface().(string)
			if !ok {
				return nil, CreateErr(c, nil, "cannot convert '%s' to SecretValue", GetTypeNameSafe(v.Type()))
			}

			secret, ok := c.Secrets[secretVal]
			if !ok {
				return nil, CreateErr(c, nil, "no secret found for '%s'", secretVal).SetHint(
					"To learn about secrets, please visit https://docs.actionforge.dev/reference/configuration/#secrets",
				)
			}

			return SecretValue{Secret: secret}, nil
		}
	case reflect.Slice:
		if v.Kind() == reflect.Slice {
			slice := reflect.MakeSlice(requestedType, v.Len(), v.Len())
			for i := 0; i < v.Len(); i++ {
				convertedElem, err := ConvertValue(c, v.Index(i), requestedType.Elem())
				if err != nil {
					return nil, CreateErr(nil, err, "unable to convert element %d", i)
				}
				slice.Index(i).Set(reflect.ValueOf(convertedElem))
			}
			return slice.Interface(), nil
		}
		return nil, CreateErr(c, nil, "expected array but got %T", GetTypeNameSafe(v.Type()))
	}

	return nil, CreateErr(c, nil, "unsupported conversion to %s", requestedType)
}

func ConvertToString(c *ExecutionState, elem reflect.Value) (string, error) {
	if elem.Kind() == reflect.Interface && !elem.IsNil() {
		elem = elem.Elem()
	}

	switch elem.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(elem.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(elem.Uint(), 10), nil
	case reflect.Bool:
		return strconv.FormatBool(elem.Bool()), nil
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(elem.Float(), 'f', -1, 64), nil
	case reflect.String:
		return elem.String(), nil
	case reflect.Interface:
		if elem.Type().Implements(ioReaderType) {
			reader, err := convertToReader(c, elem)
			if err != nil {
				return "", err
			}
			bytes, err := io.ReadAll(reader)
			if err != nil {
				return "", CreateErr(c, nil, "error reading from io.Reader: %s", err)
			}

			str, err := utils.DecodeBytes(bytes)
			if err != nil {
				return "", CreateErr(c, nil, "error decoding bytes: %s", err)
			}

			return str, nil
		}
	case reflect.Map:
		var str strings.Builder
		for i, key := range elem.MapKeys() {
			if i > 0 {
				str.WriteString("\n")
			}
			keyStr, err := ConvertToString(c, key)
			if err != nil {
				return "", err
			}
			v, err := ConvertToString(c, elem.MapIndex(key))
			if err != nil {
				return "", err
			}
			str.WriteString(fmt.Sprintf("%s: %v", keyStr, v))
		}
		return str.String(), nil
	case reflect.Struct:
		dsf, ok := elem.Interface().(DataStreamFactory)
		if !ok {
			return "", CreateErr(c, nil, "cannot convert '%s' to string", elem.Kind())
		}

		bytes, err := io.ReadAll(dsf.Reader)
		if err != nil {
			dsf.CloseStreamAndIgnoreError()
			return "", CreateErr(c, nil, "error reading from io.Reader: %s", err)
		}

		err = dsf.CloseStream()
		if err != nil {
			return "", CreateErr(c, nil, "error closing reader: %s", err)
		}

		return string(bytes), nil
	}

	if elem.IsValid() {
		return "", CreateErr(c, nil, "cannot convert '%s' to string", GetTypeNameSafe(elem.Type()))
	}
	return "", CreateErr(c, nil, "cannot convert nil to string")
}

func convertToBool(c *ExecutionState, elem reflect.Value) (bool, error) {
	if elem.Kind() == reflect.Interface {
		elem = elem.Elem()
	}

	switch elem.Kind() {
	case reflect.Slice, reflect.Map, reflect.String:
		return elem.Len() > 0, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return bool(elem.Int() != 0), nil
	case reflect.Float32, reflect.Float64:
		return bool(elem.Float() != 0), nil
	case reflect.Bool:
		return elem.Bool(), nil
	default:
		return false, CreateErr(c, nil, "cannot convert %s to bool", elem.Kind())
	}
}

func convertToNumber(c *ExecutionState, value reflect.Value, requestedType reflect.Type) (any, error) {
	if !value.IsValid() {
		return nil, CreateErr(c, nil, "cannot convert invalid value to %s", requestedType.Name())
	}
	if value.Kind() == reflect.Interface {
		value = value.Elem()
	}

	for value.Kind() == reflect.Ptr {
		if value.IsNil() {
			// If we request a pointer type, we can return a nil pointer of that type.
			if requestedType.Kind() == reflect.Ptr {
				return reflect.Zero(requestedType).Interface(), nil
			}
			return nil, CreateErr(c, nil, "cannot convert nil pointer to non-pointer type %s", requestedType.Name())
		}
		value = value.Elem()
	}

	switch requestedType.Kind() {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var i64 int64
		switch value.Kind() {
		case reflect.String:
			i, err := strconv.ParseInt(strings.TrimSpace(value.String()), 10, 64)
			if err != nil {
				return nil, CreateErr(c, nil, "unable to convert string to int: %w", err)
			}
			i64 = i
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			i64 = value.Int()
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			u := value.Uint()
			if u > uint64(math.MaxInt64) {
				return nil, CreateErr(c, nil, "cannot convert unsigned value %d to signed int: overflow", u)
			}
			i64 = int64(u)
		case reflect.Float32, reflect.Float64:
			i64 = int64(value.Float())
		case reflect.Bool:
			if value.Bool() {
				i64 = 1
			} else {
				i64 = 0
			}
		default:
			return nil, CreateErr(c, nil, "cannot convert from %s to an integer type", value.Kind())
		}

		err := checkIntOverflow(i64, requestedType)
		if err != nil {
			return nil, CreateErr(c, err)
		}

		resultVal := reflect.New(requestedType).Elem()
		resultVal.SetInt(i64)
		return resultVal.Interface(), nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		var u64 uint64
		switch value.Kind() {
		case reflect.String:
			u, err := strconv.ParseUint(strings.TrimSpace(value.String()), 10, 64)
			if err != nil {
				return nil, CreateErr(c, nil, "unable to convert string to uint: %w", err)
			}
			u64 = u
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			i := value.Int()
			if i < 0 {
				return nil, CreateErr(c, nil, "cannot convert negative value %d to an unsigned int", i)
			}
			u64 = uint64(i)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			u64 = value.Uint()
		case reflect.Float32, reflect.Float64:
			f := value.Float()
			if f < 0 {
				return nil, CreateErr(c, nil, "cannot convert negative float %f to an unsigned int", f)
			}
			u64 = uint64(f)
		case reflect.Bool:
			if value.Bool() {
				u64 = 1
			}
		default:
			return nil, CreateErr(c, nil, "cannot convert from %s to an unsigned integer type", value.Kind())
		}

		err := checkUintOverflow(u64, requestedType)
		if err != nil {
			return nil, err
		}

		resultVal := reflect.New(requestedType).Elem()
		resultVal.SetUint(u64)
		return resultVal.Interface(), nil

	case reflect.Float32, reflect.Float64:
		var f64 float64
		switch value.Kind() {
		case reflect.String:
			f, err := strconv.ParseFloat(strings.TrimSpace(value.String()), 64)
			if err != nil {
				return nil, CreateErr(c, nil, "unable to convert string to float: %w", err)
			}
			f64 = f
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			f64 = float64(value.Int())
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			f64 = float64(value.Uint())
		case reflect.Float32, reflect.Float64:
			f64 = value.Float()
		case reflect.Bool:
			if value.Bool() {
				f64 = 1.0
			}
		default:
			return nil, CreateErr(c, nil, "cannot convert from %s to a float type", value.Kind())
		}

		if requestedType.Kind() == reflect.Float32 && (f64 > math.MaxFloat32 || f64 < -math.MaxFloat32) {
			return nil, CreateErr(c, nil, "value %f overflows float32", f64)
		}

		resultVal := reflect.New(requestedType).Elem()
		resultVal.SetFloat(f64)
		return resultVal.Interface(), nil

	case reflect.Bool:
		var result bool
		switch value.Kind() {
		case reflect.String:
			result = len(value.String()) > 0
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			result = value.Int() != 0
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			result = value.Uint() != 0
		case reflect.Float32, reflect.Float64:
			result = value.Float() != 0
		case reflect.Bool:
			result = value.Bool()
		default:
			// for other types we didn't define above
			result = !value.IsZero()
		}
		return result, nil

	case reflect.String:
		return fmt.Sprintf("%v", value.Interface()), nil

	default:
		return nil, CreateErr(c, nil, "unsupported requested conversion type: %s", requestedType.Kind())
	}
}

func checkIntOverflow(val int64, t reflect.Type) error {
	switch t.Kind() {
	case reflect.Int8:
		if val < math.MinInt8 || val > math.MaxInt8 {
			return fmt.Errorf("value %d overflows int8", val)
		}
	case reflect.Int16:
		if val < math.MinInt16 || val > math.MaxInt16 {
			return fmt.Errorf("value %d overflows int16", val)
		}
	case reflect.Int32:
		if val < math.MinInt32 || val > math.MaxInt32 {
			return fmt.Errorf("value %d overflows int32", val)
		}
	}
	return nil // int and int64 are fine as our source is int64
}

func checkUintOverflow(val uint64, t reflect.Type) error {
	switch t.Kind() {
	case reflect.Uint8:
		if val > math.MaxUint8 {
			return fmt.Errorf("value %d overflows uint8", val)
		}
	case reflect.Uint16:
		if val > math.MaxUint16 {
			return fmt.Errorf("value %d overflows uint16", val)
		}
	case reflect.Uint32:
		if val > math.MaxUint32 {
			return fmt.Errorf("value %d overflows uint32", val)
		}
	}
	return nil // uint and uint64 are fine as our source is uint64
}

func convertToReader(c *ExecutionState, value reflect.Value) (io.Reader, error) {
	switch v := value.Interface().(type) {
	case DataStreamFactory:
		return v.Reader, nil
	case string:
		return strings.NewReader(v), nil
	case []byte:
		return bytes.NewReader(v), nil
	default:
		return nil, CreateErr(c, nil, "unsupported type '%s'", GetTypeNameSafe(value.Type()))
	}
}

func convertToIndexable(c *ExecutionState, value reflect.Value) (Indexable, error) {
	switch value.Kind() {
	case reflect.Slice, reflect.String:
		return &IndexableImpl{Data: value}, nil
	}

	switch v := value.Interface().(type) {
	case DataStreamFactory:
		{
			bytes, err := io.ReadAll(v.Reader)
			if err != nil {
				return nil, CreateErr(c, err, "failed to read DataStreamFactory")
			}
			return &IndexableImpl{Data: reflect.ValueOf(bytes)}, nil
		}
	case Indexable:
		return v, nil
	default:
		return nil, CreateErr(c, nil, "unsupported type '%s'", reflect.TypeOf(value))
	}
}

func convertToIterable(c *ExecutionState, value reflect.Value) (Iterable, error) {
	switch value.Kind() {
	case reflect.Slice:
		return &SliceIterable{data: value}, nil
	case reflect.Map:
		return &MapIterable{data: value}, nil
	case reflect.String:
		return &StringIterable{data: value.String()}, nil
	}

	switch v := value.Interface().(type) {
	case DataStreamFactory:
		return &ReaderIterable{
			reader: v.Reader,
			buffer: make([]byte, 0),
			pos:    0,
		}, nil
	case Iterable:
		return v, nil
	default:
		return nil, CreateErr(c, nil, "unsupported type '%s'", reflect.TypeOf(value))
	}
}

func GetTypeNameSafe(t reflect.Type) string {
	if t == nil {
		return "<nil>"
	}

	label := typeLabels[t]
	if label != "" {
		return label
	}

	if t.Name() == "" {
		return t.String()
	}
	return t.Name()
}

func EvaluateToStringExpression(ctx *ExecutionState, raw string) (string, error) {
	evaluator := NewEvaluator(ctx)
	res, err := evaluator.Evaluate(raw)
	if err != nil {
		return "", err
	}

	// since this function expects a string result, treat nil as empty string
	if res == nil {
		return "", nil
	}

	strRes, ok := res.(string)
	if !ok {
		return "", fmt.Errorf("expression did not evaluate to a string: %T", res)
	}

	return strRes, nil
}

// ContextAdapter maps the evaluator's request for variables (e.g., "inputs.foo")
// to the actual data inside your ExecutionState.
type ContextAdapter struct {
	ExecCtx *ExecutionState
}

// GetSymbol is the method the Evaluator calls when it hits a variable like "inputs.param".
// It splits the name and looks it up in the correct map within ExecutionState.
func (c *ContextAdapter) GetSymbol(name string) (any, error) {
	parts := strings.SplitN(name, ".", 2)
	root := parts[0]

	// Select the correct data source based on the root scope
	switch strings.ToLower(root) {
	case "inputs":
		if len(parts) < 2 {
			return c.ExecCtx.Inputs, nil
		}
		return getFromMapOrEmpty(c.ExecCtx.Inputs, parts[1])
	case "env":
		if len(parts) < 2 {
			return c.ExecCtx.Env, nil
		}
		return getFromMapOrEmpty(c.ExecCtx.Env, parts[1])
	case "secrets":
		if len(parts) < 2 {
			return c.ExecCtx.Secrets, nil
		}
		return getFromMapOrEmpty(c.ExecCtx.Secrets, parts[1])
	case "github":
		// Handle github context lookups (run_id, sha, etc.)
		// Implementation depends on how you store github context data
		return nil, nil
	default:
		return nil, nil // Unknown context
	}
}

// Helper to handle nested lookups like "mykey" inside a map
func getFromMapOrEmpty[T any](data map[string]T, key string) (T, error) {
	// Basic lookup - could be expanded for nested objects (foo.bar.baz)
	if val, ok := data[key]; ok {
		return val, nil
	}
	var zero T
	return zero, nil // Return nil if not found (GHA standard behavior is often null string)
}
