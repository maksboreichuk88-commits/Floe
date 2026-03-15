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

// OllamaProvider implements the Provider interface for local Ollama instances.
type OllamaProvider struct {
	id      string
	baseURL string
	client  *http.Client
}

// OllamaConfig configures an Ollama provider instance.
type OllamaConfig struct {
	ID      string
	BaseURL string // Default: "http://localhost:11434"
	Timeout time.Duration
}

// NewOllamaProvider creates a new Ollama provider.
func NewOllamaProvider(cfg OllamaConfig) *OllamaProvider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}

	return &OllamaProvider{
		id:      cfg.ID,
		baseURL: baseURL,
		client:  &http.Client{Timeout: timeout},
	}
}

func (p *OllamaProvider) ID() string   { return p.id }
func (p *OllamaProvider) Name() string { return "Ollama" }

func (p *OllamaProvider) Models() []ModelInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/api/tags", nil)
	if err != nil {
		return nil
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}

	models := make([]ModelInfo, len(result.Models))
	for i, m := range result.Models {
		models[i] = ModelInfo{
			ID:       m.Name,
			Name:     m.Name,
			Provider: "ollama",
		}
	}
	return models
}

func (p *OllamaProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	// Use Ollama's OpenAI-compatible endpoint
	payload := openAIChatPayload{
		Model:       req.Model,
		Messages:    toOpenAIMessages(req.Messages),
		Temperature: req.Temperature,
		Stream:      false,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

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
		return nil, fmt.Errorf("Ollama error (status %d): %s", resp.StatusCode, string(respBody))
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

func (p *OllamaProvider) StreamChat(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	payload := openAIChatPayload{
		Model:       req.Model,
		Messages:    toOpenAIMessages(req.Messages),
		Temperature: req.Temperature,
		Stream:      true,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("Ollama error (status %d): %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamChunk, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		readOpenAICompatStream(resp.Body, ch)
	}()

	return ch, nil
}

func (p *OllamaProvider) HealthCheck(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ollama unreachable: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama health check failed: status %d", resp.StatusCode)
	}
	return nil
}

// readOpenAICompatStream reads an OpenAI-compatible SSE stream.
// Shared between Ollama, vLLM, and generic providers.
func readOpenAICompatStream(r io.Reader, ch chan<- StreamChunk) {
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
					data := string(line[6:])
					if data == "[DONE]" {
						ch <- StreamChunk{Done: true}
						return
					}
					var chunk openAIStreamChunk
					if jsonErr := json.Unmarshal([]byte(data), &chunk); jsonErr == nil {
						if len(chunk.Choices) > 0 {
							ch <- StreamChunk{Content: chunk.Choices[0].Delta.Content}
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
