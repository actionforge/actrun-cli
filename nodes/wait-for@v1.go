package nodes

import (
	_ "embed"
	"sync"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
	"github.com/actionforge/actrun-cli/utils"
)

//go:embed wait-for@v1.yml
var waitForDefinition string

type WaitForNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs

	Lock sync.Mutex

	MergedEnv      map[string]string
	CurrentCounter int
}

func (n *WaitForNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {

	if inputId == ni.Core_wait_for_v1_Input_exec {

		loop, err := core.InputValueById[bool](c, n, ni.Core_wait_for_v1_Input_loop)
		if err != nil {
			return err
		}

		executeAfter, err := core.InputValueById[int](c, n, ni.Core_wait_for_v1_Input_after)
		if err != nil {
			return err
		}

		var skip bool

		n.Lock.Lock()
		switch n.CurrentCounter {
		case -1:
			n.CurrentCounter = executeAfter
		case 0:
			if loop {
				n.CurrentCounter = executeAfter
			} else {
				n.Lock.Unlock()
				return nil
			}
		}
		n.CurrentCounter--
		skip = n.CurrentCounter > 0
		n.MergedEnv = utils.MergeEnvMaps(c.GetContextEnvironMapCopy(), n.MergedEnv)

		finalEnv := n.MergedEnv

		// if we are about to fire the execution node, we must reset the
		// accumulated environment for the next loop/batch.
		if !skip {
			n.MergedEnv = make(map[string]string)
		}

		n.Lock.Unlock()

		if skip {
			return nil
		}

		// Use the captured local variable, not the struct field
		c.SetContextEnvironMap(finalEnv)

		err = n.Execute(ni.Core_wait_for_v1_Output_exec, c, nil)
		if err != nil {
			return err
		}

		return nil
	}
	return nil
}

func init() {
	err := core.RegisterNodeFactory(waitForDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &WaitForNode{
			Lock:           sync.Mutex{},
			CurrentCounter: -1,
		}, nil
	})
	if err != nil {
		panic(err)
	}
}
