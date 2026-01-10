package nodes

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
	"github.com/actionforge/actrun-cli/utils"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

//go:embed run@v1.yml
var runDefinition string

type RunNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions
}

var bashPathOnce sync.Once
var winBashExes = []struct {
	Path  string
	Mount string
}{
	// Keep this list in sync with 'check_requirements' in 'tests_e2e.go'
	{Path: "C:\\Program Files\\Git\\bin\\bash.exe", Mount: ""},
	{Path: "C:\\msys64\\usr\\bin\\bash.exe", Mount: ""},
	{Path: "C:\\cygwin64\\bin\\bash.exe", Mount: "/cygdrive"},
}
var bashArgs = []string{"--noprofile", "--norc", "-eo", "pipefail", "-l"}
var winBashPath string // Path to bash.exe that is valid for all run calls
var winBashMount string

var cmdArgs = []string{"/D", "/E:ON", "/V:OFF", "/S", "/C"}

var pythonPathOnce sync.Once
var pythonPath string

var pwshPathOnce sync.Once
var pwshPath string

func (n *RunNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	shell, err := core.InputValueById[string](c, n, ni.Core_run_v1_Input_shell)
	if err != nil {
		return err
	}

	script, err := core.InputValueById[string](c, n, ni.Core_run_v1_Input_script)
	if err != nil {
		return err
	}

	print, err := core.InputValueById[string](c, n, ni.Core_run_v1_Input_print)
	if err != nil {
		return err
	}

	args, err := core.InputValueById[[]string](c, n, ni.Core_run_exec_v1_Input_args)
	if err != nil {
		return err
	}

	envs, err := core.InputValueById[[]string](c, n, ni.Core_run_v1_Input_env)
	if err != nil {
		return err
	}

	stdin, err := core.InputValueById[io.Reader](c, n, ni.Core_run_v1_Input_stdin)
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
		for envName, path := range ghEnvs {
			currentEnvMap[envName] = path
		}
	}

	output, exitCode, runErr := runCommand(c, shell, &script, args, print, stdin, currentEnvMap)

	// I don't see a reason here why capturing a
	// failed reader close would be important here
	_ = utils.SafeCloseReader(stdin)

	err = n.SetOutputValue(c, ni.Core_run_v1_Output_output, output, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.SetOutputValue(c, ni.Core_run_v1_Output_exit_code, exitCode, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	if runErr != nil {
		err = n.Execute(ni.Core_run_v1_Output_exec_err, c, runErr)
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

		err = n.Execute(ni.Core_run_v1_Output_exec_success, c, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func runCommand(c *core.ExecutionState, shell string, script *string, args []string, print string, stdin io.Reader, curEnvMap map[string]string) (string, int, error) {
	for i, arg := range args {
		args[i] = strings.TrimSpace(arg)
	}

	var (
		ok  bool
		cmd *exec.Cmd
	)

	if script == nil {
		cmd = exec.Command(shell, args...)
	} else {
		scriptName := "run-script-*"
		if runtime.GOOS == "windows" {
			switch shell {
			case "bash":
				scriptName += ".sh"
			case "cmd":
				scriptName += ".cmd"
			case "pwsh":
				scriptName += ".ps1"
			}
		}

		if shell == "cmd" {
			// ensure that cmd.exe and all subcommands write everything to stdout/stderr in utf-8
			*script = "@chcp 65001>nul\n" + *script
		}

		// Normalize line endings from \r\n to \n since most shells don't like \r\n like bash
		scriptPath, err := utils.CreateAndWriteTempFile(*script, scriptName, utils.Normalize_LineEndings)
		if err != nil {
			return "", 0, err
		}
		defer os.Remove(scriptPath)

		switch shell {
		case "bash":
			var mnt string
			shell, mnt, ok = determineBashPath()
			if !ok {
				// replace with human readable error
				return "", 0, core.CreateErr(c, nil, "bash not found").SetHint(`oops! We couldn't run the script because Bash is missing. Please check the link below try again.
	https://docs.actionforge.dev/nodes/core/run/v1/#bash-for-%s`, getProperOsName())
			}

			// ensure that bash and commands write everything to stdout/stderr in utf-8
			curEnvMap["LANG"] = "en_US.UTF-8"
			curEnvMap["LC_ALL"] = "en_US.UTF-8"

			if runtime.GOOS == "windows" {
				scriptPath = mnt + utils.ConvertToPosixPath(scriptPath)
			}

			tmpArgs := append(bashArgs, scriptPath)
			args = append(tmpArgs, args...)
		case "pwsh":
			shell, ok = determinePwshPath()
			if !ok {
				// replace with human readable error
				return "", 0, core.CreateErr(c, nil, "pwsh not found").SetHint(`oops! We couldn't run the script because PowerShell Core is missing. Please check the link below try again.
	https://docs.actionforge.dev/nodes/core/run-v1/#pwsh-for-%s`, getProperOsName())
			}

			args = append([]string{scriptPath}, args...)
		case "cmd":
			if runtime.GOOS != "windows" {
				return "", 0, core.CreateErr(c, nil, "cmd not found").SetHint("oops! We couldn't run the script because the 'cmd' shell is only supported on Windows. Run this script again on Windows, or use a different shell.")
			}

			cmdPath, ok := os.LookupEnv("ComSpec")
			if ok {
				shell = cmdPath
			}

			tmpArgs := append(cmdArgs, scriptPath)
			args = append(tmpArgs, args...)
		case "python":
			shell, ok = determinePythonPath()
			if !ok {
				return "", 0, err
			}

			// ensure that python writes everything to stdout/stderr in utf-8
			curEnvMap["PYTHONIOENCODING"] = "utf-8"

			args = append([]string{scriptPath}, args...)
		}
		cmd = exec.Command(shell, args...)

	}

	if stdin != nil {
		cmd.Stdin = stdin
	}
	cmd.Env = func() []string {
		env := make([]string, 0, len(curEnvMap))
		for k, v := range curEnvMap {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		return env
	}()

	var combinedOutput bytes.Buffer
	runErr := runAndCaptureOutput(c, cmd, print, &combinedOutput)

	combinedOutputStr, err := utils.DecodeBytes(combinedOutput.Bytes())
	if err != nil {
		return "", 0, err
	}

	return combinedOutputStr, cmd.ProcessState.ExitCode(), runErr
}

func determinePythonPath() (string, bool) {
	var err error
	pythonPathOnce.Do(func() {
		for _, pythonName := range []string{"python3", "python"} {
			var path string
			path, err = exec.LookPath(pythonName)
			if err != nil {
				continue
			}

			if exec.Command(path, "--version").Run() == nil {
				pythonPath = path
				break
			}
		}
	})
	if err != nil || pythonPath == "" {
		return "", false
	}
	return pythonPath, true
}

func determinePwshPath() (string, bool) {
	var err error
	pwshPathOnce.Do(func() {
		var path string
		path, err = exec.LookPath("pwsh")
		if err != nil {
			return
		}

		if exec.Command(path, "--version").Run() == nil {
			pwshPath = path
		}
	})
	if err != nil || pwshPath == "" {
		return "", false
	}
	return pwshPath, true
}

func determineBashPath() (string, string, bool) {

	if runtime.GOOS != "windows" {
		// On Posix OS always use the default bash path
		return "bash", "", true
	} else {
		var err error
		bashPathOnce.Do(func() {

			for _, bash := range winBashExes {

				var scriptPath string

				// check if echo and ls is available, which hints that builtins and programs work
				script := fmt.Sprintf("echo OK\nls -l %s/c", bash.Mount)
				scriptPath, err = utils.CreateAndWriteTempFile(script, "act-bash-test-*.sh", utils.Normalize_LineEndings)
				if err != nil {
					return
				}
				defer os.Remove(scriptPath)

				tmpScriptPath := fmt.Sprintf("%s%s", bash.Mount, utils.ConvertToPosixPath(scriptPath))

				cmd := exec.Command(bash.Path)
				cmd.Args = bashArgs
				cmd.Args = append(cmd.Args, tmpScriptPath)

				var outBuffer bytes.Buffer
				cmd.Stdout = &outBuffer
				cmd.Stderr = &outBuffer

				err = cmd.Run()
				if err != nil {
					continue
				}

				output := outBuffer.String()
				if strings.HasPrefix(output, "OK\n") && strings.Contains(output, "Program Files") {
					winBashPath = bash.Path
					winBashMount = bash.Mount
					break
				}
			}
		})
		if err != nil || winBashPath == "" {
			return "", "", false
		}
		return winBashPath, winBashMount, true
	}
}

func runAndCaptureOutput(c *core.ExecutionState, cmd *exec.Cmd, print string, combinedOutput *bytes.Buffer) error {
	var runErr error

	utf8Decoder := unicode.UTF8.NewDecoder()
	stdoutTransformer := transform.NewWriter(io.MultiWriter(utils.LogOut.Out, combinedOutput), utf8Decoder)
	stderrTransformer := transform.NewWriter(io.MultiWriter(utils.LogErr.Out, combinedOutput), utf8Decoder)
	switch print {
	case "stdout":
		cmd.Stdout = stdoutTransformer
		cmd.Stderr = utils.LogErr.Out
	case "output":
		transformer := transform.NewWriter(combinedOutput, utf8Decoder)
		cmd.Stdout = transformer
		cmd.Stderr = transformer
	default: // if 'both'
		cmd.Stdout = stdoutTransformer
		cmd.Stderr = stderrTransformer
	}

	runErr = cmd.Run()

	if runErr != nil {
		return core.CreateErr(c, runErr, "failed to run command")
	}

	return runErr
}

func init() {
	err := core.RegisterNodeFactory(runDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &RunNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}

func getProperOsName() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "macos"
	case "linux":
		return "linux"
	default:
		return runtime.GOOS
	}
}
