package nodes

import (
	_ "embed"
	"sort"

	"github.com/actionforge/actrun-cli/core"
)

//go:embed sequence@v1.yml
var sequenceDefinition string

type SequenceNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions
}

func (n *SequenceNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {

	type execIndexPorts struct {
		ExecIndexPortId core.OutputId
		Index           int
	}

	sortedExecutions := []execIndexPorts{}

	for execPortId := range n.Executions.Executions {
		_, portIndex, isIndexPort := core.IsValidIndexPortId(string(execPortId))
		if !isIndexPort {
			return core.CreateErr(c, nil, "invalid input id %s", inputId)
		}

		sortedExecutions = append(sortedExecutions, execIndexPorts{
			ExecIndexPortId: execPortId,
			Index:           portIndex,
		})
	}

	sort.Slice(sortedExecutions, func(i, j int) bool {
		return sortedExecutions[i].Index < sortedExecutions[j].Index
	})

	for _, output := range sortedExecutions {
		t := n.Executions.Executions[output.ExecIndexPortId]
		if t.DstNode == nil {
			continue
		}

		err := n.Execute(output.ExecIndexPortId, c, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(sequenceDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &SequenceNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
