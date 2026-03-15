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

// OpenAIProvider implements the Provider interface for OpenAI-compatible APIs.
// Works with OpenAI, Azure OpenAI, and any OpenAI-compatible endpoint.
type OpenAIProvider struct {
	id      string
	baseURL string
	apiKey  string
	client  *http.Client
	models  []ModelInfo
}

// OpenAIConfig configures an OpenAI provider instance.
type OpenAIConfig struct {
	ID      string
	BaseURL string // Default: "https://api.openai.com/v1"
	APIKey  string
	Timeout time.Duration
}

// NewOpenAIProvider creates a new OpenAI-compatible provider.
func NewOpenAIProvider(cfg OpenAIConfig) *OpenAIProvider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	return &OpenAIProvider{
		id:      cfg.ID,
		baseURL: baseURL,
		apiKey:  cfg.APIKey,
		client:  &http.Client{Timeout: timeout},
		models: []ModelInfo{
			{ID: "gpt-4o", Name: "GPT-4o", Provider: "openai", ContextSize: 128000, CostPer1KPromptTokens: 0.005, CostPer1KCompletionTokens: 0.015},
			{ID: "gpt-4o-mini", Name: "GPT-4o Mini", Provider: "openai", ContextSize: 128000, CostPer1KPromptTokens: 0.00015, CostPer1KCompletionTokens: 0.0006},
		},
	}
}

func (p *OpenAIProvider) ID() string   { return p.id }
func (p *OpenAIProvider) Name() string { return "OpenAI" }
func (p *OpenAIProvider) Models() []ModelInfo { return p.models }

func (p *OpenAIProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	payload := openAIChatPayload{
		Model:       req.Model,
		Messages:    toOpenAIMessages(req.Messages),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		TopP:        req.TopP,
		Stop:        req.Stop,
		Stream:      false,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

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

	var oaiResp openAIChatResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	content := ""
	if len(oaiResp.Choices) > 0 {
		content = oaiResp.Choices[0].Message.Content
	}

	return &ChatResponse{
		ID:      oaiResp.ID,
		Model:   oaiResp.Model,
		Content: content,
		Usage: Usage{
			PromptTokens:     oaiResp.Usage.PromptTokens,
			CompletionTokens: oaiResp.Usage.CompletionTokens,
			TotalTokens:      oaiResp.Usage.TotalTokens,
		},
		CreatedAt: time.Now(),
	}, nil
}

func (p *OpenAIProvider) StreamChat(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	payload := openAIChatPayload{
		Model:       req.Model,
		Messages:    toOpenAIMessages(req.Messages),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      true,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

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
		p.readSSEStream(resp.Body, ch)
	}()

	return ch, nil
}

func (p *OpenAIProvider) readSSEStream(r io.Reader, ch chan<- StreamChunk) {
	buf := make([]byte, 4096)
	var partial []byte

	for {
		n, err := r.Read(buf)
		if n > 0 {
			partial = append(partial, buf[:n]...)
			// Process complete SSE events
			for {
				idx := bytes.Index(partial, []byte("\n\n"))
				if idx == -1 {
					break
				}
				event := string(partial[:idx])
				partial = partial[idx+2:]

				for _, line := range bytes.Split([]byte(event), []byte("\n")) {
					if !bytes.HasPrefix(line, []byte("data: ")) {
						continue
					}
					data := string(line[6:])
					if data == "[DONE]" {
						ch <- StreamChunk{Done: true}
						return
					}
					var chunk openAIStreamChunk
					if jsonErr := json.Unmarshal([]byte(data), &chunk); jsonErr == nil {
						if len(chunk.Choices) > 0 {
							ch <- StreamChunk{
								Content: chunk.Choices[0].Delta.Content,
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

func (p *OpenAIProvider) HealthCheck(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: status %d", resp.StatusCode)
	}
	return nil
}

// ---- OpenAI-specific types ----

type openAIChatPayload struct {
	Model       string            `json:"model"`
	Messages    []openAIMessage   `json:"messages"`
	Temperature *float64          `json:"temperature,omitempty"`
	MaxTokens   *int              `json:"max_tokens,omitempty"`
	TopP        *float64          `json:"top_p,omitempty"`
	Stop        []string          `json:"stop,omitempty"`
	Stream      bool              `json:"stream"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

func toOpenAIMessages(msgs []Message) []openAIMessage {
	out := make([]openAIMessage, len(msgs))
	for i, m := range msgs {
		out[i] = openAIMessage{Role: string(m.Role), Content: m.Content}
	}
	return out
}
