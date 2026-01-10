package nodes

import (
	_ "embed"
	"strings"

	"github.com/actionforge/actrun-cli/core"
)

//go:embed group@v1.yml
var subgraphDefinition string

var DefaultExec core.OutputId = "exec"

type GroupNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions
}

func (n *GroupNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	v, err := n.InputValueById(c, n, core.InputId(outputId), nil)
	if err != nil {
		return nil, err
	}

	return v, nil
}

func (n *GroupNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	// For group nodes, `ExecuteImpl` is called twice. First when entered, then the second time from the node that leaves the group (mostly group-outputs@v1)

	// TODO(Seb): Evaluate if group nodes should create their own execution state.
	// Currently, I use the shared parent state to preserve visited nodes and output values.
	// To support this in the future, the inner graph execution would need to be merged into the outer one
	// when the group node is left. Review if this comes with other advantages
	// Draft implementation for push/pull execution state is commented out below.
	/*
		_, ok := n.Inputs.GetInputDefs()[inputId]
		if ok {
			// entering the group node
			c = c.PushNewExecutionState(n)
		} else {
			_, _, ok := n.Outputs.OutputDefByPortId(string(inputId))
			if !ok {
				return core.CreateErr(nil, nil, "group node '%s' has no input or output with id '%s'", n.GetId(), inputId).SetHint(core.HINT_INTERNAL_ERROR)
			}

			// leaving the group node, pop execution state
			tmp := c.ParentExecution
			if tmp == nil {
				return core.CreateErr(nil, nil, "group node '%s' has no parent execution state", n.GetId()).SetHint(core.HINT_INTERNAL_ERROR)
			}
			c = tmp
		}
	*/

	_, ok := n.Inputs.GetInputDefs()[inputId]
	if ok {
		// entering the group node
		c.Hierarchy = append(c.Hierarchy, n)
	} else {
		_, _, ok := n.Outputs.OutputDefByPortId(string(inputId))
		if !ok {
			return core.CreateErr(nil, nil, "group node '%s' has no input or output with id '%s'", n.GetId(), inputId).SetHint(core.HINT_INTERNAL_ERROR)
		}

		if len(c.Hierarchy) == 0 {
			return core.CreateErr(nil, nil, "group node '%s' has no parent execution state", n.GetId()).SetHint(core.HINT_INTERNAL_ERROR)
		}
		c.Hierarchy = c.Hierarchy[:len(c.Hierarchy)-1]
	}

	err := n.Execute(core.OutputId(inputId), c, prevError)
	if err != nil {
		return err
	}
	return nil
}

