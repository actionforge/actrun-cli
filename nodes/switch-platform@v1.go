package nodes

import (
	_ "embed"
	"runtime"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed switch-platform@v1.yml
var platformSwitchDefinition string

type PlatformSwitchNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Outputs
	core.Inputs
}

func (n *PlatformSwitchNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {

	var err error

	switch runtime.GOOS {
	case "windows":
		err = n.Execute(ni.Core_switch_platform_v1_Output_exec_win, c, nil)
	case "linux":
		err = n.Execute(ni.Core_switch_platform_v1_Output_exec_linux, c, nil)
	case "darwin":
		err = n.Execute(ni.Core_switch_platform_v1_Output_exec_macos, c, nil)
	default:
		return core.CreateErr(c, nil, "unsupported platform: %s", runtime.GOOS)
	}

	if err != nil {
		return err
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(platformSwitchDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &PlatformSwitchNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
