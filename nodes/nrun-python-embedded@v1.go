//go:build !cpython

package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
)

//go:embed run-python-embedded@v1.yml
var runExecPython string

func init() {
	err := core.RegisterNodeFactory(runExecPython, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return nil, []error{core.CreateErr(nil, nil, "node 'Run Python Embedded' (%v) not available", nodeDef["id"]).SetHint(`the node can only be used if the action graph is run within a Python environment. For more information check the link below.
	https://docs.actionforge.dev/nodes/core/run-python-embedded/v1/#not-available`)}
	})
	if err != nil {
		panic(err)
	}
}
