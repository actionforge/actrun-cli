package nodes

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
	"github.com/actionforge/actrun-cli/utils"
	u "github.com/actionforge/actrun-cli/utils"

	"github.com/Masterminds/semver/v3"
	"github.com/google/uuid"
	"go.yaml.in/yaml/v4"
)

var (
	dockerGithubWorkspace    = "/github/workspace"
	dockerGithubWorkflow     = "/github/workflow"
	dockerGithubFileCommands = "/github/file_commands"
	dockerGithubHome         = "/github/home"

	//go:embed gh-action@v1.yml
	ghActionNodeDefinition string
)

// 1. (github.com/)?        -> Registry (Optional)
// 2. ([-\w\.]+)/           -> Owner (Required, followed by /)
// 3. ([-\w\.]+)            -> Repo Name (Required)
// 4. (/[^@]+)?             -> Path (Optional). Matches slash followed by anything NOT an @
// 5. (@[-\w\.]+)?          -> Ref/Version (Optional). Matches @ followed by chars
var nodeTypeIdRegex = regexp.MustCompile(`^(github.com/)?([-\w\.]+)/([-\w\.]+)(/[^@]+)?(@[-\w\.]+)?$`)

type ActionType int

const (
	Docker ActionType = iota
	Node
)

type DockerData struct {
	Image               string
	DockerInstanceLabel string
	ExecutionStateId    string
}

type GhActionNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions

	actionName      string
	actionType      ActionType // docker or node
	actionRuns      ActionRuns
	actionRunJsPath string

	Data DockerData
}

