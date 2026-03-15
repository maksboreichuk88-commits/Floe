// Package provider defines the unified interface for all LLM provider adapters.
// Every provider (OpenAI, Anthropic, Ollama, etc.) must implement the Provider
// interface to be usable by the Floe gateway router.
package provider

import (
	"context"
	"time"
)

// Provider is the core contract that every LLM backend must satisfy.
// Implementations are expected to be safe for concurrent use.
type Provider interface {
	// ID returns the unique identifier for this provider instance.
	// Example: "openai-primary", "ollama-local".
	ID() string

	// Name returns the human-readable provider type name.
	// Example: "OpenAI", "Anthropic", "Ollama".
	Name() string

	// Chat sends a synchronous chat completion request and returns the full response.
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

	// StreamChat sends a streaming chat completion request.
	// The returned channel emits chunks until complete, then closes.
	// Callers must drain the channel or cancel the context.
	StreamChat(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error)

	// Models returns the list of models available from this provider.
	Models() []ModelInfo

	// HealthCheck verifies the provider is reachable and responsive.
	// Implementations should use a lightweight request (e.g., list models).
	HealthCheck(ctx context.Context) error
}

// ChatRequest represents a unified chat completion request across all providers.
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []Message     `json:"messages"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	TopP        *float64      `json:"top_p,omitempty"`
	Stop        []string      `json:"stop,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
	Metadata    RequestMeta   `json:"-"`
}

// RequestMeta holds internal tracking metadata not sent to providers.
type RequestMeta struct {
	RequestID  string
	ProjectID  string
	StartTime  time.Time
	ProviderID string
}

// Message represents a single message in a chat conversation.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// Role is the sender identity in a chat message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// ChatResponse is the unified response from a chat completion.
type ChatResponse struct {
	ID        string    `json:"id"`
	Model     string    `json:"model"`
	Content   string    `json:"content"`
	Usage     Usage     `json:"usage"`
	Latency   time.Duration `json:"-"`
	Provider  string    `json:"provider"`
	CreatedAt time.Time `json:"created_at"`
}

// Usage tracks token consumption for a single request.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk represents a single piece of a streaming response.
type StreamChunk struct {
	Content string `json:"content"`
	Done    bool   `json:"done"`
	Err     error  `json:"-"`
	Usage   *Usage `json:"usage,omitempty"` // Only set on the final chunk.
}

// ModelInfo describes a model available through a provider.
type ModelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	ContextSize int    `json:"context_size"`
	// CostPer1KPromptTokens in USD. Zero if unknown or free (e.g., local models).
	CostPer1KPromptTokens float64 `json:"cost_per_1k_prompt_tokens"`
	// CostPer1KCompletionTokens in USD.
	CostPer1KCompletionTokens float64 `json:"cost_per_1k_completion_tokens"`
}

// ProviderStatus represents the current operational state of a provider.
type ProviderStatus struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Healthy        bool          `json:"healthy"`
	CircuitState   string        `json:"circuit_state"` // "closed", "open", "half-open"
	AvgLatency     time.Duration `json:"avg_latency"`
	ErrorRate      float64       `json:"error_rate"` // 0.0 - 1.0
	RequestCount   int64         `json:"request_count"`
	LastError      string        `json:"last_error,omitempty"`
	LastCheckedAt  time.Time     `json:"last_checked_at"`
}
