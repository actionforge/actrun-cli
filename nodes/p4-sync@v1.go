//go:build p4

package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed p4-sync@v1.yml
var p4SyncDefinition string

type P4SyncNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *P4SyncNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	depotPath, err := core.InputValueById[string](c, n, ni.Core_p4_sync_v1_Input_depot_path)
	if err != nil {
		return err
	}

	creds, _ := core.InputValueById[core.Credentials](c, n, ni.Core_p4_sync_v1_Input_credentials)
	client, _ := core.InputValueById[string](c, n, ni.Core_p4_sync_v1_Input_client)
	force, _ := core.InputValueById[bool](c, n, ni.Core_p4_sync_v1_Input_force)

	fields := buildP4Fields(c, creds)
	if client != "" {
		fields.Client = client
	}

	p4, err := connectP4(c, fields)
	if err != nil {
		return n.Execute(ni.Core_p4_sync_v1_Output_exec_err, c, err)
	}
	defer func() {
		p4.Disconnect()
		p4.Close()
	}()

	var syncArgs []string
	if force {
		syncArgs = append(syncArgs, "-f")
	}
	syncArgs = append(syncArgs, depotPath)

	results, runErr := p4.Run("sync", syncArgs...)
	output := formatP4Results(results)

	_ = n.SetOutputValue(c, ni.Core_p4_sync_v1_Output_output, output, core.SetOutputValueOpts{})

	if runErr != nil {
		syncErr := core.CreateErr(c, runErr, "p4 sync failed")
		return n.Execute(ni.Core_p4_sync_v1_Output_exec_err, c, syncErr)
	}
	return n.Execute(ni.Core_p4_sync_v1_Output_exec_success, c, nil)
}

func init() {
	err := core.RegisterNodeFactory(p4SyncDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool, opts core.RunOpts) (core.NodeBaseInterface, []error) {
		return &P4SyncNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