func (n *GhActionNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	currentEnvMap := c.GetContextEnvironMapCopy()

	sysRunnerTempDir := currentEnvMap["RUNNER_TEMP"]
	if sysRunnerTempDir == "" {
		return core.CreateErr(c, nil, "RUNNER_TEMP is not set")
	}

	sysGhWorkspaceDir := currentEnvMap["GITHUB_WORKSPACE"]
	if sysGhWorkspaceDir == "" {
		return core.CreateErr(c, nil, "GITHUB_WORKSPACE is not set")
	}

	runnerToolCache := currentEnvMap["RUNNER_TOOL_CACHE"]
	if runnerToolCache == "" {
		return core.CreateErr(c, nil, "RUNNER_TOOL_CACHE is not set")
	}

	// turn inputs into env vars (INPUT_*)
	// matches ContainerActionHandler.cs AddInputsToEnvironment() behavior
	withInputs := ""
	for inputName, inputDef := range n.Inputs.GetInputDefs() {
		if inputDef.Exec {
			continue
		}

		if inputDef.Type == "string" {
			v, err := core.InputValueById[string](c, n, inputName)
			if err != nil {
				return err
			}

			// convert input name to uppercase for INPUT_ env var
			envKey := fmt.Sprintf("INPUT_%v", strings.ToUpper(string(inputName)))
			currentEnvMap[envKey] = v
			withInputs += fmt.Sprintf(" %s: %s\n", inputName, v)
		}
	}

	utils.LogOut.Infof("%sRun '%s (%s)'\n%s%s\n",
		u.LogGhStartGroup,
		n.GetId(),
		n.GetNodeTypeId(),
		withInputs,
		u.LogGhEndGroup,
	)

	// move env vars passed to node to env map
	inputEnv, err := core.InputValueById[[]string](c, n, "env")
	if err != nil {
		if !errors.Is(err, &core.ErrNoInputValue{}) {
			return err
		}
	}

	for _, env := range inputEnv {
		envName, envValue, found := strings.Cut(env, "=")
		if found {
			currentEnvMap[envName] = envValue
		}
	}

	// expose context vars (AllowList)
	// matches GitHubContext.cs GetRuntimeEnvironmentVariables()
	for contextName := range contextEnvAllowList {
		// in the original runner, context data is stored in a dictionary.
		// Here we assume currentEnvMap already contains the raw values (e.g. "sha", "actor").
		// We project them to GITHUB_SHA, GITHUB_ACTOR, etc.
		if val, ok := currentEnvMap[contextName]; ok {
			envName := fmt.Sprintf("GITHUB_%s", strings.ToUpper(contextName))
			currentEnvMap[envName] = val
		}
	}

	// init context parser for file commands (GITHUB_PATH, GITHUB_ENV)
	ghContextParser := GhContextParser{}
	ghEnvs, err := ghContextParser.Init(c, sysRunnerTempDir)
	if err != nil {
		return err
	}
	maps.Copy(currentEnvMap, ghEnvs)

	// set dir envs in case they are missing
	// https://github.com/actions/runner/blob/f467e9e1255530d3bf2e33f580d041925ab01951/src/Runner.Common/HostContext.cs#L288
	if currentEnvMap["AGENT_TOOLSDIRECTORY"] == "" {
		currentEnvMap["AGENT_TOOLSDIRECTORY"] = currentEnvMap["RUNNER_TOOL_CACHE"]
	}
	if currentEnvMap["RUNNER_TOOLSDIRECTORY"] == "" {
		currentEnvMap["RUNNER_TOOLSDIRECTORY"] = currentEnvMap["RUNNER_TOOL_CACHE"]
	}

	executionEnv := maps.Clone(currentEnvMap)

	var runErr error
	switch n.actionType {
	case Docker:
		runErr = n.ExecuteDocker(c, sysGhWorkspaceDir, executionEnv)
	case Node:
		runErr = n.ExecuteNode(c, sysGhWorkspaceDir, executionEnv)
	default:
		return core.CreateErr(c, nil, "unsupported action type: %v", n.actionType)
	}

	for outputId, outputDef := range n.OutputDefsClone() {
		if outputDef.Exec {
			continue
		}
		// all outputs in github actions are empty by default
		err = n.SetOutputValue(c, outputId, "", core.SetOutputValueOpts{})
		if err != nil {
			return err
		}
	}

	if runErr != nil {
		err = n.Execute(ni.Core_gh_action_v1_Output_exec_err, c, runErr)
		if err != nil {
			return err
		}
		return nil
	}

	// process fiel commands post-execution (GITHUB_ENV, GITHUB_OUTPUT, GITHUB_PATH)
	ghEnvs, err = ghContextParser.Parse(c, currentEnvMap)
	if err != nil {
		return err
	}

	// transfer env vars to next node
	nextEnvMap := c.GetContextEnvironMapCopy()
	maps.Copy(nextEnvMap, ghEnvs)
	c.SetContextEnvironMap(nextEnvMap)

	// parse GITHUB_OUTPUT
	githubOutput := currentEnvMap["GITHUB_OUTPUT"]
	if githubOutput != "" {
		b, err := os.ReadFile(githubOutput)
		if err != nil {
			return core.CreateErr(c, err, "unable to read github output file")
		}

		outputs, err := parseOutputFile(string(b))
		if err != nil {
			return err
		}
		for key, value := range outputs {
			err = n.SetOutputValue(c, core.OutputId(key), strings.TrimRight(value, "\t\n"), core.SetOutputValueOpts{
				NotExistsIsNoError: true,
			})
			if err != nil {
				return err
			}
		}

		_ = os.Remove(githubOutput)
	}

	err = n.Execute(ni.Core_gh_action_v1_Output_exec_success, c, nil)
	if err != nil {
		return err
	}

	return nil
}

