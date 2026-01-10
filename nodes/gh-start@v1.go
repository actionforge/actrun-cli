package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed gh-start@v1.yml
var GithubActionStartNodeDefinition string

type GhActionStartNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Outputs
}

var githubEventMap = map[string]string{
	"branch_protection_rule":      "On Branch Protection Rule",
	"check_run":                   "On Check Run",
	"check_suite":                 "On Check Suite",
	"create":                      "On Create",
	"delete":                      "On Delete",
	"deployment":                  "On Deployment",
	"deployment_status":           "On Deployment Status",
	"discussion":                  "On Discussion",
	"discussion_comment":          "On Discussion Comment",
	"fork":                        "On Fork",
	"gollum":                      "On Gollum",
	"issue_comment":               "On Issue Comment",
	"issues":                      "On Issue",
	"label":                       "On Label",
	"merge_group":                 "On Merge Group",
	"milestone":                   "On Milestone",
	"page_build":                  "On Page Build",
	"project":                     "On Project",
	"project_card":                "On Project Card",
	"project_column":              "On Project Column",
	"public":                      "On Public",
	"pull_request":                "On Pull Request",
	"pull_request_comment":        "On Pull Request Comment",
	"pull_request_review":         "On Pull Request Review",
	"pull_request_review_comment": "On Pull Request Review Comment",
	"pull_request_target":         "On Pull Request Target",
	"push":                        "On Push",
	"registry_package":            "On Registry Package",
	"release":                     "On Release",
	"repository_dispatch":         "On Repository Dispatch",
	"schedule":                    "On Schedule",
	"status":                      "On Status",
	"watch":                       "On Watch",
	"workflow_call":               "On Workflow Call",
	"workflow_dispatch":           "On Workflow Dispatch",
	"workflow_run":                "On Workflow Run",
}

const unexpectedEventErrorStr = `Connect the execution port '%s' of the start node with another node. For more information on GitHub Action events consult the documentation: 🔗 https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#%s`

func (n *GhActionStartNode) ExecuteEntry(c *core.ExecutionState, inputValues map[core.OutputId]any, args []string) error {
	core.LogDebugInfoForGh(n)
	return n.ExecuteImpl(c, "", nil)
}

func (n *GhActionStartNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	exec, err := n.GetStartOutput(c)
	if err != nil {
		return err
	}

	err = n.Execute(exec, c, nil)
	if err != nil {
		return err
	}

	return nil
}

func (n *GhActionStartNode) GetStartOutput(c *core.ExecutionState) (core.OutputId, error) {
	event := c.Env["GITHUB_EVENT_NAME"]

	var exec core.OutputId

	// All trigger events are listed here:
	// https://docs.github.com/en/actions/reference/events-that-trigger-workflows
	switch event {
	case "branch_protection_rule":
		exec = ni.Core_gh_start_v1_Output_exec_on_branch_protection_rule
	case "check_run":
		exec = ni.Core_gh_start_v1_Output_exec_on_check_run
	case "check_suite":
		exec = ni.Core_gh_start_v1_Output_exec_on_check_suite
	case "create":
		exec = ni.Core_gh_start_v1_Output_exec_on_create
	case "delete":
		exec = ni.Core_gh_start_v1_Output_exec_on_delete
	case "deployment":
		exec = ni.Core_gh_start_v1_Output_exec_on_deployment
	case "deployment_status":
		exec = ni.Core_gh_start_v1_Output_exec_on_deployment_status
	case "discussion":
		exec = ni.Core_gh_start_v1_Output_exec_on_discussion
	case "discussion_comment":
		exec = ni.Core_gh_start_v1_Output_exec_on_discussion_comment
	case "fork":
		exec = ni.Core_gh_start_v1_Output_exec_on_fork
	case "gollum":
		exec = ni.Core_gh_start_v1_Output_exec_on_gollum
	// it looks like pull_request_comment is deprecated and substituted with 'issue_comment'
	case "issue_comment", "pull_request_comment":
		exec = ni.Core_gh_start_v1_Output_exec_on_issue_comment
	case "issues":
		exec = ni.Core_gh_start_v1_Output_exec_on_issues
	case "label":
		exec = ni.Core_gh_start_v1_Output_exec_on_label
	case "merge_group":
		exec = ni.Core_gh_start_v1_Output_exec_on_merge_group
	case "milestone":
		exec = ni.Core_gh_start_v1_Output_exec_on_milestone
	case "page_build":
		exec = ni.Core_gh_start_v1_Output_exec_on_page_build
	case "project":
		exec = ni.Core_gh_start_v1_Output_exec_on_project
	case "project_card":
		exec = ni.Core_gh_start_v1_Output_exec_on_project_card
	case "project_column":
		exec = ni.Core_gh_start_v1_Output_exec_on_project_column
	case "public":
		exec = ni.Core_gh_start_v1_Output_exec_on_public
	case "pull_request":
		exec = ni.Core_gh_start_v1_Output_exec_on_pull_request
	case "pull_request_review":
		exec = ni.Core_gh_start_v1_Output_exec_on_pull_request_review
	case "pull_request_review_comment":
		exec = ni.Core_gh_start_v1_Output_exec_on_pull_request_review_comment
	case "pull_request_target":
		exec = ni.Core_gh_start_v1_Output_exec_on_pull_request_target
	case "push":
		exec = ni.Core_gh_start_v1_Output_exec_on_push
	case "registry_package":
		exec = ni.Core_gh_start_v1_Output_exec_on_registry_package
	case "release":
		exec = ni.Core_gh_start_v1_Output_exec_on_release
	case "repository_dispatch":
		exec = ni.Core_gh_start_v1_Output_exec_on_repository_dispatch
	case "schedule":
		exec = ni.Core_gh_start_v1_Output_exec_on_schedule
	case "status":
		exec = ni.Core_gh_start_v1_Output_exec_on_status
	case "watch":
		exec = ni.Core_gh_start_v1_Output_exec_on_watch
	case "workflow_call":
		exec = ni.Core_gh_start_v1_Output_exec_on_workflow_call
	case "workflow_dispatch":
		exec = ni.Core_gh_start_v1_Output_exec_on_workflow_dispatch
	case "workflow_run":
		exec = ni.Core_gh_start_v1_Output_exec_on_workflow_run
	default:
		if event == "" {
			return "", core.CreateErr(c, nil, "no event name set (GITHUB_EVENT_NAME is empty)")
		}
		return "", core.CreateErr(c, nil, "unknown event name: %s", event)
	}

	_, ok := n.GetExecutionTarget(exec)
	if !ok {
		portLabel := githubEventMap[event]
		if portLabel == "" {
			portLabel = event
		}
		return "", core.CreateErr(c, nil, "Error: No trigger port connected for event: '%s'", event).
			SetHint(unexpectedEventErrorStr, portLabel, event)
	}

	return exec, nil
}

func init() {
	err := core.RegisterNodeFactory(GithubActionStartNodeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &GhActionStartNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
