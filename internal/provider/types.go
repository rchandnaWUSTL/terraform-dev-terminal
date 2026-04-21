package provider

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type BlockType string

const (
	BlockText       BlockType = "text"
	BlockToolUse    BlockType = "tool_use"
	BlockToolResult BlockType = "tool_result"
)

// ContentBlock is provider-neutral. Exactly one of Text, ToolUse, or ToolResult
// is populated based on Type.
type ContentBlock struct {
	Type       BlockType
	Text       string
	ToolUse    *ToolUseBlock
	ToolResult *ToolResultBlock
}

type ToolUseBlock struct {
	ID    string
	Name  string
	Input map[string]any
}

type ToolResultBlock struct {
	ToolUseID string
	Content   string
	IsError   bool
}

type Message struct {
	Role    Role
	Content []ContentBlock
}

// ToolDefinition is a provider-neutral tool schema. The InputSchema is a standard
// JSON Schema object (map[string]any) — the same shape used by both Anthropic's
// tool_use API and OpenAI's function-calling API, so it can be handed to either
// provider without transformation.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]any
}

type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
)

type SendRequest struct {
	Model        string
	SystemPrompt string
	Messages     []Message
	Tools        []ToolDefinition
	MaxTokens    int
}

type StreamEventType string

const (
	EventText    StreamEventType = "text"
	EventToolUse StreamEventType = "tool_use"
	EventStop    StreamEventType = "stop"
	EventError   StreamEventType = "error"
)

// StreamEvent is emitted by Provider.SendMessage on the returned channel. The
// channel emits any number of EventText and EventToolUse events followed by
// exactly one terminal EventStop or EventError, after which it is closed.
type StreamEvent struct {
	Type         StreamEventType
	TextDelta    string
	ToolUse      *ToolUseBlock
	StopReason   StopReason
	FinalMessage *Message
	Err          error
}
