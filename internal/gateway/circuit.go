// Package gateway implements the Floe HTTP gateway — the core routing,
// circuit breaking, and middleware pipeline that proxies requests to LLM providers.
package gateway

import (
	"sync"
	"time"
)

// CircuitState represents the current state of a circuit breaker.
type CircuitState int

const (
	// StateClosed allows all requests through (normal operation).
	StateClosed CircuitState = iota
	// StateOpen blocks all requests (provider assumed down).
	StateOpen
	// StateHalfOpen allows a limited number of probe requests.
	StateHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements the three-state circuit breaker pattern
// for a single LLM provider. It is safe for concurrent use.
type CircuitBreaker struct {
	mu sync.RWMutex

	state            CircuitState
	failureCount     int
	successCount     int
	failureThreshold int
	successThreshold int
	recoveryTimeout  time.Duration

	lastFailureTime time.Time
	lastStateChange time.Time

	// Metrics
	totalRequests  int64
	totalFailures  int64
	totalSuccesses int64
}

// CircuitBreakerConfig holds the parameters for circuit breaker initialization.
type CircuitBreakerConfig struct {
	FailureThreshold int
	SuccessThreshold int
	RecoveryTimeout  time.Duration
}

// NewCircuitBreaker creates a circuit breaker with the given configuration.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 3
	}
	if cfg.RecoveryTimeout <= 0 {
		cfg.RecoveryTimeout = 30 * time.Second
	}

	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: cfg.FailureThreshold,
		successThreshold: cfg.SuccessThreshold,
		recoveryTimeout:  cfg.RecoveryTimeout,
		lastStateChange:  time.Now(),
	}
}

// Allow reports whether a request should be allowed through.
// Returns true if the circuit is closed or half-open (probe request).
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true

	case StateOpen:
		// Check if recovery timeout has elapsed
		if time.Since(cb.lastFailureTime) >= cb.recoveryTimeout {
			cb.transitionTo(StateHalfOpen)
			return true
		}
		return false

	case StateHalfOpen:
		// Allow probe requests in half-open state
		return true

	default:
		return false
	}
}

// RecordSuccess records a successful request.
// In half-open state, consecutive successes above threshold close the circuit.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalRequests++
	cb.totalSuccesses++

	switch cb.state {
	case StateClosed:
		// Reset failure count on success in closed state
		cb.failureCount = 0

	case StateHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.successThreshold {
			cb.transitionTo(StateClosed)
		}
	}
}

// RecordFailure records a failed request.
// In closed state, consecutive failures above threshold open the circuit.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalRequests++
	cb.totalFailures++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case StateClosed:
		cb.failureCount++
		if cb.failureCount >= cb.failureThreshold {
			cb.transitionTo(StateOpen)
		}

	case StateHalfOpen:
		// Any failure in half-open state immediately re-opens
		cb.transitionTo(StateOpen)
	}
}

// State returns the current circuit breaker state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Stats returns metrics about the circuit breaker.
func (cb *CircuitBreaker) Stats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	var errorRate float64
	if cb.totalRequests > 0 {
		errorRate = float64(cb.totalFailures) / float64(cb.totalRequests)
	}

	return CircuitBreakerStats{
		State:          cb.state.String(),
		TotalRequests:  cb.totalRequests,
		TotalFailures:  cb.totalFailures,
		TotalSuccesses: cb.totalSuccesses,
		ErrorRate:      errorRate,
		FailureCount:   cb.failureCount,
		SuccessCount:   cb.successCount,
		LastFailure:    cb.lastFailureTime,
		LastStateChange: cb.lastStateChange,
	}
}

// transitionTo changes the circuit breaker state. Caller must hold cb.mu lock.
func (cb *CircuitBreaker) transitionTo(newState CircuitState) {
	cb.state = newState
	cb.lastStateChange = time.Now()
	cb.failureCount = 0
	cb.successCount = 0
}

// CircuitBreakerStats holds observability metrics.
type CircuitBreakerStats struct {
	State           string    `json:"state"`
	TotalRequests   int64     `json:"total_requests"`
	TotalFailures   int64     `json:"total_failures"`
	TotalSuccesses  int64     `json:"total_successes"`
	ErrorRate       float64   `json:"error_rate"`
	FailureCount    int       `json:"failure_count"`
	SuccessCount    int       `json:"success_count"`
	LastFailure     time.Time `json:"last_failure"`
	LastStateChange time.Time `json:"last_state_change"`
}
