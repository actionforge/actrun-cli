//go:build cpython

package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/api"
	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed run-python-embedded@v1.yml
var runExecPython string

type RunPythonEmbedded struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions
}

func (n *RunPythonEmbedded) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {

	script, err := core.InputValueById[string](c, n, ni.Core_run_python_embedded_v1_Input_script)
	if err != nil {
		return err
	}

	ret, runErr := api.RunPythonCodeRet(script)
	if runErr != nil {
		err = n.Execute(ni.Core_run_python_embedded_v1_Output_exec_err, c, runErr)
		if err != nil {
			return err
		}
	} else {
		if ret == nil {
			// default return is an empty string
			ret = ""
		}

		err = n.Outputs.SetOutputValue(c, ni.Core_run_python_embedded_v1_Output_return, ret, core.SetOutputValueOpts{})
		if err != nil {
			return err
		}

		err = n.Execute(ni.Core_run_python_embedded_v1_Output_exec_success, c, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func init() {
	err := core.RegisterNodeFactory(runExecPython, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &RunPythonEmbedded{}, nil
	})
	if err != nil {
		panic(err)
	}
}
