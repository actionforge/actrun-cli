package nodes

import (
	_ "embed"
	"runtime"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed switch-arch@v1.yml
var archSwitchDefinition string

type ArchSwitchNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Outputs
	core.Inputs
}

func (n *ArchSwitchNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {

	var err error

	switch runtime.GOARCH {
	case "amd64":
		err = n.Execute(ni.Core_switch_arch_v1_Output_exec_x64, c, nil)
	case "arm64":
		err = n.Execute(ni.Core_switch_arch_v1_Output_exec_arm64, c, nil)
	default:
		return core.CreateErr(c, nil, "unsupported platform: %s", runtime.GOOS)
	}

	if err != nil {
		return err
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(archSwitchDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &ArchSwitchNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