func (n *GhActionNode) ExecuteNode(c *core.ExecutionState, workspace string, envs map[string]string) error {
	nodeBin := "node"
	runners, err := getRunnersDir()
	if err == nil {
		// Look for external node binary bundled with the runner
		externalNodeBin := filepath.Join(runners, "externals", n.actionRuns.Using, "bin", "node")
		_, err := os.Stat(nodeBin)
		if err == nil {
			nodeBin = externalNodeBin
		}
	}

	utils.LogOut.Infof("Use node binary: %s %s\n", nodeBin, n.actionRunJsPath)

	cmd := exec.Command(nodeBin, n.actionRunJsPath)
	cmd.Dir = workspace
	cmd.Stdout = utils.LogOut.Out
	cmd.Stderr = utils.LogErr.Out
	cmd.Stdin = nil
	cmd.Env = func() []string {
		env := make([]string, 0)
		for k, v := range envs {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		return env
	}()
	err = cmd.Run()
	if err != nil {
		return err
	}
	return nil
}

func (n *GhActionNode) ExecuteDocker(c *core.ExecutionState, workingDirectory string, env map[string]string) error {
	// Replicating logic from ContainerActionHandler.cs
	sysRunnerTempDir := env["RUNNER_TEMP"]
	if sysRunnerTempDir == "" {
		return core.CreateErr(c, nil, "RUNNER_TEMP is not set")
	}

	sysGithubWorkspace := env["GITHUB_WORKSPACE"]
	if sysGithubWorkspace == "" {
		return core.CreateErr(c, nil, "GITHUB_WORKSPACE is not set")
	}

	// path translation for file commands.
	// in Docker the file command paths must point to the mapped locations (/github/file_commands/...)
	// See ContainerActionHandler.cs: container.AddPathTranslateMapping
	fileCmds := []string{"GITHUB_ENV", "GITHUB_PATH", "GITHUB_OUTPUT", "GITHUB_STATE", "GITHUB_STEP_SUMMARY"}
	for _, envName := range fileCmds {
		path := env[envName]
		if path != "" {
			env[envName] = filepath.Join(dockerGithubFileCommands, filepath.Base(path))
		}
	}

	env["HOME"] = dockerGithubHome

	// in the original GH runner (ActionManifestManager.cs) arguments are evaluated using templates.
	// for now we perform simple context variable replacement.
	ContainerEntryArgs := make([]string, 0)
	for _, arg := range n.actionRuns.Args {
		res, err := core.EvaluateToStringExpression(c, arg)
		if err != nil {
			return err
		}
		ContainerEntryArgs = append(ContainerEntryArgs, res)
	}

	ci := core.ContainerInfo{
		ContainerImage:                n.Data.Image,
		ContainerDisplayName:          fmt.Sprintf("actionforge_%s_%s", n.Data.DockerInstanceLabel, uuid.New()),
		ContainerWorkDirectory:        dockerGithubWorkspace, // As set in ContainerActionHandler.cs
		ContainerEntryPointArgs:       strings.Join(ContainerEntryArgs, " "),
		ContainerEnvironmentVariables: env,
	}

	// mount docer sock
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		ci.MountVolumes = append(ci.MountVolumes, core.Volume{
			SourceVolumePath: "/var/run/docker.sock",
			TargetVolumePath: "/var/run/docker.sock",
			ReadOnly:         false,
		})
	}

	// mount workspace
	ci.MountVolumes = append(ci.MountVolumes, core.Volume{
		SourceVolumePath: sysGithubWorkspace,
		TargetVolumePath: dockerGithubWorkspace,
		ReadOnly:         false,
	})

	// mount workflow dir
	ci.MountVolumes = append(ci.MountVolumes, core.Volume{
		SourceVolumePath: filepath.Join(sysRunnerTempDir, "_github_workflow"),
		TargetVolumePath: dockerGithubWorkflow,
		ReadOnly:         false,
	})

	// mount home dir
	ci.MountVolumes = append(ci.MountVolumes, core.Volume{
		SourceVolumePath: filepath.Join(sysRunnerTempDir, "_github_home"),
		TargetVolumePath: dockerGithubHome,
		ReadOnly:         false,
	})

	// mount file commands dir
	ci.MountVolumes = append(ci.MountVolumes, core.Volume{
		SourceVolumePath: filepath.Join(sysRunnerTempDir, "_runner_file_commands"),
		TargetVolumePath: dockerGithubFileCommands,
		ReadOnly:         false,
	})

	exitCode, err := core.DockerRun(context.Background(), n.Data.DockerInstanceLabel, ci, workingDirectory, nil, nil)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return core.CreateErr(c, nil, "docker run failed with exit code %d", exitCode)
	}
	return nil
}

