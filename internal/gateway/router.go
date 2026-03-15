package gateway

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/floe-dev/floe/internal/config"
	"github.com/floe-dev/floe/internal/provider"
)

// Router distributes chat requests across registered LLM providers
// based on the configured routing strategy and circuit breaker state.
type Router struct {
	mu sync.RWMutex

	providers map[string]*providerEntry
	order     []string // Provider IDs in priority order
	strategy  RoutingStrategy

	// Round-robin state
	rrIndex atomic.Uint64

	retryAttempts int
	retryDelay    time.Duration
	fallbackOrder []string
}

// providerEntry wraps a Provider with its circuit breaker and metrics.
type providerEntry struct {
	provider provider.Provider
	circuit  *CircuitBreaker
	config   config.ProviderConf
	latency  *ewmaLatency
}

// RoutingStrategy defines how requests are distributed.
type RoutingStrategy string

const (
	StrategyPriority     RoutingStrategy = "priority"
	StrategyRoundRobin   RoutingStrategy = "round-robin"
	StrategyLeastLatency RoutingStrategy = "least-latency"
	StrategyCostOptimized RoutingStrategy = "cost-optimized"
)

// NewRouter creates a Router from the given configuration.
func NewRouter(cfg config.RoutingConfig, cbCfg config.CircuitBreakerConfig) *Router {
	strategy := RoutingStrategy(cfg.Strategy)
	if strategy == "" {
		strategy = StrategyPriority
	}

	retryAttempts := cfg.RetryAttempts
	if retryAttempts <= 0 {
		retryAttempts = 2
	}

	retryDelay := cfg.RetryDelay
	if retryDelay <= 0 {
		retryDelay = 500 * time.Millisecond
	}

	return &Router{
		providers:     make(map[string]*providerEntry),
		strategy:      strategy,
		retryAttempts: retryAttempts,
		retryDelay:    retryDelay,
		fallbackOrder: cfg.FallbackOrder,
	}
}

// Register adds a provider to the router.
func (r *Router) Register(p provider.Provider, cfg config.ProviderConf, cbCfg CircuitBreakerConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := &providerEntry{
		provider: p,
		circuit:  NewCircuitBreaker(cbCfg),
		config:   cfg,
		latency:  newEWMALatency(0.3),
	}

	r.providers[p.ID()] = entry
	r.order = append(r.order, p.ID())

	// Sort by priority (lower = higher priority)
	sort.Slice(r.order, func(i, j int) bool {
		pi := r.providers[r.order[i]]
		pj := r.providers[r.order[j]]
		return pi.config.Priority < pj.config.Priority
	})
}

// Route selects the best available provider and sends the request.
// It handles circuit breaking, retries, and failover transparently.
func (r *Router) Route(ctx context.Context, req *provider.ChatRequest) (*provider.ChatResponse, error) {
	r.mu.RLock()
	candidates := r.selectCandidates(req)
	r.mu.RUnlock()

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no available providers for model %q (all circuits open)", req.Model)
	}

	var lastErr error
	attempted := make(map[string]bool)

	for attempt := 0; attempt <= r.retryAttempts; attempt++ {
		for _, entry := range candidates {
			if attempted[entry.provider.ID()] {
				continue
			}

			if !entry.circuit.Allow() {
				continue
			}

			start := time.Now()
			req.Metadata.ProviderID = entry.provider.ID()
			req.Metadata.StartTime = start

			resp, err := entry.provider.Chat(ctx, req)
			elapsed := time.Since(start)

			if err != nil {
				entry.circuit.RecordFailure()
				entry.latency.add(elapsed)
				lastErr = fmt.Errorf("provider %s: %w", entry.provider.ID(), err)
				attempted[entry.provider.ID()] = true
				continue
			}

			entry.circuit.RecordSuccess()
			entry.latency.add(elapsed)
			resp.Latency = elapsed
			resp.Provider = entry.provider.ID()
			return resp, nil
		}

		// Wait before retrying with next candidate set
		if attempt < r.retryAttempts {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(r.retryDelay):
			}
			// Re-evaluate candidates (circuits may have changed)
			r.mu.RLock()
			candidates = r.selectCandidates(req)
			r.mu.RUnlock()
			attempted = make(map[string]bool) // Reset for retry round
		}
	}

	return nil, fmt.Errorf("all providers exhausted after %d attempts: %w", r.retryAttempts+1, lastErr)
}

