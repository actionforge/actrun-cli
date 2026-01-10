package nodes

import (
	_ "embed"
	"fmt"
	"io"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
	"github.com/actionforge/actrun-cli/utils"

	"github.com/fatih/color"
)

//go:embed print@v1.yml
var printDefinition string

type PrintNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *PrintNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	values, err := core.InputArrayValueById[any](c, n, ni.Core_print_v1_Input_values, core.GetInputValueOpts{})
	if err != nil {
		return err
	}

	// never fail because of color, it's not that important
	copt, _ := core.InputValueById[string](c, n, ni.Core_print_v1_Input_color)

	var currentColor color.Attribute

	switch copt {
	case "fg_black":
		currentColor = color.FgBlack
	case "fg_red":
		currentColor = color.FgRed
	case "fg_green":
		currentColor = color.FgGreen
	case "fg_yellow":
		currentColor = color.FgYellow
	case "fg_blue":
		currentColor = color.FgBlue
	case "fg_magenta":
		currentColor = color.FgMagenta
	case "fg_cyan":
		currentColor = color.FgCyan
	case "fg_white":
		currentColor = color.FgWhite
	}

	for _, value := range values {
		if value == nil {
			utils.LogOut.Infof("\n")
			continue
		}

		const escape = "\x1b"
		var (
			col   string
			unset string
		)

		if !color.NoColor && currentColor != color.Reset {
			col = fmt.Sprintf("%s[%dm", escape, currentColor)
			unset = fmt.Sprintf("%s[%dm", escape, color.Reset)
		}

		reader, err := core.ConvertValueByType[io.Reader](c, value)
		if err == nil {

			var (
				col   string
				unset string
			)
			if color.NoColor || currentColor == 0 {
				col = ""
				unset = ""
			} else {
				col = fmt.Sprintf("\x1b[%dm", currentColor)
				unset = fmt.Sprintf("\x1b[%dm", color.Reset)
			}

			reader = io.MultiReader(strings.NewReader(col), reader, strings.NewReader(unset+"\n"))
			_, err = io.Copy(utils.LogOut.Out, reader)

			utils.SafeCloseReaderAndIgnoreError(reader)

			if err != nil {
				return err
			}
		} else {
			// If the input source is not, or cannot be
			// converted to a reader, just print the value as-is
			fmt.Fprintf(utils.LogOut.Out, "%s%v%s\n", col, value, unset)
		}
	}

	err = n.Execute(ni.Core_print_v1_Output_exec, c, nil)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(printDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &PrintNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
