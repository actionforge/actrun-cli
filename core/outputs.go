package core

import (
	"maps"
	"reflect"
	"strings"
)

type SetOutputValueOpts struct {
	NotExistsIsNoError bool
}

// HasOutputsInterface is a representation for all outputs of a node.
// The node that implements this interface has outgoing connections.
type HasOutputsInterface interface {
	OutputDefsClone() map[OutputId]OutputDefinition
	OutputDefByPortId(outputId string) (OutputDefinition, *IndexPortInfo, bool)
	SetOutputDefs(outputs map[OutputId]OutputDefinition, opts SetDefsOpts)

	OutputValueById(c *ExecutionState, outputId OutputId) (value any, err error)
	SetOutputValue(c *ExecutionState, outputId OutputId, value any, opts SetOutputValueOpts) error
	AddSubOutput(portId string, groupPortId string, portIndex int) error

	IncrementConnectionCounter(outputId OutputId)
	SetOwner(host NodeBaseInterface)
}

type Outputs struct {
	outputDefs              map[OutputId]OutputDefinition
	outputConnectionCounter map[OutputId]int64
	outputIndexPorts        map[string]IndexPortInfo
	owner                   NodeBaseInterface
}

func (n *Outputs) OutputDefByPortId(outputId string) (OutputDefinition, *IndexPortInfo, bool) {
	indexPort, ok := n.outputIndexPorts[outputId]
	if !ok {
		outputDef, ok := n.outputDefs[OutputId(outputId)]
		return outputDef, nil, ok
	}

	outputDef, ok := n.outputDefs[OutputId(indexPort.ArrayPortId)]
	if !ok {
		// must never happen since `outputIndexPorts` is
		// only filled with existing output ports
		panic("group output port not found")
	}

	return outputDef, &indexPort, true

}

func (n *Outputs) SetOwner(owner NodeBaseInterface) {
	n.owner = owner
}

func (n *Outputs) IncrementConnectionCounter(outputId OutputId) {
	if n.outputConnectionCounter == nil {
		n.outputConnectionCounter = make(map[OutputId]int64)
	}
	n.outputConnectionCounter[outputId]++
}

func (n *Outputs) OutputDefsClone() map[OutputId]OutputDefinition {
	return maps.Clone(n.outputDefs)
}

func (n *Outputs) SetOutputDefs(outputDefs map[OutputId]OutputDefinition, opts SetDefsOpts) {
	if opts.AssignmentMode == AssignmentMode_Replace {
		n.outputDefs = outputDefs
	} else {
		if n.outputDefs == nil {
			n.outputDefs = make(map[OutputId]OutputDefinition)
		}

		maps.Copy(n.outputDefs, outputDefs)
	}
}

func (n *Outputs) AddSubOutput(portId string, groupPortId string, portIndex int) error {

	// simple test, proper test should be done by caller by using `IsValidIndexPortId`
	if !strings.Contains(portId, "[") {
		return CreateErr(nil, nil, "port '%s' is not a sub port", portId)
	}

	if n.outputIndexPorts == nil {
		n.outputIndexPorts = make(map[string]IndexPortInfo)
	}

	groupOutputDef, exists := n.outputDefs[OutputId(groupPortId)]
	if !exists {
		return CreateErr(nil, nil, "port '%s' does not exist", groupPortId)
	}

	if !groupOutputDef.Array {
		return CreateErr(nil, nil, "port '%s' is not an array port", groupPortId)
	}

	n.outputIndexPorts[portId] = IndexPortInfo{
		IndexPortId: portId,
		ArrayPortId: groupPortId,
		Index:       portIndex,
	}
	return nil
}

func (n *Outputs) OutputValueById(c *ExecutionState, outputId OutputId) (any, error) {
	// If 'Outputs' belongs to a data node, then this method doesn't seem to be implemented.
	// If 'Outputs' belongs to a execution node, then the value hasn't been set yet.
	// Reminder, for execution nodes, once they are executed, all outputs have to be populated!
	return nil, CreateErr(c, &ErrNoOutputValue{}, "output port '%v' has no value", outputId)
}

