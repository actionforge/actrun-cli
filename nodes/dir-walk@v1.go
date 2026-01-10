package nodes

import (
	_ "embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"

	"golang.org/x/exp/maps"
)

//go:embed dir-walk@v1.yml
var walkDefinition string

type WalkNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions
}

func (n *WalkNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {

	glob, err := core.InputValueById[string](c, n, ni.Core_dir_walk_v1_Input_glob)
	if err != nil {
		return err
	}

	path, err := core.InputValueById[string](c, n, ni.Core_dir_walk_v1_Input_path)
	if err != nil {
		return err
	}

	recursive, err := core.InputValueById[bool](c, n, ni.Core_dir_walk_v1_Input_recursive)
	if err != nil {
		return err
	}

	dirs, err := core.InputValueById[bool](c, n, ni.Core_dir_walk_v1_Input_dirs)
	if err != nil {
		return err
	}

	files, err := core.InputValueById[bool](c, n, ni.Core_dir_walk_v1_Input_files)
	if err != nil {
		return err
	}

	var pattern []string
	if glob != "" {
		pattern = strings.Split(glob, ";")
	}

	absPaths := make(map[string]os.FileInfo)

	if path == "" {
		return core.CreateErr(c, nil, "dir path is empty")
	}

	path, walkErr := walk(path, walkOpts{
		recursive: recursive,
		files:     files,
		dirs:      dirs,
	}, pattern, absPaths)

	relPaths := make([]string, 0, len(absPaths))
	for p := range absPaths {
		rel, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		relPaths = append(relPaths, rel)
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_dir_walk_v1_Output_items_abs, maps.Keys(absPaths), core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_dir_walk_v1_Output_items_rel, relPaths, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	if walkErr != nil {
		err := n.Execute(ni.Core_dir_walk_v1_Output_exec_err, c, walkErr)
		if err != nil {
			return err
		}
	} else {
		err := n.Execute(ni.Core_dir_walk_v1_Output_exec_success, c, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

type walkOpts struct {
	recursive bool
	files     bool
	dirs      bool
}

func walk(root string, opts walkOpts, pattern []string, items map[string]os.FileInfo) (string, error) {

	var err error

	if root == "." {
		root, err = os.Getwd()
		if err != nil {
			return "", core.CreateErr(nil, err, "failed to get current working directory")
		}
	}

	root, err = filepath.Abs(root)
	if err != nil {
		return "", core.CreateErr(nil, err, "failed to get absolute path")
	}

	if opts.recursive {
		return root, filepath.WalkDir(root, func(path string, info fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if path == root {
				return nil
			}

			include, err := core.GlobFilter(path, pattern)
			if err != nil {
				return err
			}

			if include {
				if (info.IsDir() && opts.dirs) || (!info.IsDir() && opts.files) {
					items[path], _ = info.Info()
				}
			}

			return nil
		})

	} else {
		entries, err := os.ReadDir(root)
		if err != nil {
			return "", core.CreateErr(nil, err, "failed to read directory")
		}

		for _, entry := range entries {
			path := filepath.Join(root, entry.Name())

			if path == root {
				continue
			}

			include, err := core.GlobFilter(path, pattern)
			if err != nil {
				return "", err
			}

			if include {
				if (entry.IsDir() && opts.dirs) || (!entry.IsDir() && opts.files) {
					items[path], _ = entry.Info()
				}
			}
		}

		return root, nil
	}
}

func init() {
	err := core.RegisterNodeFactory(walkDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &WalkNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
