package nodes

import (
	_ "embed"
	"os"
	"path/filepath"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed filepath-location@v1.yml
var filepathLocationDefinition string

type FilepathLocation struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *FilepathLocation) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	location, err := core.InputValueById[string](c, n, ni.Core_filepath_location_v1_Input_location)
	if err != nil {
		return nil, err
	}

	switch location {
	case "home":
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, core.CreateErr(c, nil, "unable to get home directory: %v", err)
		}
		return homeDir, nil
	case "current_graph":
		if c.GraphFile == "" {
			return nil, core.CreateErr(c, nil, "current graph path not found in context")
		}
		graphPath, err := filepath.Abs(c.GraphFile)
		if err != nil {
			return nil, core.CreateErr(c, nil, "unable to get absolute path of current graph")
		}
		return filepath.Clean(graphPath), nil
	case "temp_dir":
		return os.TempDir(), nil
	case "exe_dir":
		exePath, err := os.Executable()
		if err != nil {
			return nil, core.CreateErr(c, nil, "unable to get executable path: %v", err)
		}
		return filepath.Dir(exePath), nil
	case "root_dir":
		return string(filepath.Separator), nil
	case "working_dir":
		wd, err := os.Getwd()
		if err != nil {
			return nil, core.CreateErr(c, nil, "unable to get working directory: %v", err)
		}
		return wd, nil
	case "user_config_dir":
		configDir, err := os.UserConfigDir()
		if err != nil {
			return nil, core.CreateErr(c, nil, "unable to get user config directory: %v", err)
		}
		return configDir, nil
	case "desktop_dir":
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, core.CreateErr(c, nil, "unable to get home directory: %v", err)
		}

		desktop := filepath.Join(homeDir, "Desktop")
		_, err = os.Stat(desktop)
		if err != nil {
			return nil, core.CreateErr(c, nil, "no desktop directory found: %v", err)
		}
		return desktop, nil
	}

	return nil, core.CreateErr(c, nil, "unknown location '%s'", location)
}

func init() {
	err := core.RegisterNodeFactory(filepathLocationDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &FilepathLocation{}, nil
	})
	if err != nil {
		panic(err)
	}
}
