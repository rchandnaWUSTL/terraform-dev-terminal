package provider

import "context"

// Provider is the contract each model backend implements. The agent loop in
// internal/agent calls these methods without knowing which provider is wired up.
type Provider interface {
	// Name returns a stable identifier: "anthropic", "openai", or "copilot".
	Name() string

	// Authenticate prepares credentials. Called once at startup and again after
	// a 401 from SendMessage (providers that support token refresh). Must be
	// safe to call repeatedly.
	Authenticate(ctx context.Context) error

	// SendMessage runs a single turn. The returned channel emits any number of
	// EventText and EventToolUse events followed by exactly one terminal
	// EventStop or EventError, after which the channel is closed. The
	// FinalMessage on EventStop is the full assistant message, ready to be
	// appended to conversation history.
	SendMessage(ctx context.Context, req SendRequest) (<-chan StreamEvent, error)
}
