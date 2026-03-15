package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:            "127.0.0.1",
			Port:            4400,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    120 * time.Second,
			MaxRequestSize:  4 * 1024 * 1024, // 4 MB
			ShutdownTimeout: 15 * time.Second,
		},
		Routing: RoutingConfig{
			Strategy:      "priority",
			RetryAttempts: 2,
			RetryDelay:    500 * time.Millisecond,
		},
		Budget: BudgetConfig{
			Enabled: true,
		},
		Audit: AuditConfig{
			Enabled:       true,
			DBPath:        "./data/audit.db",
			LogBodies:     false,
			RetentionDays: 90,
		},
		Vault: VaultConfig{
			Enabled: true,
			Path:    "./data/vault.enc",
		},
		Workflow: WorkflowConfig{
			Enabled:     true,
			Dir:         "./workflows/",
			MaxSteps:    1000,
			MaxDuration: 5 * time.Minute,
			MaxTokens:   100000,
		},
		Dashboard: DashboardConfig{
			Enabled: true,
			Port:    4401,
		},
		Security: SecurityConfig{
			PIIRedaction: false,
			CircuitBreaker: CircuitBreakerConfig{
				FailureThreshold: 5,
				RecoveryTimeout:  30 * time.Second,
				SuccessThreshold: 3,
			},
		},
	}
}

// Load reads and parses a YAML config file, merging with defaults.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil // Use defaults if no config file exists
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

// Validate performs structural validation on a loaded Config.
func Validate(cfg *Config) error {
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535, got %d", cfg.Server.Port)
	}

	if cfg.Server.Host != "127.0.0.1" && cfg.Server.Host != "localhost" {
		if cfg.Security.AuthToken == "" {
			return fmt.Errorf("security.auth_token is required when binding to non-localhost address %q", cfg.Server.Host)
		}
	}

	if cfg.Server.MaxRequestSize <= 0 {
		return fmt.Errorf("server.max_request_size must be positive")
	}

	for i, p := range cfg.Providers {
		if p.ID == "" {
			return fmt.Errorf("providers[%d].id is required", i)
		}
		if p.Type == "" {
			return fmt.Errorf("providers[%d].type is required", i)
		}
		validTypes := map[string]bool{
			"openai": true, "anthropic": true, "ollama": true,
			"vllm": true, "generic": true, "mock": true,
		}
		if !validTypes[p.Type] {
			return fmt.Errorf("providers[%d].type %q is not valid (expected openai|anthropic|ollama|vllm|generic|mock)", i, p.Type)
		}
	}

	if cfg.Workflow.MaxSteps <= 0 {
		return fmt.Errorf("workflow.max_steps must be positive")
	}

	if cfg.Workflow.MaxDuration <= 0 {
		return fmt.Errorf("workflow.max_duration must be positive")
	}

	return nil
}

// ComputeChecksum computes the SHA-256 checksum of a config file.
func ComputeChecksum(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading file for checksum: %w", err)
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// VerifyIntegrity checks the config file against its .floe.lock checksum.
func VerifyIntegrity(configPath, lockPath string) error {
	currentChecksum, err := ComputeChecksum(configPath)
	if err != nil {
		return fmt.Errorf("computing current checksum: %w", err)
	}

	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("lock file %q not found; run 'floe config seal' to create it", lockPath)
		}
		return fmt.Errorf("reading lock file: %w", err)
	}

	storedChecksum := string(lockData)
	if currentChecksum != storedChecksum {
		return fmt.Errorf(
			"config integrity check failed: expected checksum %s, got %s. "+
				"Run 'floe config seal' to update, or use --force to bypass",
			storedChecksum, currentChecksum,
		)
	}

	return nil
}

// Seal writes the SHA-256 checksum of the config file to a lock file.
func Seal(configPath, lockPath string) error {
	checksum, err := ComputeChecksum(configPath)
	if err != nil {
		return err
	}
	return os.WriteFile(lockPath, []byte(checksum), 0644)
}
