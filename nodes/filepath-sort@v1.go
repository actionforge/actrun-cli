package nodes

import (
	_ "embed"
	"path/filepath"
	"sort"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed filepath-sort@v1.yml
var filepathSortDefinition string

type FilepathSort struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *FilepathSort) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	paths, err := core.InputValueById[[]string](c, n, ni.Core_filepath_sort_v1_Input_paths)
	if err != nil {
		return nil, err
	}

	sortBy, err := core.InputValueById[string](c, n, ni.Core_filepath_sort_v1_Input_sort_by)
	if err != nil {
		return nil, err
	}

	switch sortBy {
	case "alphabetical":
		sort.Strings(paths)
	case "reverse_alphabetical":
		sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	case "by_extension":
		sort.Slice(paths, func(i, j int) bool {
			return filepath.Ext(paths[i]) < filepath.Ext(paths[j])
		})
	case "by_directory_depth":
		sort.Slice(paths, func(i, j int) bool {
			return len(strings.Split(filepath.ToSlash(paths[i]), "/")) < len(strings.Split(filepath.ToSlash(paths[j]), "/"))
		})
	case "by_filename_length":
		sort.Slice(paths, func(i, j int) bool {
			return len(filepath.Base(paths[i])) < len(filepath.Base(paths[j]))
		})
	default:
		return nil, core.CreateErr(c, nil, "unknown sorting option '%s'", sortBy)
	}

	return paths, nil
}

func init() {
	err := core.RegisterNodeFactory(filepathSortDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &FilepathSort{}, nil
	})
	if err != nil {
		panic(err)
	}
}
