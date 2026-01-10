package nodes

import (
	_ "embed"
	"fmt"
	"reflect"
	"strings"
	"unicode"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed string-fmt@v1.yml
var stringFmtDefinition string

type StringFmt struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *StringFmt) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	fmtString, err := core.InputValueById[string](c, n, ni.Core_string_fmt_v1_Input_fmt)
	if err != nil {
		return nil, err
	}

	inputs, err := core.InputArrayValueById[string](c, n, ni.Core_string_fmt_v1_Input_substitutes, core.GetInputValueOpts{})
	if err != nil {
		return nil, err
	}

	if strings.Contains(fmtString, "%") && len(inputs) > 0 {
		subs := make([]any, len(inputs))
		for i, v := range inputs {
			subs[i] = v
		}

		err := verifyFmtString(n.Id, fmtString, len(inputs))
		if err != nil {
			return nil, err
		}

		return fmt.Sprintf(fmtString, subs...), nil
	} else {
		return fmtString, nil
	}
}

func init() {
	err := core.RegisterNodeFactory(stringFmtDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		// this definition ensures that the number of format verbs in the format string
		// matches the number of substitution inputs. This can be done if the format string
		// is already predefined and not dynamic (in that case, there is another check above in OutputValueById).
		inputsDef := nodeDef["inputs"]
		if inputsDef != nil {
			inputs := reflect.ValueOf(inputsDef)
			if inputs.Kind() == reflect.Map {

				var formatString string
				substitutes := 0

				for _, key := range inputs.MapKeys() {
					value := inputs.MapIndex(key)
					if value.IsValid() && value.CanInterface() {

						value = reflect.ValueOf(value.Interface())

						var valueString string
						if value.Kind() == reflect.String {
							valueString = value.String()
						}

						if key.String() == "fmt" {
							formatString = valueString
						} else if strings.HasPrefix(key.String(), "substitutes") {
							substitutes++
						}
					}
				}

				nodeId, ok := nodeDef["id"].(string)
				if !ok {
					nodeId = "unknown"
				}

				err := verifyFmtString(nodeId, formatString, substitutes)
				if err != nil {
					return nil, []error{err}
				}
			}
		}
		return &StringFmt{}, nil
	})
	if err != nil {
		panic(err)
	}
}

func verifyFmtString(nodeId string, fmtString string, substitutes int) error {
	verbsCount := countFormatVerbs(fmtString)
	if verbsCount != substitutes {
		return core.CreateErr(nil, nil, "node 'String Format' (%s) with format string '%s' has %d verb(s) but %d inputs were provided", nodeId, fmtString, verbsCount, substitutes).SetHint("the number of format verbs (%%s, %%v, etc.) must match the number of substitution inputs in the 'String Format' node")
	}
	return nil
}

func countFormatVerbs(format string) int {
	count := 0
	for i := 0; i < len(format); i++ {
		if format[i] == '%' {
			// Check if the next character is also '%' (escaped verb)
			if i+1 < len(format) && format[i+1] == '%' {
				i++ // Skip the escaped '%'
				continue
			}
			// Move to the next character, which should be the format verb
			i++
			// Skip any flags, width, or precision specifiers
			for i < len(format) && (unicode.IsDigit(rune(format[i])) || format[i] == '.' || format[i] == '#') {
				i++
			}
			// Check if we have a valid verb character
			if i < len(format) && unicode.IsLetter(rune(format[i])) {
				count++
			}
		}
	}
	return count
}
