package nodes

import (
	_ "embed"
	"reflect"

	"github.com/actionforge/actrun-cli/core"
	"github.com/actionforge/actrun-cli/utils"

	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed property-setter@v1.yml
var propertySetterDefinition string

type PropertySetterNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions
}

func (n *PropertySetterNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	object, err := core.InputValueById[map[string]any](c, n, ni.Core_property_setter_v1_Input_object)
	if err != nil {
		return err
	}

	propertyPath, err := core.InputValueById[string](c, n, ni.Core_property_setter_v1_Input_path)
	if err != nil {
		return err
	}

	replaceWithThis, err := core.InputValueById[any](c, n, ni.Core_property_setter_v1_Input_value)
	if err != nil {
		return err
	}

	clone, err := deepCopy(object)
	if err != nil {
		return core.CreateErr(c, err, "failed to copy dictionary")
	}

	// There are a few internal types that we don't set as-is.
	switch v := replaceWithThis.(type) {
	case core.DataStreamFactory:
		replaceWithThis, err = core.ConvertToString(c, reflect.ValueOf(v))
		if err != nil {
			return err
		}
	}

	cloneVal, setErr := utils.SetPropertyByPath(clone, propertyPath, replaceWithThis)

	if setErr != nil {
		err = n.Execute(ni.Core_property_setter_v1_Output_exec_err, c, setErr)
		if err != nil {
			return err
		}
	} else {
		err = n.SetOutputValue(c, ni.Core_property_setter_v1_Output_object, cloneVal, core.SetOutputValueOpts{})
		if err != nil {
			return err
		}
		err = n.Execute(ni.Core_property_setter_v1_Output_exec_success, c, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(propertySetterDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &PropertySetterNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