// SetOutputValue sets the value of an output to the node.
// The value type must match the output type, otherwise an error
// is returned.
func (n *Outputs) SetOutputValue(ec *ExecutionState, outputId OutputId, value any, opts SetOutputValueOpts) error {
	outputDef, outputExists := n.outputDefs[outputId]
	if outputExists {
		expectedType := outputDef.Type
		if outputDef.Array {
			expectedType = "[]" + expectedType
		}

		if !isValueValidForOutput(value, expectedType) {
			return CreateErr(ec, nil, "output '%s' (%s): expected %v, but got %T", outputDef.Name, outputId, outputDef.Type, value)
		}
	} else {
		// if the output could not be found,
		// check if it is a sub port instead
		groupPortId, _, isIndexPort := IsValidIndexPortId(string(outputId))
		if !isIndexPort {
			if opts.NotExistsIsNoError {
				return nil
			}
			return CreateErr(ec, nil, "failed to set a value to an unknown port '%s'", outputId)
		}

		outputDef, outputExists = n.outputDefs[OutputId(groupPortId)]
		if !outputExists {
			if opts.NotExistsIsNoError {
				return nil
			}
			// If still nothing found, return an error
			return CreateErr(ec, nil, "failed to set a value to an unknown port '%s'", outputId)
		}

		if !isValueValidForOutput(value, outputDef.Type) {
			return CreateErr(ec, nil, "output '%s' (%s): expected %v, but got %T", outputDef.Name, outputId, outputDef.Type, value)
		}
	}

	// If the output is not connected, there's no need to keep the value. It can be discarded, unless
	// for debug sessions where we always keep the output value, as it will be transmitted to the client for inspection
	connectionCounter := n.outputConnectionCounter[outputId]
	if connectionCounter == 0 && !ec.IsDebugSession {
		// TODO: (Seb) If the value is a stream, we should close it here
		return nil
	}

	ec.CacheDataOutput(n.owner.GetCacheId(), string(outputId), value, Permanent)
	return nil
}

func isValueValidForOutput(value any, expectedType string) bool {
	if value == nil {
		return false
	}

	valueType := reflect.TypeOf(value)
	kind := valueType.Kind()

	if expectedType == "any" || expectedType == "unknown" {
		return true
	}

	_, mappingExists := validKindsForExpectedType[expectedType]
	if mappingExists {
		_, valid := validKindsForExpectedType[expectedType][kind]
		return valid
	}

	switch expectedType {
	case "[]string":
		return kind == reflect.Slice && valueType.Elem().Kind() == reflect.String
	case "[]number":
		return kind == reflect.Slice && isNumericType(valueType.Elem())
	case "[]bool":
		return kind == reflect.Slice && valueType.Elem().Kind() == reflect.Bool
	case "git-repo":
		return valueType == gitRepository
	case "stream":
		return kind == reflect.String || valueType == dataStreamFactoryType || valueType == ioPipeReaderFactoryType || (kind == reflect.Slice && valueType.Elem().Kind() == reflect.Uint8)
	case "storage-provider":
		return valueType.Implements(storageProviderType)
	case "credentials":
		return valueType.Implements(credentialsType)
	}

	return valueType.String() == expectedType
}

func isNumericType(valueType reflect.Type) bool {
	kind := valueType.Kind()
	_, valid := validKindsForExpectedType["number"][kind]
	return valid
}

var validKindsForExpectedType = map[string]map[reflect.Kind]struct{}{
	"iterable": {
		reflect.Slice:  {},
		reflect.Map:    {},
		reflect.String: {},
	},
	"string": {
		reflect.String: {},
	},
	"number": {
		reflect.Int:     {},
		reflect.Int8:    {},
		reflect.Int16:   {},
		reflect.Int32:   {},
		reflect.Int64:   {},
		reflect.Uint:    {},
		reflect.Uint8:   {},
		reflect.Uint16:  {},
		reflect.Uint32:  {},
		reflect.Uint64:  {},
		reflect.Float32: {},
		reflect.Float64: {},
	},
	"bool": {
		reflect.Bool: {},
	},
}
