package nodes

import (
	_ "embed"
	"errors"
	"os"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed item-stats@v1.yml
var itemStatsDefinition string

type ItemStatsNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *ItemStatsNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	path, err := core.InputValueById[string](c, n, ni.Core_file_read_v1_Input_path)
	if err != nil {
		return err
	}

	exists := true

	var (
		permissions int32
		size        int64
	)
	isRegular := false
	isDir := false

	stats, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			exists = false
		} else {
			return core.CreateErr(c, err, "error getting file stats")
		}
	}

	if stats != nil {
		isRegular = stats.Mode().IsRegular()
		isDir = stats.Mode().IsDir()
		size = stats.Size()
		perm := stats.Mode().Perm()
		// from oct to decimal
		permissions = int32((perm / 64 * 100) + ((perm % 64) / 8 * 10) + (perm % 8))
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_item_stats_v1_Output_exists, exists, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_item_stats_v1_Output_isdir, isDir, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_item_stats_v1_Output_isfile, isRegular, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_item_stats_v1_Output_permissions, permissions, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_item_stats_v1_Output_size, size, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	if exists {
		err = n.Execute(ni.Core_item_stats_v1_Output_exec_exists, c, nil)
		if err != nil {
			return err
		}
	} else {
		err = n.Execute(ni.Core_item_stats_v1_Output_exec_noexists, c, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(itemStatsDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &ItemStatsNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
