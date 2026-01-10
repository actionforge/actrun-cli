package nodes

import (
	"bytes"
	_ "embed"
	"encoding/gob"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/actionforge/actrun-cli/core"

	ni "github.com/actionforge/actrun-cli/node_interfaces"

	"github.com/BurntSushi/toml"
	"gopkg.in/ini.v1"

	"go.yaml.in/yaml/v4"
)

//go:embed parser@v1.yml
var parseDefinition string

type ParserNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions
}

func (n *ParserNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	inputString, err := core.InputValueById[string](c, n, ni.Core_parser_v1_Input_input)
	if err != nil {
		return err
	}

	format, err := core.InputValueById[string](c, n, ni.Core_parser_v1_Input_format)
	if err != nil {
		return err
	}

	var parseError error
	var parsedData any

	if format == "auto" {
		if strings.HasPrefix(strings.TrimSpace(inputString), "{") {
			format = "json"
		} else if strings.HasPrefix(strings.TrimSpace(inputString), "<") {
			format = "xml"
		} else if strings.Contains(inputString, "=") {
			format = "ini"
		} else if strings.Contains(inputString, "[") {
			format = "toml"
		} else {
			format = "yaml"
		}
	}

	switch format {
	case "json":
		parseError = json.Unmarshal([]byte(inputString), &parsedData)
		if parseError != nil {
			parseError = core.CreateErr(c, parseError, "failed to parse JSON")
		}
	case "yaml":
		parseError = yaml.Unmarshal([]byte(inputString), &parsedData)
		if parseError != nil {
			parseError = core.CreateErr(c, parseError, "failed to parse YAML")
		}
	case "toml":
		parseError = toml.Unmarshal([]byte(inputString), &parsedData)
		if parseError != nil {
			parseError = core.CreateErr(c, parseError, "failed to parse TOML")
		}
	case "ini":
		var cfg *ini.File
		cfg, parseError = ini.Load([]byte(inputString))
		if parseError != nil {
			parseError = core.CreateErr(c, parseError, "failed to parse INI")
		} else {
			for _, section := range cfg.Sections() {
				var arr = make(map[string]any)
				sectionMap := make(map[string]string)
				for _, key := range section.Keys() {
					sectionMap[key.Name()] = key.String()
				}
				arr[section.Name()] = sectionMap
				parsedData = arr
			}
		}
	default:
		return core.CreateErr(c, nil, "unsupported format: %s", format)
	}

	parsedData, err = deepCopy(parsedData)
	if err != nil {
		return core.CreateErr(c, err, "failed to deep copy parsed data")
	}

	err = n.SetOutputValue(c, ni.Core_parser_v1_Output_object, parsedData, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	if parseError != nil {
		err = n.Execute(ni.Core_parser_v1_Output_exec_err, c, parseError)
		if err != nil {
			return err
		}
	} else {
		err = n.Execute(ni.Core_parser_v1_Output_exec_success, c, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func deepCopy(src any) (any, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	dec := gob.NewDecoder(&buf)

	err := enc.Encode(src)
	if err != nil {
		return nil, err
	}

	// the decoder can't decode concrete types
	// like integers to `any`, so we use reflect
	// here to really create an object of the
	// type we expect from the decoder.
	clonePtr := reflect.New(reflect.TypeOf(src))
	clone := clonePtr.Interface()

	err = dec.Decode(clone)
	if err != nil {
		return nil, err
	}

	return reflect.Indirect(reflect.ValueOf(clone)).Interface(), nil
}

func init() {
	gob.Register([]any{})
	gob.Register(map[string]any{})

	err := core.RegisterNodeFactory(parseDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &ParserNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