func init() {
	err := core.RegisterNodeFactory(ghActionNodeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {

		nodeType := ctx.(string)

		_, owner, repo, path, ref, err := parseNodeTypeId(nodeType)
		if err != nil {
			return nil, []error{err}
		}

		home, err := os.UserHomeDir()
		if err != nil {
			return nil, []error{core.CreateErr(nil, err, "unable to get user home directory")}
		}

		// repoRoot is where the git repository is stored locall
		// ~/work/_actions/{owner}/{repo}/{ref}
		repoRoot := filepath.Join(home, "work", "_actions", owner, repo, ref)

		// actionDir is where the action.yml lives of the action which is not always the repo root it seems
		// If the action is in the root, path is empty
		// If the action is in a subdir like "github.com/owner/repo/sub/path", path is just "sub/path"
		actionDir := filepath.Join(repoRoot, path)

		_, ok := os.LookupEnv("GITHUB_ACTIONS")
		if !ok {
			return nil, []error{core.CreateErr(nil, nil, "environment not configured yet to run GitHub Actions.").SetHint(
				"In order to run GitHub Actions, please follow the instructions at https://docs.actionforge.dev/reference/github-actions/#configure"),
			}
		}

		// Reminder that INPUT_* env vars are only prefixed for the graph execution, not here
		ghToken := os.Getenv("INPUT_TOKEN")

		// TODO: (Seb) for the validation process we only need the action.yml, not the entire repo
		// so check if we are in validate mode and only download the action.yml file
		_, err = os.Stat(repoRoot)
		if errors.Is(err, os.ErrNotExist) {
			if ghToken == "" {
				return nil, []error{core.CreateErr(nil, nil, "INPUT_TOKEN not set")}
			}

			cloneUrl := fmt.Sprintf("https://%s@github.com/%s/%s", ghToken, owner, repo)

			if err := os.MkdirAll(filepath.Dir(repoRoot), 0755); err != nil {
				return nil, []error{core.CreateErr(nil, err, "unable to create action directory")}
			}

			c := exec.Command("git", "clone", "--quiet", "--no-checkout", cloneUrl, repoRoot)
			c.Stderr = os.Stderr
			err = c.Run()
			if err != nil {
				return nil, []error{err}
			}

			c = exec.Command("git", "checkout", u.If(ref == "", "HEAD", ref))
			c.Stderr = os.Stderr
			c.Dir = repoRoot
			err = c.Run()
			if err != nil {
				return nil, []error{err}
			}
		} else {
			// reset in case something or someone tampered with the cached gh actions
			c := exec.Command("git", "reset", "--quiet", "--hard", u.If(ref == "", "HEAD", ref))
			c.Stderr = os.Stderr
			c.Dir = repoRoot
			err = c.Run()
			if err != nil {
				return nil, []error{err}
			}
		}

		// double check action.yml exists in the directory
		actionYamlPath := filepath.Join(actionDir, "action.yml")
		actionContent, err := os.ReadFile(actionYamlPath)
		if err != nil {
			// or action.yaml
			actionYamlPath = filepath.Join(actionDir, "action.yaml")
			actionContent, err = os.ReadFile(actionYamlPath)
			if err != nil {
				return nil, []error{core.CreateErr(nil, err, "unable to read action.yml or action.yaml in %s", actionDir)}
			}
		}

		var action GithubActionDefinition
		err = yaml.Unmarshal(actionContent, &action)
		if err != nil {
			return nil, []error{err}
		}

		node := &GhActionNode{
			actionName: action.Name,
			actionRuns: action.Runs,
		}

		switch action.Runs.Using {
		case "docker":
			sysWorkspaceDir := os.Getenv("GITHUB_WORKSPACE")
			if sysWorkspaceDir == "" {
				return nil, []error{core.CreateErr(nil, nil, "GITHUB_WORKSPACE not set")}
			}

			node.actionType = Docker

			if after, ok := strings.CutPrefix(action.Runs.Image, "docker://"); ok {
				dockerUrl := after
				if dockerUrl == "" {
					return nil, []error{core.CreateErr(nil, nil, "docker image not specified")}
				}

				node.Data.Image = dockerUrl
				if !validate {
					exitCode, err := core.DockerPull(context.Background(), dockerUrl, sysWorkspaceDir)
					if err != nil {
						return nil, []error{err}
					}
					if exitCode != 0 {
						return nil, []error{core.CreateErr(nil, nil, "docker pull failed with exit code %d", exitCode)}
					}
				}

			} else {
				executionContextId := uuid.New()
				runnersDir, err := getRunnersDir()
				if err != nil {
					return nil, []error{err}
				}

				runnersSha256, err := u.GetSha256OfFile(filepath.Join(runnersDir, ".runner"))
				if err != nil {
					return nil, []error{err}
				}
				node.Data.DockerInstanceLabel = runnersSha256[:6]

				imageName := fmt.Sprintf("%s:%s", node.Data.DockerInstanceLabel, executionContextId.String())
				node.Data.Image = imageName
				node.Data.ExecutionStateId = executionContextId.String()

				if !validate {
					utils.LogOut.Infof("%sBuild container for action use '%s'.\n", u.LogGhStartGroup, "")

					// resolve Dockerfile path relative to the action directory
					dockerFilePath := filepath.Join(actionDir, action.Runs.Image)

					// Build context is usually the action directory, but we pass actionDir.
					// If the Dockerfile is "../../Dockerfile", this logic handles the location of the file.
					exitCode, err := core.DockerBuild(context.Background(), actionDir, dockerFilePath, actionDir, imageName)
					if err != nil {
						return nil, []error{err}
					}

					if exitCode != 0 {
						return nil, []error{core.CreateErr(nil, nil, "docker build failed with exit code %d", exitCode)}
					}

					utils.LogOut.Infof(u.LogGhEndGroup)
				}
			}
		case "node12", "node14", "node16", "node20", "node24":
			node.actionType = Node

			// so for anyone reading this here, it took me an emberassing amount of time to notice
			// that the main file might be also in a parent directory
			actionRunFile := filepath.Join(actionDir, action.Runs.Main)

			// in that case remove the ".." just for cleaner logs
			actionRunFile = filepath.Clean(actionRunFile)

			_, err := os.Stat(actionRunFile)
			if errors.Is(err, os.ErrNotExist) {
				return nil, []error{core.CreateErr(nil, nil, "action run file does not exist: %s", actionRunFile)}
			}

			node.actionRunJsPath = actionRunFile

		case "composite":
			// we should never see a composite here, since they should have already been expanded by the editor/gateway
			// into a group node. In the future we may add support by returning the group node here in case graph
			// files were created without the editor/gateway.
			fallthrough
		default:
			return nil, []error{core.CreateErr(nil, nil, "unsupported action run type: %s", action.Runs.Using)}
		}

		inputs := make(map[core.InputId]core.InputDefinition, 0)
		if len(action.Inputs) > 0 {
			for name, input := range action.Inputs {
				pd := core.InputDefinition{
					PortDefinition: core.PortDefinition{
						Name: name,
						Type: "string",
						Desc: input.Desc,
					},
				}
				if input.Default != "" {
					pd.Default = input.Default
				}
				inputs[core.InputId(name)] = pd
			}
		}

		outputs := make(map[core.OutputId]core.OutputDefinition, 0)
		if len(action.Outputs) > 0 {
			for name, output := range action.Outputs {
				outputs[core.OutputId(name)] = core.OutputDefinition{
					PortDefinition: core.PortDefinition{
						Name: name,
						Type: "string",
						Desc: output.Description,
					},
				}
			}
		}

		inputs["exec"] = core.InputDefinition{PortDefinition: core.PortDefinition{Exec: true}}
		outputs["exec-success"] = core.OutputDefinition{PortDefinition: core.PortDefinition{Exec: true}}
		outputs["exec-err"] = core.OutputDefinition{PortDefinition: core.PortDefinition{Exec: true}}
		inputs["env"] = core.InputDefinition{
			PortDefinition: core.PortDefinition{Name: "Environment Vars", Type: "[]string"},
			Hint:           "MY_ENV=1234",
		}

		node.SetInputDefs(inputs, core.SetDefsOpts{})
		node.SetOutputDefs(outputs, core.SetDefsOpts{})
		node.SetNodeType(nodeType)
		node.SetName(action.Name)
		return node, nil
	})
	if err != nil {
		panic(err)
	}
}

