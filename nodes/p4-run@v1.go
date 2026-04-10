//go:build p4

package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed p4-run@v1.yml
var p4RunDefinition string

type P4RunNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *P4RunNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	command, err := core.InputValueById[string](c, n, ni.Core_p4_run_v1_Input_command)
	if err != nil {
		return err
	}

	extraArgs, _ := core.InputValueById[[]string](c, n, ni.Core_p4_run_v1_Input_args)
	creds, _ := core.InputValueById[core.Credentials](c, n, ni.Core_p4_run_v1_Input_credentials)

	fields := buildP4Fields(c, creds)

	p4, err := connectP4(c, fields)
	if err != nil {
		_ = n.SetOutputValue(c, ni.Core_p4_run_v1_Output_exit_code, 1, core.SetOutputValueOpts{})
		return n.Execute(ni.Core_p4_run_v1_Output_exec_err, c, err)
	}
	defer func() {
		p4.Disconnect()
		p4.Close()
	}()

	results, runErr := p4.Run(command, extraArgs...)
	output := formatP4Results(results)

	_ = n.SetOutputValue(c, ni.Core_p4_run_v1_Output_output, output, core.SetOutputValueOpts{})

	if runErr != nil {
		_ = n.SetOutputValue(c, ni.Core_p4_run_v1_Output_exit_code, 1, core.SetOutputValueOpts{})
		cmdErr := core.CreateErr(c, runErr, "p4 %s failed", command)
		return n.Execute(ni.Core_p4_run_v1_Output_exec_err, c, cmdErr)
	}

	_ = n.SetOutputValue(c, ni.Core_p4_run_v1_Output_exit_code, 0, core.SetOutputValueOpts{})
	return n.Execute(ni.Core_p4_run_v1_Output_exec_success, c, nil)
}

func init() {
	err := core.RegisterNodeFactory(p4RunDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool, opts core.RunOpts) (core.NodeBaseInterface, []error) {
		return &P4RunNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
