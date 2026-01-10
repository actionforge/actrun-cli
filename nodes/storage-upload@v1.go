package nodes

import (
	_ "embed"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed storage-upload@v1.yml
var storageUploadDefinition string

type StorageUploadNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *StorageUploadNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	dsf, err := core.InputValueById[core.DataStreamFactory](c, n, ni.Core_storage_upload_v1_Input_data)
	if err != nil {
		return err
	}
	defer dsf.CloseStreamAndIgnoreError()

	objectName, err := core.InputValueById[string](c, n, ni.Core_storage_upload_v1_Input_name)
	if err != nil {
		return err
	}

	dataSource, ok := n.Inputs.GetDataSource(ni.Core_storage_upload_v1_Input_data)
	if !ok {
		// should never happen since data was already found and valid above
		return core.CreateErr(c, nil, "no data source")
	}

	var (
		cloneProvider StorageCloneProvider
		uploadErr     error
	)

	provider, err := core.InputValueById[core.StorageProvider](c, n, ni.Core_storage_upload_v1_Input_provider)
	if err != nil {
		return err
	}

	// if the data source is a storage download, we can try to clone the object
	// instead of downloading and uploading it. For example, this is useful
	// for S3 buckets where we can clone objects between buckets wihout the middleman.
	if strings.HasPrefix(dataSource.SrcNode.GetNodeTypeId(), "core/storage-download@") {
		p, ok := provider.(StorageCloneProvider)
		if ok {
			cloneProvider = p
		}
	}

	// The data source might have a storage provider attached to it.
	// This is the case for e.g. a storage download node, where as
	// a file-read node doesn't have a storage provider attached to it.
	sourceProvider, ok := dsf.SourceProvider.(core.StorageProvider)
	if ok && cloneProvider.CanClone(sourceProvider) {
		uploadErr = cloneProvider.CloneObject(objectName, sourceProvider, dsf.SourcePath)
		if uploadErr != nil {
			uploadErr = core.CreateErr(c, uploadErr, "failed to clone object")
		}
	} else {
		// if cannot clone, fall back to io.Reader implementation
		provider, ok := provider.(StorageUploadProvider)
		if !ok {
			return core.CreateErr(c, nil, "provider does not support upload")
		}

		uploadErr = provider.UploadObject(objectName, dsf.Reader)
		if uploadErr != nil {
			uploadErr = core.CreateErr(c, uploadErr, "failed to upload object")
		}
	}

	// Ensure the input stream is closed in all cases.
	// If closing the stream fails without a prior error,
	// treat it as an error which is part of the upload op.
	err = dsf.CloseStream()
	if err != nil && uploadErr == nil {
		uploadErr = err
	}

	if uploadErr != nil {
		err = n.Execute(ni.Core_storage_upload_v1_Output_exec_err, c, uploadErr)
		if err != nil {
			return err
		}
	} else {
		err = n.Outputs.SetOutputValue(c, ni.Core_storage_upload_v1_Output_provider, provider, core.SetOutputValueOpts{})
		if err != nil {
			return err
		}

		err = n.Execute(ni.Core_storage_upload_v1_Output_exec_success, c, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(storageUploadDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &StorageUploadNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
