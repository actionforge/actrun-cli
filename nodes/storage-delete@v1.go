package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed storage-delete@v1.yml
var storageDeleteDefinition string

type StorageDeleteNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *StorageDeleteNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {

	objectName, err := core.InputValueById[string](c, n, ni.Core_storage_delete_v1_Input_name)
	if err != nil {
		return err
	}

	provider, err := core.InputValueById[StorageDeleteProvider](c, n, ni.Core_storage_delete_v1_Input_provider)
	if err != nil {
		return err
	}

	deleteErr := provider.DeleteFile(objectName)
	if deleteErr != nil {
		deleteErr = core.CreateErr(c, deleteErr, "failed to delete object")
	}

	err = n.SetOutputValue(c, ni.Core_storage_delete_v1_Output_deleted, deleteErr == nil, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_storage_delete_v1_Output_provider, provider, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	if deleteErr != nil {
		err = n.Execute(ni.Core_storage_delete_v1_Output_exec_err, c, deleteErr)
		if err != nil {
			return err
		}
	} else {
		err = n.Execute(ni.Core_storage_delete_v1_Output_exec_success, c, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(storageDeleteDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &StorageDeleteNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
