package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	"github.com/actionforge/actrun-cli/utils"

	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed property-getter@v1.yml
var propertyGetterDefinition string

type PropertyGetterDefinition struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions
}

func (n *PropertyGetterDefinition) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	object, err := core.InputValueById[map[string]any](c, n, ni.Core_property_getter_v1_Input_object)
	if err != nil {
		return err
	}

	propertyPath, err := core.InputValueById[string](c, n, ni.Core_property_getter_v1_Input_path)
	if err != nil {
		return err
	}

	value, getErr := utils.GetPropertyByPath(object, propertyPath)

	err = n.SetOutputValue(c, ni.Core_property_getter_v1_Output_found, bool(getErr == nil), core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	if getErr != nil {
		err = n.Execute(ni.Core_property_getter_v1_Output_exec_err, c, getErr)
		if err != nil {
			return err
		}
	} else {
		err = n.SetOutputValue(c, ni.Core_property_getter_v1_Output_value, value, core.SetOutputValueOpts{})
		if err != nil {
			return err
		}

		err = n.Execute(ni.Core_property_getter_v1_Output_exec_success, c, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(propertyGetterDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &PropertyGetterDefinition{}, nil
	})
	if err != nil {
		panic(err)
	}
}
