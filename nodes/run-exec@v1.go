package nodes

import (
	_ "embed"
	"io"
	"maps"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
	"github.com/actionforge/actrun-cli/utils"
)

//go:embed run-exec@v1.yml
var runExecDefinition string

type RunExecNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions
}

func (n *RunExecNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	path, err := core.InputValueById[string](c, n, ni.Core_run_exec_v1_Input_path)
	if err != nil {
		return err
	}

	print, err := core.InputValueById[string](c, n, ni.Core_run_exec_v1_Input_print)
	if err != nil {
		return err
	}

	args, err := core.InputValueById[[]string](c, n, ni.Core_run_exec_v1_Input_args)
	if err != nil {
		return err
	}

	envs, err := core.InputValueById[[]string](c, n, ni.Core_run_exec_v1_Input_env)
	if err != nil {
		return err
	}

	stdin, err := core.InputValueById[io.Reader](c, n, ni.Core_run_exec_v1_Input_stdin)
	if err != nil {
		return err
	}

	defer utils.SafeCloseReaderAndIgnoreError(stdin)

	currentEnvMap := c.GetContextEnvironMapCopy()
	for _, env := range envs {
		kv := strings.SplitN(env, "=", 2)
		if len(kv) != 2 {
			return core.CreateErr(c, nil, "invalid env format: '%s'", env)
		}
		currentEnvMap[kv[0]] = kv[1]
	}

	if c.IsGitHubWorkflow {
		ghContextParser := GhContextParser{}
		sysRunnerTempDir := currentEnvMap["RUNNER_TEMP"]
		if sysRunnerTempDir == "" {
			return core.CreateErr(c, nil, "RUNNER_TEMP is not set")
		}
		ghEnvs, err := ghContextParser.Init(c, sysRunnerTempDir)
		if err != nil {
			return core.CreateErr(c, err, "failed to initialize GitHub context parser")
		}
		maps.Copy(currentEnvMap, ghEnvs)
	}

	if path == "python" {
		betterPy, ok := determinePythonPath()
		if ok {
			path = betterPy
		}
	}

	output, exitCode, runErr := runCommand(c, path, nil, args, print, stdin, currentEnvMap)

	// I don't see a reason here why capturing a
	// failed reader close would be important here
	_ = utils.SafeCloseReader(stdin)

	err = n.SetOutputValue(c, ni.Core_run_exec_v1_Output_output, output, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.SetOutputValue(c, ni.Core_run_exec_v1_Output_exit_code, exitCode, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	if runErr != nil {
		err = n.Execute(ni.Core_run_exec_v1_Output_exec_err, c, runErr)
		if err != nil {
			return err
		}
	} else {
		if c.IsGitHubWorkflow {
			ghContextParser := GhContextParser{}
			ghEnvs, err := ghContextParser.Parse(c, currentEnvMap)
			if err != nil {
				return err
			}

			nextEnvMap := c.GetContextEnvironMapCopy()
			maps.Copy(nextEnvMap, ghEnvs)
			c.SetContextEnvironMap(nextEnvMap)
		}

		err = n.Execute(ni.Core_run_exec_v1_Output_exec_success, c, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(runExecDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &RunExecNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
