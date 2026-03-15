// Package web3sec provides specialized constraints for autonomous agents handling
// real economic value, including Human-in-the-loop (HITL) intercepts and hard spending caps.
package web3sec

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Policy defines the hard constraints the autonomous agent CANNOT override.
type Policy struct {
	MaxDailyUSDC         float64       // Agent cannot spend more than this per Rolling 24h
	HITLThresholdUSDC    float64       // Transactions over this amount require explicit human approval
	ApprovalTimeout      time.Duration // How long to wait for human approval before aborting
	AllowedNetworks      []string      // e.g. ["ARC_TESTNET"]
}

// Guard is the singleton interceptor that evaluates all outgoing intents before execution.
type Guard struct {
	mu           sync.RWMutex
	policy       Policy
	spentLast24h float64
	logger       *slog.Logger
	// Mock channel for human approvals (in production, this blocks and waits for Dashboard/CLI input)
	approvalChannel chan struct{} 
}

// NewGuard initializes the Web3 security perimeter.
func NewGuard(policy Policy, logger *slog.Logger) *Guard {
	return &Guard{
		policy:          policy,
		logger:          logger,
		approvalChannel: make(chan struct{}, 1),
	}
}

// EvaluateIntent checks an intended on-chain action against the hardcoded policy.
// It will block if HITL approval is required, or return an error if caps are exceeded.
func (g *Guard) EvaluateIntent(ctx context.Context, intentAmount float64, network string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// 1. Check Network Whitelist
	networkAllowed := false
	for _, n := range g.policy.AllowedNetworks {
		if n == network {
			networkAllowed = true
			break
		}
	}
	if !networkAllowed {
		g.logger.Warn("hit attempt on disallowed network", "network", network)
		return fmt.Errorf("SECURITY ABORT: Autonomous execution on network %s is strictly forbidden by policy", network)
	}

	// 2. Check Hard Spending Caps
	if g.spentLast24h + intentAmount > g.policy.MaxDailyUSDC {
		g.logger.Warn("spending cap exceeded", "proposed", intentAmount, "daily_cap", g.policy.MaxDailyUSDC)
		return fmt.Errorf("SECURITY ABORT: Proposed transaction of %f USDC exceeds the hard daily cap of %f USDC", intentAmount, g.policy.MaxDailyUSDC)
	}

	// 3. Human-in-the-Loop (HITL) check
	if intentAmount >= g.policy.HITLThresholdUSDC {
		g.logger.Info("HITL trigger: Transaction requires human approval", "amount", intentAmount, "threshold", g.policy.HITLThresholdUSDC)
		
		// In a real implementation we would release the lock and wait on a channel for the Dashboard
		// to resolve the request. For this architecture scaffolding, we simulate the wait.
		select {
		case <-g.approvalChannel:
			g.logger.Info("HITL: Human operator approved the transaction.")
		case <-time.After(g.policy.ApprovalTimeout):
			return fmt.Errorf("SECURITY ABORT: Human-in-the-loop approval timed out after %v", g.policy.ApprovalTimeout)
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Proceed
	g.spentLast24h += intentAmount
	g.logger.Info("security guard passed", "amount", intentAmount, "remaining_budget", g.policy.MaxDailyUSDC - g.spentLast24h)
	
	// Start an async reset of the rolling 24h budget (simplified)
	go func(amt float64) {
		time.Sleep(24 * time.Hour)
		g.mu.Lock()
		g.spentLast24h -= amt
		g.mu.Unlock()
	}(intentAmount)

	return nil
}

// ManualApprove is called by the UI/CLI when the human owner clicks "Approve".
func (g *Guard) ManualApprove() {
	select {
	case g.approvalChannel <- struct{}{}:
	default: // non-blocking if no one is waiting
	}
}
