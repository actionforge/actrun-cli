package nodes

import (
	_ "embed"
	"errors"
	"time"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed sleep@v1.yml
var sleepNodeDefinition string

type SleepNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *SleepNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	duration, err := core.InputValueById[int](c, n, ni.Core_sleep_v1_Input_duration)
	if err != nil {
		return err
	}
	unit, err := core.InputValueById[string](c, n, ni.Core_sleep_v1_Input_unit)
	if err != nil {
		return err
	}

	var sleepDuration time.Duration
	switch unit {
	case "seconds":
		sleepDuration = time.Duration(duration) * time.Second
	case "milliseconds":
		sleepDuration = time.Duration(duration) * time.Millisecond
	case "microseconds":
		sleepDuration = time.Duration(duration) * time.Microsecond
	case "nanoseconds":
		sleepDuration = time.Duration(duration) * time.Nanosecond
	default:
		return errors.New("invalid unit")
	}

	select {
	case <-time.After(sleepDuration):
	case <-c.Ctx.Done():
		return c.Ctx.Err()
	}

	err = n.Execute(ni.Core_sleep_v1_Output_exec, c, nil)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(sleepNodeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &SleepNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
