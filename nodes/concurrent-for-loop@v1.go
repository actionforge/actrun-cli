package nodes

import (
	_ "embed"
	"sync"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
	"github.com/actionforge/actrun-cli/utils"

	"errors"
)

//go:embed concurrent-for-loop@v1.yml
var concurrentForLoopDefinition string

type ConcurrentLoopNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions
}

func (n *ConcurrentLoopNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	firstIndex, err := core.InputValueById[int](c, n, ni.Core_concurrent_for_loop_v1_Input_first_index)
	if err != nil {
		return err
	}

	lastIndex, err := core.InputValueById[int](c, n, ni.Core_concurrent_for_loop_v1_Input_last_index)
	if err != nil {
		return err
	}

	workerCount, err := core.InputValueById[int](c, n, ni.Core_concurrent_for_loop_v1_Input_worker_count)
	if err != nil {
		return err
	}

	// 0 means run everything concurrently
	if workerCount == 0 {
		workerCount = lastIndex - firstIndex + 1
	}

	if firstIndex > lastIndex {
		// zero executions
		return nil
	}

	if !utils.ConcurrencyIsEnabled() {
		workerCount = 1
	}

	taskCh := make(chan int, workerCount)
	wg := sync.WaitGroup{}
	var mutex sync.Mutex
	var firstError error

	count := lastIndex - firstIndex + 1
	nti := []*core.ExecutionState{}
	for i := 0; i < count; i++ {
		nti = append(nti, c.PushNewExecutionState(n))
	}

	_, ok := n.GetExecutionTarget(ni.Core_concurrent_for_loop_v1_Output_exec_body)
	if ok {
		for w := 0; w < workerCount; w++ {
			fn := func() {
				for i := range taskCh {

					if c.IsCancelled() {
						return
					}

					ctx := nti[i-firstIndex]

					err := n.Outputs.SetOutputValue(ctx, ni.Core_concurrent_for_loop_v1_Output_index, i, core.SetOutputValueOpts{})
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

					err = n.Execute(ni.Core_concurrent_for_loop_v1_Output_exec_body, ctx, nil)
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

		for i := firstIndex; i <= lastIndex && !c.IsCancelled(); i++ {
			taskCh <- i
		}
		close(taskCh)
	}

	wg.Wait()

	if firstError != nil {
		return firstError
	}

	err = n.Execute(ni.Core_concurrent_for_loop_v1_Output_exec_completed, c, nil)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(concurrentForLoopDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &ConcurrentLoopNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
