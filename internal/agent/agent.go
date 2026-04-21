package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/rchandnaWUSTL/terraform-dev/internal/config"
	"github.com/rchandnaWUSTL/terraform-dev/internal/tools"
)

const systemPrompt = `You are an AI agent for HCP Terraform. You help infrastructure engineers understand their Terraform workspaces, runs, drift, and policies by calling structured tools and explaining results in plain English.

Rules:
- You are in READ-ONLY mode. You MUST NOT trigger any run, apply, plan, or mutation.
- Call at most 4 tools per response.
- After calling tools, synthesize a clear narrative: what you found, what the risks are, and what the engineer should consider next.
- Never hallucinate resource names, run IDs, or workspace names. Only state facts from tool output.
- If a tool returns an error, explain it clearly and suggest what the user can do.
- When comparing workspaces, call _hcp_tf_workspace_diff once per workspace then compare the results yourself.
- Be concise. Engineers are on-call. One paragraph per key finding.`

type StreamChunk struct {
	Text string
	Done bool
	Err  error
}

type ToolCallEvent struct {
	Name string
	Args map[string]string
}

type Agent struct {
	client  anthropic.Client
	cfg     *config.Config
	history []anthropic.MessageParam
}

func New(cfg *config.Config) *Agent {
	client := anthropic.NewClient(
		option.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
	)
	return &Agent{client: client, cfg: cfg}
}

func (a *Agent) Ask(
	ctx context.Context,
	userMsg string,
	org, workspace string,
	onToolCall func(ToolCallEvent),
	onToolResult func(name string, result *tools.CallResult),
) (<-chan StreamChunk, error) {
	msg := userMsg
	if org != "" || workspace != "" {
		msg = fmt.Sprintf("[Context: org=%s workspace=%s]\n\n%s", org, workspace, userMsg)
	}

	a.history = append(a.history, anthropic.NewUserMessage(anthropic.NewTextBlock(msg)))

	ch := make(chan StreamChunk, 64)

	go func() {
		defer close(ch)

		for range 4 {
			done, err := a.runTurn(ctx, onToolCall, onToolResult, ch)
			if err != nil {
				ch <- StreamChunk{Err: err}
				return
			}
			if done {
				return
			}
		}
		ch <- StreamChunk{Done: true}
	}()

	return ch, nil
}

// runTurn executes one API call. Returns true when the agent is done (no more tool calls needed).
func (a *Agent) runTurn(
	ctx context.Context,
	onToolCall func(ToolCallEvent),
	onToolResult func(name string, result *tools.CallResult),
	ch chan<- StreamChunk,
) (done bool, err error) {
	toolDefs := buildToolDefs()

	stream := a.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(a.cfg.Model),
		MaxTokens: int64(a.cfg.MaxTokens),
		System:    []anthropic.TextBlockParam{{Type: "text", Text: systemPrompt}},
		Tools:     toolDefs,
		Messages:  a.history,
	})

	var acc anthropic.Message
	for stream.Next() {
		event := stream.Current()
		if err := acc.Accumulate(event); err != nil {
			return false, fmt.Errorf("accumulate: %w", err)
		}

		// Stream text deltas immediately
		if cbDelta, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
			if textDelta, ok := cbDelta.Delta.AsAny().(anthropic.TextDelta); ok {
				ch <- StreamChunk{Text: textDelta.Text}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return false, fmt.Errorf("stream error: %w", err)
	}

	a.history = append(a.history, acc.ToParam())

	if acc.StopReason != "tool_use" {
		ch <- StreamChunk{Done: true}
		return true, nil
	}

	// Execute tool calls and build tool_result blocks
	var resultBlocks []anthropic.ContentBlockParamUnion
	for _, block := range acc.Content {
		toolUse, ok := block.AsAny().(anthropic.ToolUseBlock)
		if !ok {
			continue
		}

		var rawArgs map[string]any
		_ = json.Unmarshal(toolUse.Input, &rawArgs)
		strArgs := toStringMap(rawArgs)

		if onToolCall != nil {
			onToolCall(ToolCallEvent{Name: toolUse.Name, Args: strArgs})
		}

		result := tools.Call(ctx, toolUse.Name, strArgs, a.cfg.TimeoutSeconds)

		if onToolResult != nil {
			onToolResult(toolUse.Name, result)
		}

		var content string
		isError := false
		if result.Err != nil {
			errJSON, _ := json.Marshal(result.Err)
			content = string(errJSON)
			isError = true
		} else {
			content = string(result.Output)
		}

		resultBlocks = append(resultBlocks, anthropic.NewToolResultBlock(toolUse.ID, content, isError))
	}

	a.history = append(a.history, anthropic.NewUserMessage(resultBlocks...))
	return false, nil
}

func (a *Agent) Reset() {
	a.history = nil
}

func buildToolDefs() []anthropic.ToolUnionParam {
	defs := tools.Definitions()
	out := make([]anthropic.ToolUnionParam, len(defs))
	for i, d := range defs {
		schemaJSON, _ := json.Marshal(d.InputSchema)
		var schema anthropic.ToolInputSchemaParam
		_ = json.Unmarshal(schemaJSON, &schema)
		out[i] = anthropic.ToolUnionParamOfTool(schema, d.Name)
		out[i].OfTool.Description = anthropic.String(d.Description)
	}
	return out
}

func toStringMap(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = fmt.Sprintf("%v", v)
	}
	return out
}
