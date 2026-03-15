// Package budget implements token metering, cost calculation, and rate limiting.
package budget

import (
	"fmt"
	"sync"
	"time"
)

// Meter tracks token usage and estimated costs across providers and projects.
type Meter struct {
	mu       sync.RWMutex
	records  []UsageRecord
	models   map[string]ModelCost
}

// ModelCost stores pricing info for a model.
type ModelCost struct {
	CostPer1KPromptTokens     float64
	CostPer1KCompletionTokens float64
}

// UsageRecord represents token usage from a single request.
type UsageRecord struct {
	Timestamp        time.Time     `json:"timestamp"`
	RequestID        string        `json:"request_id"`
	ProviderID       string        `json:"provider_id"`
	Model            string        `json:"model"`
	ProjectID        string        `json:"project_id"`
	PromptTokens     int           `json:"prompt_tokens"`
	CompletionTokens int           `json:"completion_tokens"`
	TotalTokens      int           `json:"total_tokens"`
	EstimatedCostUSD float64       `json:"estimated_cost_usd"`
	Latency          time.Duration `json:"latency"`
}

// UsageSummary provides aggregated usage statistics.
type UsageSummary struct {
	TotalRequests    int                       `json:"total_requests"`
	TotalTokens      int                       `json:"total_tokens"`
	PromptTokens     int                       `json:"prompt_tokens"`
	CompletionTokens int                       `json:"completion_tokens"`
	TotalCostUSD     float64                   `json:"total_cost_usd"`
	AvgTokensPerReq  float64                   `json:"avg_tokens_per_request"`
	AvgLatency       float64                   `json:"avg_latency_ms"`
	ByProvider       map[string]*ProviderUsage `json:"by_provider"`
	ByModel          map[string]*ModelUsage    `json:"by_model"`
}

// ProviderUsage holds per-provider usage stats.
type ProviderUsage struct {
	Requests     int     `json:"requests"`
	TotalTokens  int     `json:"total_tokens"`
	TotalCostUSD float64 `json:"total_cost_usd"`
}

// ModelUsage holds per-model usage stats.
type ModelUsage struct {
	Requests     int     `json:"requests"`
	TotalTokens  int     `json:"total_tokens"`
	TotalCostUSD float64 `json:"total_cost_usd"`
}

// NewMeter creates a new usage meter.
func NewMeter() *Meter {
	return &Meter{
		models: make(map[string]ModelCost),
	}
}

// RegisterModel associates cost data with a model ID.
func (m *Meter) RegisterModel(modelID string, cost ModelCost) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.models[modelID] = cost
}

// Record logs token usage from a completed request.
func (m *Meter) Record(requestID, providerID, model, projectID string,
	promptTokens, completionTokens int, latency time.Duration) {

	m.mu.Lock()
	defer m.mu.Unlock()

	cost := m.calculateCost(model, promptTokens, completionTokens)

	m.records = append(m.records, UsageRecord{
		Timestamp:        time.Now(),
		RequestID:        requestID,
		ProviderID:       providerID,
		Model:            model,
		ProjectID:        projectID,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		EstimatedCostUSD: cost,
		Latency:          latency,
	})
}

// Summary returns aggregated usage statistics for the given time window.
func (m *Meter) Summary(since time.Time) UsageSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()

	summary := UsageSummary{
		ByProvider: make(map[string]*ProviderUsage),
		ByModel:    make(map[string]*ModelUsage),
	}

	var totalLatency time.Duration

	for _, r := range m.records {
		if r.Timestamp.Before(since) {
			continue
		}

		summary.TotalRequests++
		summary.TotalTokens += r.TotalTokens
		summary.PromptTokens += r.PromptTokens
		summary.CompletionTokens += r.CompletionTokens
		summary.TotalCostUSD += r.EstimatedCostUSD
		totalLatency += r.Latency

		// By provider
		if _, ok := summary.ByProvider[r.ProviderID]; !ok {
			summary.ByProvider[r.ProviderID] = &ProviderUsage{}
		}
		pu := summary.ByProvider[r.ProviderID]
		pu.Requests++
		pu.TotalTokens += r.TotalTokens
		pu.TotalCostUSD += r.EstimatedCostUSD

		// By model
		if _, ok := summary.ByModel[r.Model]; !ok {
			summary.ByModel[r.Model] = &ModelUsage{}
		}
		mu := summary.ByModel[r.Model]
		mu.Requests++
		mu.TotalTokens += r.TotalTokens
		mu.TotalCostUSD += r.EstimatedCostUSD
	}

	if summary.TotalRequests > 0 {
		summary.AvgTokensPerReq = float64(summary.TotalTokens) / float64(summary.TotalRequests)
		summary.AvgLatency = float64(totalLatency.Milliseconds()) / float64(summary.TotalRequests)
	}

	return summary
}

// ProjectUsage returns total tokens used by a project within a time window.
func (m *Meter) ProjectUsage(projectID string, since time.Time) (totalTokens int, costUSD float64) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, r := range m.records {
		if r.ProjectID == projectID && !r.Timestamp.Before(since) {
			totalTokens += r.TotalTokens
			costUSD += r.EstimatedCostUSD
		}
	}
	return
}

