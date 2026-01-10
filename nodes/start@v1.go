package nodes

import (
	_ "embed"
	"os"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed start@v1.yml
var startNodeDefinition string

type StartNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Outputs

	args []string
}

func (n *StartNode) ExecuteEntry(c *core.ExecutionState, outputValues map[core.OutputId]any, args []string) error {
	n.args = args

	dsf := core.DataStreamFactory{
		Reader: os.Stdin,
	}

	err := n.Outputs.SetOutputValue(c, ni.Core_start_v1_Output_stdin, dsf, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_start_v1_Output_env, os.Environ(), core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_start_v1_Output_args, n.args, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	return n.ExecuteImpl(c, "", nil)
}

func (n *StartNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {

	err := n.Execute(ni.Core_start_v1_Output_exec, c, nil)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(startNodeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &StartNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
