package nodes

import (
	_ "embed"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed storage-walk@v1.yml
var s3ListDefinition string

type StorageListNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *StorageListNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {

	provider, err := core.InputValueById[StorageListProvider](c, n, ni.Core_storage_walk_v1_Input_provider)
	if err != nil {
		return err
	}

	dir, err := core.InputValueById[string](c, n, ni.Core_storage_walk_v1_Input_dir)
	if err != nil {
		return err
	}

	glob, err := core.InputValueById[string](c, n, ni.Core_dir_walk_v1_Input_glob)
	if err != nil {
		return err
	}

	var pattern []string
	if glob != "" {
		pattern = strings.Split(glob, ";")
	}

	list, listErr := provider.ListObjects(dir)
	if listErr != nil {
		listErr = core.CreateErr(c, listErr, "failed to list objects")
	}

	if listErr != nil {
		err = n.Outputs.SetOutputValue(c, ni.Core_storage_walk_v1_Output_provider, provider, core.SetOutputValueOpts{})
		if err != nil {
			return err
		}

		err = n.Outputs.SetOutputValue(c, ni.Core_storage_walk_v1_Output_objects, []string{}, core.SetOutputValueOpts{})
		if err != nil {
			return err
		}

		err = n.Outputs.SetOutputValue(c, ni.Core_storage_walk_v1_Output_dirs, []string{}, core.SetOutputValueOpts{})
		if err != nil {
			return err
		}

		err = n.Execute(ni.Core_storage_walk_v1_Output_exec_err, c, listErr)
		if err != nil {
			return err
		}
	} else {

		objects := make([]string, 0, len(list.Objects))
		for _, obj := range list.Objects {
			include, err := core.GlobFilter(obj, pattern)
			if err != nil {
				return err
			}

			if include {
				objects = append(objects, obj)
			}
		}

		dirs := make([]string, 0, len(list.Dirs))
		for _, dir := range list.Dirs {
			include, err := core.GlobFilter(dir, pattern)
			if err != nil {
				return err
			}

			if include {
				dirs = append(dirs, dir)
			}
		}

		err = n.Outputs.SetOutputValue(c, ni.Core_storage_walk_v1_Output_provider, provider, core.SetOutputValueOpts{})
		if err != nil {
			return err
		}

		err = n.Outputs.SetOutputValue(c, ni.Core_storage_walk_v1_Output_objects, objects, core.SetOutputValueOpts{})
		if err != nil {
			return err
		}

		err = n.Outputs.SetOutputValue(c, ni.Core_storage_walk_v1_Output_dirs, dirs, core.SetOutputValueOpts{})
		if err != nil {
			return err
		}

		err = n.Execute(ni.Core_storage_walk_v1_Output_exec_success, c, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(s3ListDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &StorageListNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
