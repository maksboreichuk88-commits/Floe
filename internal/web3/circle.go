// Package web3 provides the integration with the Circle SDK and on-chain
// execution layers for the autonomous Web3 Payment Agent.
package web3

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// CircleConfig holds the credentials for the Circle developer platform.
type CircleConfig struct {
	APIKey      string
	Environment string // "sandbox" or "production"
	EntityID    string
}

// Client interacts with the Circle Developer-Controlled Wallets API.
type Client struct {
	cfg    CircleConfig
	client *http.Client
	baseURL string
}

// NewClient initializes a new Circle Web3 Services client.
func NewClient(cfg CircleConfig) *Client {
	baseURL := "https://api.circle.com"
	if cfg.Environment == "sandbox" {
		baseURL = "https://api-sandbox.circle.com"
	}

	return &Client{
		cfg:    cfg,
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

// CreatePayment initiates a USDC transfer from the agent's developer-controlled wallet.
func (c *Client) CreatePayment(ctx context.Context, walletID string, destinationAddress string, amount string, feeLevel string) (string, error) {
	// 1. Fetch Ciphertext (PublicKey for encrypting the wallet PIN/Entity Secret)
	pubKey, keyID, err := c.getPublicKey(ctx)
	if err != nil {
		return "", fmt.Errorf("getting public key: %w", err)
	}

	// 2. Generate Ciphertext using a 32-byte Entity Secret
	// Note: In production, the EntitySecret is loaded securely from the Vault.
	// For this abstraction, we assume a placeholder secret generation for the API call.
	entitySecret := make([]byte, 32)
	rand.Read(entitySecret) // Placeholder. MUST be 32 bytes hex in prod.
	
	encryptedSecret, err := rsa.EncryptOAEP(nil, rand.Reader, pubKey, entitySecret, nil)
	if err != nil {
		return "", fmt.Errorf("encrypting entity secret: %w", err)
	}
	entitySecretCiphertext := base64.StdEncoding.EncodeToString(encryptedSecret)

	// 3. Create the Transaction payload
	url := fmt.Sprintf("%s/v1/w3s/developer/transactions/transfer", c.baseURL)

	payload := map[string]interface{}{
		"idempotencyKey": uuid.New().String(),
		"entitySecretCiphertext": entitySecretCiphertext,
		"walletId": walletID,
		"destinationAddress": destinationAddress,
		"amounts": []string{amount},
		"tokenId": "USDC_TOKEN_ID_ON_ARC_TESTNET", // Placeholder for actual Arc Testnet USDC token ID
		"feeLevel": feeLevel,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshaling transfer payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("creating transfer request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing transfer request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("circle API transfer failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding transfer response: %w", err)
	}

	return result.Data.ID, nil
}

// getPublicKey retrieves the RSA public key from Circle to encrypt the Entity Secret.
func (c *Client) getPublicKey(ctx context.Context) (*rsa.PublicKey, string, error) {
	url := fmt.Sprintf("%s/v1/w3s/config/entity/publicKey", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, "", err
	}

	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("failed fetching public key (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			PublicKey string `json:"publicKey"`
			KeyID     string `json:"keyId"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, "", err
	}

// Note: Needs full PEM parsing implementation here. Returning placeholders for architecture mockup.
	return &rsa.PublicKey{}, result.Data.KeyID, nil
}
