package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type ToolError struct {
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func (e *ToolError) Error() string { return e.Message }

type CallResult struct {
	ToolName string
	Args     map[string]string
	Output   json.RawMessage
	Err      *ToolError
	Duration time.Duration
}

func Call(ctx context.Context, name string, args map[string]string, timeoutSec int) *CallResult {
	start := time.Now()
	result := &CallResult{ToolName: name, Args: args}

	cmdArgs, err := buildArgs(name, args)
	if err != nil {
		result.Err = &ToolError{ErrorCode: "invalid_tool", Message: err.Error()}
		result.Duration = time.Since(start)
		return result
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "hcptf", cmdArgs...)
	out, execErr := cmd.Output()
	result.Duration = time.Since(start)

	if execErr != nil {
		var exitErr *exec.ExitError
		retryable := false
		msg := execErr.Error()
		if e, ok := execErr.(*exec.ExitError); ok {
			exitErr = e
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if stderr != "" {
				msg = stderr
			}
			retryable = exitErr.ExitCode() == 1
		}
		if ctx.Err() != nil {
			msg = fmt.Sprintf("tool timed out after %ds", timeoutSec)
			retryable = true
		}
		result.Err = &ToolError{ErrorCode: "execution_error", Message: msg, Retryable: retryable}
		return result
	}

	result.Output = json.RawMessage(out)
	return result
}

func buildArgs(toolName string, args map[string]string) ([]string, error) {
	switch toolName {
	case "_hcp_tf_runs_list_recent":
		return runsListRecent(args)
	case "_hcp_tf_workspace_diff":
		return workspaceDiff(args)
	case "_hcp_tf_workspace_describe":
		return workspaceDescribe(args)
	case "_hcp_tf_drift_detect":
		return driftDetect(args)
	case "_hcp_tf_policy_check":
		return policyCheck(args)
	case "_hcp_tf_plan_summary":
		return planSummary(args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

func require(args map[string]string, keys ...string) error {
	for _, k := range keys {
		if args[k] == "" {
			return fmt.Errorf("missing required argument: %s", k)
		}
	}
	return nil
}

func runsListRecent(args map[string]string) ([]string, error) {
	if err := require(args, "org", "workspace"); err != nil {
		return nil, err
	}
	return []string{"run", "list",
		"-org=" + args["org"],
		"-workspace=" + args["workspace"],
		"-output=json",
	}, nil
}

func workspaceDiff(args map[string]string) ([]string, error) {
	if err := require(args, "org", "workspace"); err != nil {
		return nil, err
	}
	return []string{"state", "list",
		"-org=" + args["org"],
		"-workspace=" + args["workspace"],
		"-output=json",
	}, nil
}

func workspaceDescribe(args map[string]string) ([]string, error) {
	if err := require(args, "org", "workspace"); err != nil {
		return nil, err
	}
	return []string{"workspace", "read",
		"-org=" + args["org"],
		"-name=" + args["workspace"],
		"-output=json",
	}, nil
}

func driftDetect(args map[string]string) ([]string, error) {
	if err := require(args, "org", "workspace"); err != nil {
		return nil, err
	}
	return []string{"assessmentresult", "list",
		"-org=" + args["org"],
		"-name=" + args["workspace"],
		"-output=json",
	}, nil
}

func policyCheck(args map[string]string) ([]string, error) {
	if err := require(args, "run_id"); err != nil {
		return nil, err
	}
	return []string{"policycheck", "list",
		"-run-id=" + args["run_id"],
		"-output=json",
	}, nil
}

func planSummary(args map[string]string) ([]string, error) {
	if err := require(args, "run_id"); err != nil {
		return nil, err
	}
	return []string{"plan", "read",
		"-run-id=" + args["run_id"],
		"-output=json",
	}, nil
}

// Definitions returns the tool definitions for the Anthropic tool_use API.
func Definitions() []ToolDef {
	return []ToolDef{
		{
			Name:        "_hcp_tf_runs_list_recent",
			Description: "Lists the most recent runs for a workspace with status, timestamps, resource counts, and cost delta.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"org":       map[string]any{"type": "string", "description": "HCP Terraform organization name"},
					"workspace": map[string]any{"type": "string", "description": "Workspace name"},
				},
				"required": []string{"org", "workspace"},
			},
		},
		{
			Name:        "_hcp_tf_workspace_diff",
			Description: "Compares two workspaces: missing resources, config drift, variable mismatches. Call twice (once per workspace) and the agent diffs the results.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"org":       map[string]any{"type": "string", "description": "HCP Terraform organization name"},
					"workspace": map[string]any{"type": "string", "description": "Workspace name to fetch state for"},
				},
				"required": []string{"org", "workspace"},
			},
		},
		{
			Name:        "_hcp_tf_workspace_describe",
			Description: "Returns workspace topology: resource types, providers, last run status, drift indicator, and state outputs.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"org":       map[string]any{"type": "string", "description": "HCP Terraform organization name"},
					"workspace": map[string]any{"type": "string", "description": "Workspace name"},
				},
				"required": []string{"org", "workspace"},
			},
		},
		{
			Name:        "_hcp_tf_drift_detect",
			Description: "Returns assessment results for a workspace showing detected drift and changed resources.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"org":       map[string]any{"type": "string", "description": "HCP Terraform organization name"},
					"workspace": map[string]any{"type": "string", "description": "Workspace name"},
				},
				"required": []string{"org", "workspace"},
			},
		},
		{
			Name:        "_hcp_tf_policy_check",
			Description: "Returns policy check results for a run: which checks passed/failed, which rules fired.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"run_id": map[string]any{"type": "string", "description": "Run ID (run-xxx)"},
				},
				"required": []string{"run_id"},
			},
		},
		{
			Name:        "_hcp_tf_plan_summary",
			Description: "Returns a summary of a plan: adds/changes/destroys, flagged risks.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"run_id": map[string]any{"type": "string", "description": "Run ID (run-xxx)"},
				},
				"required": []string{"run_id"},
			},
		},
	}
}

type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]any
}
