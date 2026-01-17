package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed storage-download@v1.yml
var storageDownloadDefinition string

type StorageDownloadNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *StorageDownloadNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {

	path, err := core.InputValueById[string](c, n, ni.Core_storage_download_v1_Input_name)
	if err != nil {
		return err
	}

	provider, err := core.InputValueById[StorageDownloadProvider](c, n, ni.Core_storage_download_v1_Input_provider)
	if err != nil {
		return err
	}

	reader, downloadErr := provider.DownloadObject(path)
	if downloadErr != nil {
		return core.CreateErr(c, downloadErr, "failed to download object")
	}

	dsf := core.DataStreamFactory{
		SourcePath:     path,
		SourceProvider: provider,
		Reader:         reader,
		Length:         core.GetReaderLength(reader),
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_storage_download_v1_Output_provider, provider, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_storage_download_v1_Output_data, dsf, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.Execute(ni.Core_storage_download_v1_Output_exec, c, nil)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(storageDownloadDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &StorageDownloadNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