func parseNodeTypeId(nodeTypeId string) (registry, owner, repo, path, ref string, err error) {
	if strings.HasPrefix(nodeTypeId, "http://") || strings.HasPrefix(nodeTypeId, "https://") {
		return "", "", "", "", "", fmt.Errorf("url must only contain the node path uri, not the full url")
	}

	matches := nodeTypeIdRegex.FindStringSubmatch(nodeTypeId)
	if len(matches) == 0 {
		return "", "", "", "", "", fmt.Errorf("invalid node type id")
	}

	// [0] = Full match
	// [1] = registry (e.g. "github.com/")
	// [2] = owner (e.g. "actions")
	// [3] = repo (e.g. "attest-build-provenance")
	// [4] = path (e.g. "/predicate") -> optional subpath inside the repo
	// [5] = ref (e.g. "@864457...")
	registry = strings.TrimSuffix(matches[1], "/")
	owner = matches[2]
	repo = matches[3]
	path = strings.TrimPrefix(matches[4], "/")
	ref = strings.TrimPrefix(matches[5], "@")

	return registry, owner, repo, path, ref, nil
}

type GithubActionDefinition struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Inputs      map[string]ActionInput  `json:"inputs"`
	Outputs     map[string]ActionOutput `json:"outputs"`
	Runs        ActionRuns              `json:"runs"`
}