func init() {
	// Factory function now accepts 'validate' and returns []error
	err := core.RegisterNodeFactory(subgraphDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		var collectedErrors []error

		subGraph, ok := nodeDef["graph"].(map[string]any)
		if !ok {
			err := core.CreateErr(nil, nil, "group node '%s' (%s) has an invalid graph definition", nodeDef["id"], nodeDef["type"])
			// Critical error, cannot proceed without graph definition
			return nil, []error{err}
		}

		group := &GroupNode{}

		graphOutputs, err := core.LoadGraphOutputs(subGraph)
		if err != nil {
			if !validate {
				return nil, []error{err}
			}
			collectedErrors = append(collectedErrors, err)
		}

		if graphOutputs != nil {
			group.SetOutputDefs(graphOutputs, core.SetDefsOpts{
				AssignmentMode: core.AssignmentMode_Replace,
			})
		}

		graphInputs, err := core.LoadGraphInputs(subGraph)
		if err != nil {
			if !validate {
				return nil, []error{err}
			}
			collectedErrors = append(collectedErrors, err)
		}

		if graphInputs != nil {
			group.SetInputDefs(graphInputs, core.SetDefsOpts{
				AssignmentMode: core.AssignmentMode_Replace,
			})
		}

		// Check for name collisions
		if graphInputs != nil && graphOutputs != nil {
			for inputId := range graphInputs {
				if _, ok := graphOutputs[core.OutputId(inputId)]; ok {
					err := core.CreateErr(nil, nil, "group node has an input and output with the same name '%s'", inputId)
					if !validate {
						return nil, []error{err}
					}
					collectedErrors = append(collectedErrors, err)
				}
			}
			for outputId := range graphOutputs {
				if _, ok := graphInputs[core.InputId(outputId)]; ok {
					err := core.CreateErr(nil, nil, "group node has an input and output with the same name '%s'", outputId)
					if !validate {
						return nil, []error{err}
					}
					collectedErrors = append(collectedErrors, err)
				}
			}
		}

		// Pass 'validate' to LoadGraph to collect internal graph errors
		ag, errs := core.LoadGraph(subGraph, group, parentId, validate)
		if len(errs) > 0 {
			if !validate {
				return nil, errs
			}
			collectedErrors = append(collectedErrors, errs...)
		}

		group.NodeBaseComponent.Graph = &ag

		// Connect the group node with the group input node
		if graphInputs != nil {
			n, ok := ag.FindNode(ag.Entry)
			if !ok || n == nil {
				err := core.CreateErr(nil, nil, "group has no group input node")
				if !validate {
					return nil, []error{err}
				}
				collectedErrors = append(collectedErrors, err)
			} else {
				groupInputNode, ok := n.(*GroupInputsNode)
				if !ok || groupInputNode == nil {
					err := core.CreateErr(nil, nil, "group input node is not a group input node")
					if !validate {
						return nil, []error{err}
					}
					collectedErrors = append(collectedErrors, err)
				} else {
					for groupInputId, groupInput := range graphInputs {
						if groupInput.Exec {
							err = group.ConnectExecutionPort(group, core.OutputId(groupInputId),
								groupInputNode,
								core.InputId(groupInputId))
							if err != nil {
								if !validate {
									return nil, []error{err}
								}
								collectedErrors = append(collectedErrors, err)
								continue
							}
						} else {
							err := groupInputNode.Inputs.ConnectDataPort(group, string(groupInputId), groupInputNode, string(groupInputId), parent, core.ConnectOpts{
								SkipValidation: true,
							})
							if err != nil {
								if !validate {
									return nil, []error{err}
								}
								collectedErrors = append(collectedErrors, err)
								continue
							}
						}
					}
				}
			}
		}

		// Connect the group output node with the group node
		if graphOutputs != nil {
			var groupOutputNode *GroupOutputsNode
			for _, node := range ag.GetNodes() {
				if strings.HasPrefix(node.GetNodeTypeId(), "core/group-outputs@") {
					groupOutputNode, ok = node.(*GroupOutputsNode)
					if !ok {
						err := core.CreateErr(nil, nil, "group output node is not a group output node")
						if !validate {
							return nil, []error{err}
						}
						collectedErrors = append(collectedErrors, err)
					}
					break
				}
			}

			if groupOutputNode == nil {
				err := core.CreateErr(nil, nil, "group has no group output node")
				if !validate {
					return nil, []error{err}
				}
				collectedErrors = append(collectedErrors, err)
			} else {
				for groupOutputId, groupOutput := range graphOutputs {
					if groupOutput.Exec {
						err = groupOutputNode.Executions.ConnectExecutionPort(groupOutputNode,
							groupOutputId,
							group,
							core.InputId(groupOutputId))
						if err != nil {
							err = core.CreateErr(nil, err, "failed to connect execution ports")
							if !validate {
								return nil, []error{err}
							}
							collectedErrors = append(collectedErrors, err)
							continue
						}
					} else {
						err = group.ConnectDataPort(groupOutputNode, string(groupOutputId), group, string(groupOutputId), parent, core.ConnectOpts{
							SkipValidation: true,
						})
						if err != nil {
							err = core.CreateErr(nil, err, "failed to connect data ports")
							if !validate {
								return nil, []error{err}
							}
							collectedErrors = append(collectedErrors, err)
							continue
						}
					}
				}
			}
		}

		if len(collectedErrors) > 0 {
			return nil, collectedErrors
		}

		return group, nil
	},
	)
	if err != nil {
		panic(err)
	}
}
