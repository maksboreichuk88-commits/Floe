package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AnthropicProvider implements the Provider interface for Anthropic's Claude API.
type AnthropicProvider struct {
	id      string
	baseURL string
	apiKey  string
	client  *http.Client
	models  []ModelInfo
}

// AnthropicConfig configures an Anthropic provider instance.
type AnthropicConfig struct {
	ID      string
	BaseURL string
	APIKey  string
	Timeout time.Duration
}

// NewAnthropicProvider creates a new Anthropic Claude provider.
func NewAnthropicProvider(cfg AnthropicConfig) *AnthropicProvider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	return &AnthropicProvider{
		id:      cfg.ID,
		baseURL: baseURL,
		apiKey:  cfg.APIKey,
		client:  &http.Client{Timeout: timeout},
		models: []ModelInfo{
			{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4", Provider: "anthropic", ContextSize: 200000, CostPer1KPromptTokens: 0.003, CostPer1KCompletionTokens: 0.015},
			{ID: "claude-3-5-haiku-20241022", Name: "Claude 3.5 Haiku", Provider: "anthropic", ContextSize: 200000, CostPer1KPromptTokens: 0.001, CostPer1KCompletionTokens: 0.005},
		},
	}
}

func (p *AnthropicProvider) ID() string   { return p.id }
func (p *AnthropicProvider) Name() string { return "Anthropic" }
func (p *AnthropicProvider) Models() []ModelInfo { return p.models }

func (p *AnthropicProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	// Convert to Anthropic's format (separate system from messages)
	systemPrompt, messages := extractSystem(req.Messages)

	payload := anthropicPayload{
		Model:    req.Model,
		Messages: toAnthropicMessages(messages),
		System:   systemPrompt,
		MaxTokens: 4096,
	}
	if req.MaxTokens != nil {
		payload.MaxTokens = *req.MaxTokens
	}
	if req.Temperature != nil {
		payload.Temperature = req.Temperature
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var antResp anthropicResponse
	if err := json.Unmarshal(respBody, &antResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	content := ""
	for _, block := range antResp.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	return &ChatResponse{
		ID:      antResp.ID,
		Model:   antResp.Model,
		Content: content,
		Usage: Usage{
			PromptTokens:     antResp.Usage.InputTokens,
			CompletionTokens: antResp.Usage.OutputTokens,
			TotalTokens:      antResp.Usage.InputTokens + antResp.Usage.OutputTokens,
		},
		CreatedAt: time.Now(),
	}, nil
}

func (p *AnthropicProvider) StreamChat(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	systemPrompt, messages := extractSystem(req.Messages)

	payload := anthropicPayload{
		Model:     req.Model,
		Messages:  toAnthropicMessages(messages),
		System:    systemPrompt,
		MaxTokens: 4096,
		Stream:    true,
	}
	if req.MaxTokens != nil {
		payload.MaxTokens = *req.MaxTokens
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamChunk, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		readAnthropicStream(resp.Body, ch)
	}()

	return ch, nil
}

func readAnthropicStream(r io.Reader, ch chan<- StreamChunk) {
	buf := make([]byte, 4096)
	var partial []byte

	for {
		n, err := r.Read(buf)
		if n > 0 {
			partial = append(partial, buf[:n]...)
			for {
				idx := bytes.Index(partial, []byte("\n\n"))
				if idx == -1 {
					break
				}
				event := partial[:idx]
				partial = partial[idx+2:]

				for _, line := range bytes.Split(event, []byte("\n")) {
					if !bytes.HasPrefix(line, []byte("data: ")) {
						continue
					}
					data := line[6:]
					var sseEvent anthropicSSEEvent
					if jsonErr := json.Unmarshal(data, &sseEvent); jsonErr == nil {
						switch sseEvent.Type {
						case "content_block_delta":
							if sseEvent.Delta.Type == "text_delta" {
								ch <- StreamChunk{Content: sseEvent.Delta.Text}
							}
						case "message_stop":
							ch <- StreamChunk{Done: true}
							return
						case "message_delta":
							if sseEvent.Usage != nil {
								ch <- StreamChunk{
									Done: true,
									Usage: &Usage{
										PromptTokens:     sseEvent.Usage.InputTokens,
										CompletionTokens: sseEvent.Usage.OutputTokens,
										TotalTokens:      sseEvent.Usage.InputTokens + sseEvent.Usage.OutputTokens,
									},
								}
								return
							}
						}
					}
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				ch <- StreamChunk{Err: err}
			}
			return
		}
	}
}

func (p *AnthropicProvider) HealthCheck(ctx context.Context) error {
	// Anthropic doesn't have a /models endpoint; use a minimal message
	payload := anthropicPayload{
		Model:     "claude-3-5-haiku-20241022",
		Messages:  []anthropicMessage{{Role: "user", Content: "ping"}},
		MaxTokens: 1,
	}

	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("health check failed: status %d", resp.StatusCode)
	}
	return nil
}

// ---- Anthropic-specific types ----

type anthropicPayload struct {
	Model       string              `json:"model"`
	Messages    []anthropicMessage  `json:"messages"`
	System      string              `json:"system,omitempty"`
	MaxTokens   int                 `json:"max_tokens"`
	Temperature *float64            `json:"temperature,omitempty"`
	Stream      bool                `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicSSEEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
}

func extractSystem(msgs []Message) (string, []Message) {
	var system string
	var filtered []Message
	for _, m := range msgs {
		if m.Role == RoleSystem {
			system = m.Content
		} else {
			filtered = append(filtered, m)
		}
	}
	return system, filtered
}

func toAnthropicMessages(msgs []Message) []anthropicMessage {
	out := make([]anthropicMessage, len(msgs))
	for i, m := range msgs {
		out[i] = anthropicMessage{Role: string(m.Role), Content: m.Content}
	}
	return out
}
