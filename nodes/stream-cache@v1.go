package nodes

import (
	"bytes"
	_ "embed"
	"io"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed stream-cache@v1.yml
var streamCacheNodeDefinition string

type StreamCacheNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *StreamCacheNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	inputStream, err := core.InputValueById[io.Reader](c, n, ni.Core_stream_cache_v1_Input_stream)
	if err != nil {
		return err
	}

	buffer := new(bytes.Buffer)
	_, err = io.Copy(buffer, inputStream)
	if err != nil {
		return core.CreateErr(c, err, "failed to read input stream")
	}

	err = n.SetOutputValue(c, ni.Core_stream_cache_v1_Output_result, buffer.String(), core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.Execute(ni.Core_stream_cache_v1_Output_exec_success, c, nil)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(streamCacheNodeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &StreamCacheNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
