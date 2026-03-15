package workflow

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/floe-dev/floe/internal/config"
	"github.com/floe-dev/floe/internal/gateway"
	"github.com/floe-dev/floe/internal/provider"
)

// Executor runs workflows, enforcing resource limits and sandbox rules.
type Executor struct {
	router *gateway.Router
	cfg    config.WorkflowConfig
}

// NewExecutor creates a new workflow engine executor.
func NewExecutor(router *gateway.Router, cfg config.WorkflowConfig) *Executor {
	return &Executor{
		router: router,
		cfg:    cfg,
	}
}

// Run executes a parsed workflow DAG to completion or failure.
func (e *Executor) Run(ctx context.Context, wf *Workflow, initialVars map[string]interface{}) (map[string]interface{}, error) {
	timeout := e.cfg.MaxDuration
	if wf.Timeout > 0 && wf.Timeout < timeout {
		timeout = wf.Timeout
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	runCtx := &ExecutionContext{
		Context:   execCtx,
		cancel:    cancel,
		Workflow:  wf,
		Vars:      initialVars,
		StepState: make(map[string]interface{}),
	}
	if runCtx.Vars == nil {
		runCtx.Vars = make(map[string]interface{})
	}

	// Channel for completed steps
	done := make(chan string, len(wf.Steps))
	errc := make(chan error, 1)

	var mu sync.Mutex
	running := 0

	// Initial trigger for steps with no dependencies
	e.triggerReadySteps(runCtx, done, errc, &mu, &running)

	completedCount := 0
	totalSteps := len(wf.Steps)

	for completedCount < totalSteps {
		select {
		case <-execCtx.Done():
			return nil, fmt.Errorf("workflow execution aborted: %w", execCtx.Err())
		case err := <-errc:
			cancel() // Halt all other steps on first failure
			return nil, err
		case stepID := <-done:
			completedCount++
			e.triggerReadySteps(runCtx, done, errc, &mu, &running)
		}
	}

	return runCtx.StepState, nil
}

func (e *Executor) triggerReadySteps(ctx *ExecutionContext, done chan<- string, errc chan<- error, mu *sync.Mutex, running *int) {
	mu.Lock()
	defer mu.Unlock()

	for id, step := range ctx.Workflow.Steps {
		if step.state != StatePending {
			continue
		}

		// Check if all dependencies are satisfied
		ready := true
		for _, depID := range step.Depends {
			if ctx.Workflow.Steps[depID].state != StateCompleted {
				ready = false
				break
			}
		}

		if ready {
			step.state = StateRunning
			*running++
			go e.executeStep(ctx, step, done, errc, mu, running)
		}
	}
}

func (e *Executor) executeStep(ctx *ExecutionContext, step *Step, done chan<- string, errc chan<- error, mu *sync.Mutex, running *int) {
	defer func() {
		mu.Lock()
		*running--
		mu.Unlock()
	}()

	var out interface{}
	var err error

	attempts := 1
	delay := 1 * time.Second
	if step.Retry != nil {
		attempts = step.Retry.MaxAttempts
		delay = step.Retry.Delay
	}

	for i := 0; i < attempts; i++ {
		out, err = e.runHandler(ctx, step)
		if err == nil {
			break // Success
		}
		if i < attempts-1 {
			select {
			case <-ctx.Done():
				err = ctx.Err()
				goto FAIL
			case <-time.After(delay):
			}
		}
	}

FAIL:
	mu.Lock()
	defer mu.Unlock()

	if err != nil {
		step.state = StateFailed
		step.err = err
		errc <- fmt.Errorf("step %q failed: %w", step.id, err)
		return
	}

	step.state = StateCompleted
	step.out = out
	ctx.StepState[step.id] = out

	// Send to done channel inside a non-blocking select or separate goroutine
	// to avoid blocking while holding the mutex.
	go func() {
		select {
		case done <- step.id:
		case <-ctx.Done():
		}
	}()
}

func (e *Executor) runHandler(ctx *ExecutionContext, step *Step) (interface{}, error) {
	// Evaluate template expressions in inputs
	inputs, err := EvaluateMap(step.Input, ctx)
	if err != nil {
		return nil, fmt.Errorf("evaluating inputs: %w", err)
	}

	switch step.Type {
	case "llm":
		return e.handleLLM(ctx, step, inputs)
	case "transform":
		return handleTransform(inputs)
	case "condition":
		return handleCondition(inputs)
	default:
		return nil, fmt.Errorf("unknown step type %q", step.Type)
	}
}

func (e *Executor) handleLLM(ctx *ExecutionContext, step *Step, inputs map[string]interface{}) (interface{}, error) {
	promptInter, ok := inputs["prompt"]
	if !ok {
		return nil, fmt.Errorf("llm step requires 'prompt' input")
	}
	prompt, ok := promptInter.(string)
	if !ok {
		return nil, fmt.Errorf("'prompt' must be a string")
	}

	req := &provider.ChatRequest{
		Model: step.Model,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: prompt},
		},
		Metadata: provider.RequestMeta{
			RequestID: fmt.Sprintf("wf-%s-%s", ctx.Workflow.ID, step.id),
			StartTime: time.Now(),
		},
	}

	// Try to route using workflow's specified provider constraints if any.
	// We rely on the router to apply fallback/circuit breakers.

	resp, err := e.router.Route(ctx.Context, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"content": resp.Content,
		"usage":   resp.Usage,
		"latency": resp.Latency.Milliseconds(),
	}, nil
}