type ActionInput struct {
	Default  string `json:"default"`
	Required bool   `json:"required"`
	Desc     string `json:"description"`
}

type ActionOutput struct {
	Description string `json:"description"`
}

type ActionRuns struct {
	Image string   `json:"image"`
	Using string   `json:"using"`
	Main  string   `json:"main"`
	Post  string   `json:"post"`
	Args  []string `json:"args"`
}

// getRunnersDir returns the directory of the latest runner version.
func getRunnersDir() (string, error) {

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", core.CreateErr(nil, err, "unable to get user home directory")
	}

	// First, try the path ~/actions-runner/cached
	cachedDir := filepath.Join(homeDir, "actions-runner", "cached")
	_, err = os.Stat(cachedDir)
	if err == nil {
		return cachedDir, nil
	}

	// TODO: (Seb) The code below iterates over the different runner versions
	// in the home folder to find the latest dir version. There is currently
	// no other way to find the real runner version.
	_, err = os.ReadDir(cachedDir)
	if err == nil {
		return "", core.CreateErr(nil, err, "unable to read runners directory")
	}

	// If not found, fallback to ~/runners, not sure when they changed the directory structure.
	files, err := os.ReadDir(filepath.Join(homeDir, "runners"))
	if err != nil {
		return "", core.CreateErr(nil, err, "unable to read runners directory")
	}

	var highestVersion *semver.Version
	var highestVersionDir string

	for _, file := range files {
		if file.IsDir() {
			ver, err := semver.NewVersion(file.Name())
			if err == nil {
				if highestVersion == nil || ver.GreaterThan(highestVersion) {
					highestVersion = ver
					highestVersionDir = file.Name()
				}
			}
		}
	}

	if highestVersion == nil {
		return "", core.CreateErr(nil, nil, "no valid semantic version directories found")
	}

	return filepath.Join(homeDir, "runners", highestVersionDir), nil
}

// https://github.com/actions/runner/blob/f467e9e1255530d3bf2e33f580d041925ab01951/src/Runner.Worker/GitHubContext.cs#L9
var contextEnvAllowList = map[string]struct{}{
	"action_path":         {},
	"action_ref":          {},
	"action_repository":   {},
	"action":              {},
	"actor":               {},
	"actor_id":            {},
	"api_url":             {},
	"base_ref":            {},
	"env":                 {},
	"event_name":          {},
	"event_path":          {},
	"graphql_url":         {},
	"head_ref":            {},
	"job":                 {},
	"output":              {},
	"path":                {},
	"ref_name":            {},
	"ref_protected":       {},
	"ref_type":            {},
	"ref":                 {},
	"repository":          {},
	"repository_id":       {},
	"repository_owner":    {},
	"repository_owner_id": {},
	"retention_days":      {},
	"run_attempt":         {},
	"run_id":              {},
	"run_number":          {},
	"server_url":          {},
	"sha":                 {},
	"state":               {},
	"step_summary":        {},
	"triggering_actor":    {},
	"workflow":            {},
	"workflow_ref":        {},
	"workflow_sha":        {},
	"workspace":           {},
}
