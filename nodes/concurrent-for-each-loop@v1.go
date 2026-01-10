package nodes

import (
	_ "embed"
	"errors"
	"sync"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
	"github.com/actionforge/actrun-cli/utils"
)

//go:embed concurrent-for-each-loop@v1.yml
var concurrentForEachLoopDefinition string

type ConcurrentForEachLoopNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions
}

func (n *ConcurrentForEachLoopNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	iterable, err := core.InputValueById[core.Iterable](c, n, ni.Core_concurrent_for_each_loop_v1_Input_input)
	if err != nil {
		return err
	}

	workerCount, err := core.InputValueById[int](c, n, ni.Core_concurrent_for_loop_v1_Input_worker_count)
	if err != nil {
		return err
	}

	if !utils.ConcurrencyIsEnabled() {
		workerCount = 1
	}

	taskCh := make(chan [2]any, workerCount)
	wg := sync.WaitGroup{}
	var mutex sync.Mutex
	var firstError error

	iter := func(key, value any) {
		taskCh <- [2]any{key, value}
	}

	for w := 0; w < workerCount; w++ {
		fn := func() {
			for task := range taskCh {
				key := task[0]
				value := task[1]

				if c.IsCancelled() {
					return
				}

				nti := c.PushNewExecutionState(n)
				err := n.Outputs.SetOutputValue(nti, ni.Core_concurrent_for_each_loop_v1_Output_key, key, core.SetOutputValueOpts{})
				if err != nil {

					mutex.Lock()
					defer mutex.Unlock()

					if firstError == nil {
						firstError = err
					}

					return
				}

				err = n.Outputs.SetOutputValue(nti, ni.Core_concurrent_for_each_loop_v1_Output_value, value, core.SetOutputValueOpts{})
				if err != nil {

					mutex.Lock()
					defer mutex.Unlock()

					if firstError == nil {
						firstError = err
					} else {
						firstError = errors.Join(firstError, err)
					}

					return
				}

				err = n.Execute(ni.Core_concurrent_for_each_loop_v1_Output_exec_body, nti, nil)
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
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}

	for iterable.Next() && !c.IsCancelled() {
		iter(iterable.Key(), iterable.Value())
	}

	close(taskCh)

	wg.Wait()

	if firstError != nil {
		return firstError
	}

	err = n.Execute(ni.Core_concurrent_for_each_loop_v1_Output_exec_completed, c, nil)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(concurrentForEachLoopDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &ConcurrentForEachLoopNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
