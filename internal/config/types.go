// Package config handles loading, validation, and integrity verification
// of Floe configuration files.
package config

import "time"

// Config is the top-level Floe configuration structure, typically loaded from floe.yaml.
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Providers []ProviderConf  `yaml:"providers"`
	Routing   RoutingConfig   `yaml:"routing"`
	Budget    BudgetConfig    `yaml:"budget"`
	Audit     AuditConfig     `yaml:"audit"`
	Vault     VaultConfig     `yaml:"vault"`
	Workflow  WorkflowConfig  `yaml:"workflow"`
	Dashboard DashboardConfig `yaml:"dashboard"`
	Security  SecurityConfig  `yaml:"security"`
}

// ServerConfig controls the HTTP gateway listener.
type ServerConfig struct {
	Host            string        `yaml:"host"` // Default: "127.0.0.1"
	Port            int           `yaml:"port"` // Default: 4400
	ReadTimeout     time.Duration `yaml:"read_timeout"`     // Default: 30s
	WriteTimeout    time.Duration `yaml:"write_timeout"`    // Default: 120s
	MaxRequestSize  int64         `yaml:"max_request_size"` // Default: 4194304 (4 MB)
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"` // Default: 15s
}

// ProviderConf defines a single LLM provider connection.
type ProviderConf struct {
	ID       string            `yaml:"id"`       // Unique identifier like "openai-main"
	Type     string            `yaml:"type"`     // "openai", "anthropic", "ollama", "vllm", "generic"
	BaseURL  string            `yaml:"base_url"` // API base URL
	APIKey   string            `yaml:"api_key"`  // Direct key or vault reference "$vault:key-name"
	Models   []string          `yaml:"models"`   // Allowed models. Empty = all.
	Priority int               `yaml:"priority"` // Lower = higher priority for routing
	Weight   int               `yaml:"weight"`   // Relative weight for weighted round-robin
	Timeout  time.Duration     `yaml:"timeout"`  // Per-request timeout. Default: 60s
	Headers  map[string]string `yaml:"headers"`  // Custom headers to send
}

// RoutingConfig controls how requests are distributed across providers.
type RoutingConfig struct {
	Strategy       string        `yaml:"strategy"`        // "priority", "round-robin", "least-latency", "cost-optimized"
	RetryAttempts  int           `yaml:"retry_attempts"`  // Default: 2
	RetryDelay     time.Duration `yaml:"retry_delay"`     // Default: 500ms
	FallbackOrder  []string      `yaml:"fallback_order"`  // Provider IDs in fallback priority
}

// CircuitBreakerConfig controls per-provider circuit breaker behavior.
type CircuitBreakerConfig struct {
	FailureThreshold int           `yaml:"failure_threshold"` // Default: 5
	RecoveryTimeout  time.Duration `yaml:"recovery_timeout"`  // Default: 30s
	SuccessThreshold int           `yaml:"success_threshold"` // Default: 3
}

// BudgetConfig controls cost metering and limits.
type BudgetConfig struct {
	Enabled       bool                `yaml:"enabled"`        // Default: true
	GlobalLimit   *BudgetLimit        `yaml:"global_limit"`   // Optional global cap
	ProjectLimits map[string]BudgetLimit `yaml:"project_limits"` // Per-project caps
}

// BudgetLimit defines a spending cap over a time window.
type BudgetLimit struct {
	MaxTokens int           `yaml:"max_tokens"`
	MaxCostUSD float64      `yaml:"max_cost_usd"`
	Window     time.Duration `yaml:"window"` // e.g., "24h", "720h" (30 days)
}

// AuditConfig controls request/response logging.
type AuditConfig struct {
	Enabled     bool   `yaml:"enabled"`     // Default: true
	DBPath      string `yaml:"db_path"`     // Default: "./data/audit.db"
	LogBodies   bool   `yaml:"log_bodies"`  // Default: false (privacy-preserving)
	RetentionDays int  `yaml:"retention_days"` // Default: 90
}

// VaultConfig controls the encrypted API key store.
type VaultConfig struct {
	Enabled  bool   `yaml:"enabled"`   // Default: true
	Path     string `yaml:"path"`      // Default: "./data/vault.enc"
}

// WorkflowConfig controls the YAML workflow engine.
type WorkflowConfig struct {
	Enabled     bool          `yaml:"enabled"`       // Default: true
	Dir         string        `yaml:"dir"`           // Default: "./workflows/"
	MaxSteps    int           `yaml:"max_steps"`     // Default: 1000
	MaxDuration time.Duration `yaml:"max_duration"`  // Default: 5m
	MaxTokens   int           `yaml:"max_tokens"`    // Default: 100000
}

// DashboardConfig controls the built-in monitoring UI.
type DashboardConfig struct {
	Enabled bool   `yaml:"enabled"` // Default: true
	Port    int    `yaml:"port"`    // Default: 4401
}

// SecurityConfig holds security-related settings.
type SecurityConfig struct {
	AuthToken      string              `yaml:"auth_token"`     // Required if host != 127.0.0.1
	PIIRedaction   bool                `yaml:"pii_redaction"`  // Default: false
	CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker"`
}
