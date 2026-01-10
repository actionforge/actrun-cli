package nodes

import (
	_ "embed"
	"runtime"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed system-info@v1.yml
var systemInfoDefinition string

type SystemInfoNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *SystemInfoNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	platformIndex := map[string]int{
		"windows": 0,
		"darwin":  1,
		"linux":   2,
	}

	archIndex := map[string]int{
		"amd64": 0,
		"arm64": 1,
	}

	switch outputId {
	case ni.Core_system_info_v1_Output_platform_string:
		switch runtime.GOOS {
		case "windows":
			return "windows", nil
		case "darwin":
			return "macos", nil
		case "linux":
			return "linux", nil
		default:
			return runtime.GOOS, nil
		}
	case ni.Core_system_info_v1_Output_arch_string:
		switch runtime.GOARCH {
		case "amd64":
			return "x64", nil
		case "arm64":
			return "arm64", nil
		default:
			return runtime.GOARCH, nil
		}
	case ni.Core_system_info_v1_Output_is_linux:
		return runtime.GOOS == "linux", nil
	case ni.Core_system_info_v1_Output_is_macos:
		return runtime.GOOS == "darwin", nil
	case ni.Core_system_info_v1_Output_is_win:
		return runtime.GOOS == "windows", nil
	case ni.Core_system_info_v1_Output_platform_index:
		return platformIndex[runtime.GOOS], nil
	case ni.Core_system_info_v1_Output_is_x64:
		return runtime.GOARCH == "amd64", nil
	case ni.Core_system_info_v1_Output_is_arm64:
		return runtime.GOARCH == "arm64", nil
	case ni.Core_system_info_v1_Output_arch_index:
		return archIndex[runtime.GOARCH], nil
	case ni.Core_system_info_v1_Output_cpu_count:
		return runtime.NumCPU(), nil
	default:
		return nil, core.CreateErr(c, nil, "unknown output id: '%s'", outputId)
	}
}

func init() {
	err := core.RegisterNodeFactory(systemInfoDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &SystemInfoNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
