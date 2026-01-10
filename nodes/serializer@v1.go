package nodes

import (
	"bytes"
	_ "embed"
	"encoding/json"

	"github.com/actionforge/actrun-cli/core"

	ni "github.com/actionforge/actrun-cli/node_interfaces"

	"github.com/BurntSushi/toml"
	"go.yaml.in/yaml/v4"
	"gopkg.in/ini.v1"
)

//go:embed serializer@v1.yml
var serializeDefinition string

type SerializerNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions
}

func (n *SerializerNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	object, err := core.InputValueById[any](c, n, ni.Core_serializer_v1_Input_object)
	if err != nil {
		return err
	}

	format, err := core.InputValueById[string](c, n, ni.Core_serializer_v1_Input_format)
	if err != nil {
		return err
	}

	var (
		serializeError error
		output         string
	)

	switch format {
	case "json":
		outputBytes, err := json.MarshalIndent(object, "", "  ")
		if err != nil {
			serializeError = core.CreateErr(c, err, "failed to serialize JSON")
		} else {
			output = string(outputBytes)
		}
	case "yaml":
		outputBytes, err := yaml.Marshal(object)
		if err != nil {
			serializeError = core.CreateErr(c, err, "failed to serialize YAML")
		} else {
			output = string(outputBytes)
		}
	case "toml":
		outputBytes := bytes.Buffer{}
		encoder := toml.NewEncoder(&outputBytes)
		err = encoder.Encode(object)
		if err != nil {
			serializeError = core.CreateErr(c, err, "failed to serialize TOML")
		} else {
			output = outputBytes.String()
		}
	case "ini":
		_, ok := object.(map[string]any)
		if !ok {
			return core.CreateErr(c, nil, "data must be a map")
		}

		arr := object.(map[string]any)

		cfg := ini.Empty()
		for sectionName, sectionData := range arr {
			sectionMap, ok := sectionData.(map[string]any)
			if ok {
				section := cfg.Section(sectionName)
				for key, value := range sectionMap {
					section.Key(key).SetValue(value.(string))
				}
			}
		}
		outputBytes := bytes.Buffer{}
		_, err := cfg.WriteTo(&outputBytes)
		if err != nil {
			serializeError = core.CreateErr(c, err, "failed to serialize INI")
		} else {
			output = outputBytes.String()
		}
	default:
		return core.CreateErr(c, nil, "unsupported format: %s", format)
	}

	err = n.SetOutputValue(c, ni.Core_serializer_v1_Output_output, output, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	if serializeError != nil {
		err = n.Execute(ni.Core_serializer_v1_Output_exec_err, c, serializeError)
		if err != nil {
			return err
		}
	} else {
		err = n.Execute(ni.Core_serializer_v1_Output_exec_success, c, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(serializeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &SerializerNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
