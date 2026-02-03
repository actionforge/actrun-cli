package core

import "github.com/actionforge/actrun-cli/utils"

type ExecutionSource struct {
	SrcNode HasExecutionInterface
	Output  OutputId
}

type NodeBaseAndExecutionInterface interface {
	NodeBaseInterface
	HasExecutionInterface
}

type ExecutionTarget struct {
	SrcNode NodeBaseInterface
	DstNode NodeBaseAndExecutionInterface
	Port    InputId
}

type Executions struct {
	Executions map[OutputId]ExecutionTarget
}

func (n *Executions) Execute(outputPort OutputId, ec *ExecutionState, err error) error {

	// Inbetween the execution of nodes we need to reset the ephemeral data output cache.
	// Every execution node receives a fresh batch of data from its incoming connections.
	ec.EmptyDataOutputCache()

	dest, hasDest := n.GetExecutionTarget(outputPort)

	// Set the step conclusion for the SOURCE node based on which output port is being executed.
	// This enables ${{ steps.X.conclusion }} syntax similar to GitHub Actions.
	// The conclusion is set BEFORE downstream nodes execute so they can read it.
	if hasDest && dest.SrcNode != nil {
		srcStepEntry := GetOrCreateStepEntry(ec.StepCache, dest.SrcNode.GetId())
		if err != nil {
			srcStepEntry.Conclusion = "failure"
		} else {
			srcStepEntry.Conclusion = "success"
		}
	}

	// if this is the error path, and the error port is not connected
	// we return the error so it won't be silently ignored
	if err != nil {
		// If the error output is not connected or its a group output, we can safely fail here
		if !hasDest {
			return CreateErr(ec, err, "error during execution")
		}
	}

	// nothing to execute
	if !hasDest || dest.DstNode == nil {
		return nil
	}

	if ec.IsGitHubWorkflow || utils.GetLogLevel() == utils.LogLevelDebug {
		LogDebugInfoForGh(dest.DstNode)
	}

	ec.PushNodeVisit(dest.DstNode, true)
	if ec.IsCancelled() {
		return nil
	}

	err = dest.DstNode.ExecuteImpl(ec, dest.Port, err)
	if err != nil {
		return err
	}

	return nil
}

func (e *Executions) GetExecutionTarget(outputId OutputId) (ExecutionTarget, bool) {
	t, ok := e.Executions[OutputId(outputId)]
	return t, ok
}

func (e *Executions) ConnectExecutionPort(srcNode NodeBaseInterface, srcPortId OutputId, dstNode NodeBaseInterface, dstPortId InputId) error {
	if e.Executions == nil {
		e.Executions = make(map[OutputId]ExecutionTarget)
	}

	execNode, ok := dstNode.(NodeBaseAndExecutionInterface)
	if !ok {
		return CreateErr(nil, nil, "node '%s' has no execution interface", dstNode.GetNodeTypeId())
	}

	srcNode.SetExecutionNode(true)
	dstNode.SetExecutionNode(true)

	e.Executions[srcPortId] = ExecutionTarget{
		SrcNode: srcNode,
		DstNode: execNode,
		Port:    dstPortId,
	}

	return nil
}