// StreamRoute selects a provider and returns a streaming response channel.
func (r *Router) StreamRoute(ctx context.Context, req *provider.ChatRequest) (<-chan provider.StreamChunk, error) {
	r.mu.RLock()
	candidates := r.selectCandidates(req)
	r.mu.RUnlock()

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no available providers for model %q", req.Model)
	}

	for _, entry := range candidates {
		if !entry.circuit.Allow() {
			continue
		}

		req.Metadata.ProviderID = entry.provider.ID()
		req.Metadata.StartTime = time.Now()

		ch, err := entry.provider.StreamChat(ctx, req)
		if err != nil {
			entry.circuit.RecordFailure()
			continue
		}

		// Wrap channel to record circuit breaker outcomes
		wrapped := make(chan provider.StreamChunk, 32)
		go func() {
			defer close(wrapped)
			var hadError bool
			for chunk := range ch {
				if chunk.Err != nil {
					hadError = true
				}
				wrapped <- chunk
			}
			if hadError {
				entry.circuit.RecordFailure()
			} else {
				entry.circuit.RecordSuccess()
			}
		}()

		return wrapped, nil
	}

	return nil, fmt.Errorf("no healthy providers available for model %q", req.Model)
}

// selectCandidates returns providers ordered by the current routing strategy.
// Caller must hold r.mu read lock.
func (r *Router) selectCandidates(req *provider.ChatRequest) []*providerEntry {
	var candidates []*providerEntry

	for _, id := range r.order {
		entry := r.providers[id]
		// Filter by model if specified
		if req.Model != "" && len(entry.config.Models) > 0 {
			found := false
			for _, m := range entry.config.Models {
				if m == req.Model {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		candidates = append(candidates, entry)
	}

	// Apply routing strategy ordering
	switch r.strategy {
	case StrategyRoundRobin:
		idx := int(r.rrIndex.Add(1)) % len(candidates)
		// Rotate candidates so the next one is first
		rotated := make([]*providerEntry, len(candidates))
		for i := range candidates {
			rotated[i] = candidates[(idx+i)%len(candidates)]
		}
		candidates = rotated

	case StrategyLeastLatency:
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].latency.value() < candidates[j].latency.value()
		})

	case StrategyCostOptimized:
		sort.Slice(candidates, func(i, j int) bool {
			ci := lowestCost(candidates[i])
			cj := lowestCost(candidates[j])
			return ci < cj
		})

	case StrategyPriority:
		// Already sorted by priority from Register()
	}

	// Apply fallback order override if configured
	if len(r.fallbackOrder) > 0 {
		ordered := make([]*providerEntry, 0, len(candidates))
		added := make(map[string]bool)
		for _, id := range r.fallbackOrder {
			for _, c := range candidates {
				if c.provider.ID() == id && !added[id] {
					ordered = append(ordered, c)
					added[id] = true
					break
				}
			}
		}
		// Append any remaining not in fallback list
		for _, c := range candidates {
			if !added[c.provider.ID()] {
				ordered = append(ordered, c)
			}
		}
		candidates = ordered
	}

	return candidates
}

// ProviderStatuses returns the current status of all registered providers.
func (r *Router) ProviderStatuses() []provider.ProviderStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	statuses := make([]provider.ProviderStatus, 0, len(r.providers))
	for _, id := range r.order {
		entry := r.providers[id]
		stats := entry.circuit.Stats()
		statuses = append(statuses, provider.ProviderStatus{
			ID:           entry.provider.ID(),
			Name:         entry.provider.Name(),
			Healthy:      entry.circuit.State() != StateOpen,
			CircuitState: stats.State,
			AvgLatency:   time.Duration(entry.latency.value()),
			ErrorRate:    stats.ErrorRate,
			RequestCount: stats.TotalRequests,
			LastError:    "",
			LastCheckedAt: stats.LastStateChange,
		})
	}
	return statuses
}

// lowestCost returns the minimum prompt token cost across a provider's models.
func lowestCost(entry *providerEntry) float64 {
	models := entry.provider.Models()
	if len(models) == 0 {
		return 0
	}
	min := math.MaxFloat64
	for _, m := range models {
		if m.CostPer1KPromptTokens < min {
			min = m.CostPer1KPromptTokens
		}
	}
	return min
}

// ewmaLatency tracks an exponentially weighted moving average of latency.
type ewmaLatency struct {
	mu    sync.Mutex
	alpha float64
	avg   float64
	init  bool
}

func newEWMALatency(alpha float64) *ewmaLatency {
	return &ewmaLatency{alpha: alpha}
}

func (e *ewmaLatency) add(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	v := float64(d)
	if !e.init {
		e.avg = v
		e.init = true
		return
	}
	e.avg = e.alpha*v + (1-e.alpha)*e.avg
}

func (e *ewmaLatency) value() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.avg
}
