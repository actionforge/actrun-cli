//go:build p4

package nodes

import (
	_ "embed"
	"os"
	"path/filepath"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed p4-print@v1.yml
var p4PrintDefinition string

type P4PrintNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *P4PrintNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	depotPath, err := core.InputValueById[string](c, n, ni.Core_p4_print_v1_Input_depot_path)
	if err != nil {
		return err
	}

	outputPath, err := core.InputValueById[string](c, n, ni.Core_p4_print_v1_Input_output_path)
	if err != nil {
		return err
	}

	creds, _ := core.InputValueById[core.Credentials](c, n, ni.Core_p4_print_v1_Input_credentials)

	fields := buildP4Fields(c, creds)

	p4, err := connectP4(c, fields)
	if err != nil {
		return n.Execute(ni.Core_p4_print_v1_Output_exec_err, c, err)
	}
	defer func() {
		p4.Disconnect()
		p4.Close()
	}()

	// Ensure parent directory exists
	if dir := filepath.Dir(outputPath); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			mkdirErr := core.CreateErr(c, err, "failed to create output directory '%s'", dir)
			return n.Execute(ni.Core_p4_print_v1_Output_exec_err, c, mkdirErr)
		}
	}

	// p4 print -q -o <output> <depot_path>
	_, runErr := p4.Run("print", "-q", "-o", outputPath, depotPath)
	if runErr != nil {
		printErr := core.CreateErr(c, runErr, "p4 print failed for %s", depotPath)
		return n.Execute(ni.Core_p4_print_v1_Output_exec_err, c, printErr)
	}

	return n.Execute(ni.Core_p4_print_v1_Output_exec_success, c, nil)
}

func init() {
	err := core.RegisterNodeFactory(p4PrintDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool, opts core.RunOpts) (core.NodeBaseInterface, []error) {
		return &P4PrintNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
