package nodes

import (
	_ "embed"
	"errors"
	"sync"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
	"github.com/actionforge/actrun-cli/utils"
)

//go:embed concurrent-exec@v1.yml
var concurrentExecDefinition string

type ConcurrentExecNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions
}

func (n *ConcurrentExecNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	wg := sync.WaitGroup{}

	var mutex sync.Mutex
	var firstError error

	for outputId, t := range n.Executions.Executions {
		if t.DstNode == nil {
			continue
		}

		outputIdCopy := outputId

		fn := func() {

			nti := c.PushNewExecutionState(n)
			err := n.Execute(outputIdCopy, nti, nil)
			if err != nil {
				c.Cancel()

				mutex.Lock()
				defer mutex.Unlock()

				if firstError == nil {
					firstError = err
				} else {
					firstError = errors.Join(firstError, err)
				}

				return
			}
		}

		if utils.ConcurrencyIsEnabled() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				fn()
			}()
		} else {
			fn()
		}
	}

	wg.Wait()

	if firstError != nil {
		return firstError
	}

	err := n.Execute(ni.Core_concurrent_exec_v1_Output_exec_completed, c, nil)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(concurrentExecDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &ConcurrentExecNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
