// Package agent implements the autonomous Web3 intent parsing and command execution
// utilizing Cloudflare Workers AI (Llama 3.1) and ElevenLabs.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Intent represents a strictly parsed on-chain action.
type Intent struct {
	Action    string  `json:"action"`     // e.g., "PAYMENT", "LEND", "BORROW"
	Amount    float64 `json:"amount"`     // Amount in USDC
	Recipient string  `json:"recipient"`  // Destination address or ENS
	Network   string  `json:"network"`    // e.g., "ARC_TESTNET"
	Details   string  `json:"details"`    // Additional context
}

// Config holds credentials for the AI agent stack.
type Config struct {
	CloudflareAccountID string
	CloudflareAPIToken  string
	ElevenLabsAPIKey    string
	ModelID             string // Default: @cf/meta/llama-3.1-8b-instruct
}

// Engine processes natural language (text or voice) into actionable Web3 intents.
type Engine struct {
	cfg    Config
	client *http.Client
}

// NewEngine initializes the autonomous agent engine.
func NewEngine(cfg Config) *Engine {
	if cfg.ModelID == "" {
		cfg.ModelID = "@cf/meta/llama-3.1-8b-instruct"
	}
	return &Engine{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ParseIntent sends user text to Llama 3.1 on Cloudflare Workers AI and forces a JSON schema match.
func (e *Engine) ParseIntent(ctx context.Context, command string) (*Intent, error) {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/%s", e.cfg.CloudflareAccountID, e.cfg.ModelID)

// We use prompt engineering to force Llama 3.1 to output strict JSON
// matching our Intent struct, mitigating hallucinated parameters.
	systemPrompt := `You are an autonomous Web3 financial agent. 
Extract the user's intent into a strict JSON object. Do not output markdown code blocks. Output ONLY valid JSON.
Schema:
{
  "action": "PAYMENT" | "LEND" | "BORROW" | "UNKNOWN",
  "amount": number (in USDC),
  "recipient": "0x... address or name",
  "network": "ARC_TESTNET"
}
If a field is missing, infer the "action" but leave "amount" as 0 and "recipient" empty.`

	payload := map[string]interface{}{
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": command},
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+e.cfg.CloudflareAPIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Result struct {
			Response string `json:"response"`
		} `json:"result"`
		Success bool `json:"success"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("cloudflare API reported failure")
	}

	var parsedIntent Intent
	if err := json.Unmarshal([]byte(result.Result.Response), &parsedIntent); err != nil {
		return nil, fmt.Errorf("failed to parse Llama 3.1 JSON output: %w (raw: %s)", err, result.Result.Response)
	}

	return &parsedIntent, nil
}