func (m *Meter) calculateCost(model string, promptTokens, completionTokens int) float64 {
	mc, ok := m.models[model]
	if !ok {
		return 0
	}
	promptCost := (float64(promptTokens) / 1000.0) * mc.CostPer1KPromptTokens
	completionCost := (float64(completionTokens) / 1000.0) * mc.CostPer1KCompletionTokens
	return promptCost + completionCost
}

// ---- Rate Limiter (Token Bucket) ----

// Limiter implements a token-bucket rate limiter.
type Limiter struct {
	mu         sync.Mutex
	rate       float64 // tokens per second
	burst      int     // max bucket size
	tokens     float64
	lastRefill time.Time
}

// NewLimiter creates a token-bucket rate limiter.
// rate is tokens permitted per second. burst is the max burst size.
func NewLimiter(rate float64, burst int) *Limiter {
	return &Limiter{
		rate:       rate,
		burst:      burst,
		tokens:     float64(burst),
		lastRefill: time.Now(),
	}
}

// Allow reports whether n tokens can be consumed.
func (l *Limiter) Allow(n int) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.refill()

	if float64(n) > l.tokens {
		return fmt.Errorf("rate limit exceeded: requested %d tokens, available %.0f", n, l.tokens)
	}

	l.tokens -= float64(n)
	return nil
}

// refill adds tokens based on elapsed time. Caller must hold l.mu.
func (l *Limiter) refill() {
	now := time.Now()
	elapsed := now.Sub(l.lastRefill)
	l.lastRefill = now

	l.tokens += l.rate * elapsed.Seconds()
	if l.tokens > float64(l.burst) {
		l.tokens = float64(l.burst)
	}
}

// ---- Budget Alert ----

// Alert represents a budget threshold notification.
type Alert struct {
	Type       string    `json:"type"` // "warning", "critical", "exceeded"
	ProjectID  string    `json:"project_id"`
	Message    string    `json:"message"`
	CurrentUSD float64   `json:"current_usd"`
	LimitUSD   float64   `json:"limit_usd"`
	Percentage float64   `json:"percentage"`
	Timestamp  time.Time `json:"timestamp"`
}

// BudgetChecker monitors spending against configured limits.
type BudgetChecker struct {
	mu     sync.Mutex
	limits map[string]BudgetLimit // projectID -> limit
	meter  *Meter
	alerts []Alert
}

// BudgetLimit defines spending caps.
type BudgetLimit struct {
	MaxCostUSD float64
	MaxTokens  int
	Window     time.Duration
}

// NewBudgetChecker creates a budget monitor.
func NewBudgetChecker(meter *Meter) *BudgetChecker {
	return &BudgetChecker{
		limits: make(map[string]BudgetLimit),
		meter:  meter,
	}
}

// SetLimit configures a budget limit for a project.
func (bc *BudgetChecker) SetLimit(projectID string, limit BudgetLimit) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.limits[projectID] = limit
}

// Check evaluates current spending against limits. Returns any new alerts.
func (bc *BudgetChecker) Check(projectID string) []Alert {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	limit, ok := bc.limits[projectID]
	if !ok {
		return nil
	}

	since := time.Now().Add(-limit.Window)
	_, costUSD := bc.meter.ProjectUsage(projectID, since)

	var alerts []Alert
	pct := (costUSD / limit.MaxCostUSD) * 100

	if pct >= 100 {
		alerts = append(alerts, Alert{
			Type: "exceeded", ProjectID: projectID,
			Message:    fmt.Sprintf("Budget exceeded: $%.4f / $%.4f (%.0f%%)", costUSD, limit.MaxCostUSD, pct),
			CurrentUSD: costUSD, LimitUSD: limit.MaxCostUSD, Percentage: pct,
			Timestamp: time.Now(),
		})
	} else if pct >= 90 {
		alerts = append(alerts, Alert{
			Type: "critical", ProjectID: projectID,
			Message:    fmt.Sprintf("Budget critical: $%.4f / $%.4f (%.0f%%)", costUSD, limit.MaxCostUSD, pct),
			CurrentUSD: costUSD, LimitUSD: limit.MaxCostUSD, Percentage: pct,
			Timestamp: time.Now(),
		})
	} else if pct >= 75 {
		alerts = append(alerts, Alert{
			Type: "warning", ProjectID: projectID,
			Message:    fmt.Sprintf("Budget warning: $%.4f / $%.4f (%.0f%%)", costUSD, limit.MaxCostUSD, pct),
			CurrentUSD: costUSD, LimitUSD: limit.MaxCostUSD, Percentage: pct,
			Timestamp: time.Now(),
		})
	}

	bc.alerts = append(bc.alerts, alerts...)
	return alerts
}

// RecentAlerts returns the most recent alerts.
func (bc *BudgetChecker) RecentAlerts(n int) []Alert {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if n >= len(bc.alerts) {
		return bc.alerts
	}
	return bc.alerts[len(bc.alerts)-n:]
}
