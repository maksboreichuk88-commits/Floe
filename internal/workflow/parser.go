package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/floe-dev/floe/internal/config"
	"gopkg.in/yaml.v3"
)

// Workflow defines a DAG (Directed Acyclic Graph) of executable steps.
type Workflow struct {
	ID          string            `yaml:"id"`
	Description string            `yaml:"description"`
	Version     string            `yaml:"version"`
	Steps       map[string]*Step  `yaml:"steps"`
	Timeout     time.Duration     `yaml:"timeout,omitempty"`
}

// Step represents a single node in the workflow graph.
type Step struct {
	Type     string                 `yaml:"type"`               // "llm", "transform", "condition"
	Depends  []string               `yaml:"depends,omitempty"`  // Step IDs that must complete first
	Input    map[string]interface{} `yaml:"input,omitempty"`    // Dynamic input expressions
	Provider string                 `yaml:"provider,omitempty"` // For "llm" type
	Model    string                 `yaml:"model,omitempty"`    // For "llm" type
	Retry    *RetryConfig           `yaml:"retry,omitempty"`

	// Runtime state
	id    string
	state StepState
	err   error
	out   interface{}
}

// RetryConfig configures retry behavior for a step.
type RetryConfig struct {
	MaxAttempts int           `yaml:"max_attempts"`
	Delay       time.Duration `yaml:"delay"`
}

// StepState tracks the execution status of a step.
type StepState string

const (
	StatePending   StepState = "pending"
	StateRunning   StepState = "running"
	StateCompleted StepState = "completed"
	StateFailed    StepState = "failed"
	StateSkipped   StepState = "skipped"
)

// Registry holds loaded workflows.
type Registry struct {
	workflows map[string]*Workflow
	cfg       config.WorkflowConfig
}

// NewRegistry creates a new workflow registry.
func NewRegistry(cfg config.WorkflowConfig) *Registry {
	return &Registry{
		workflows: make(map[string]*Workflow),
		cfg:       cfg,
	}
}

// LoadAll reads and parses all .yaml files in the configured directory.
func (r *Registry) LoadAll() error {
	if !r.cfg.Enabled {
		return nil
	}

	dir := r.cfg.Dir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating workflow directory: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading workflow directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" && filepath.Ext(entry.Name()) != ".yml" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		wf, err := r.parseFile(path)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}

		if err := r.validate(wf); err != nil {
			return fmt.Errorf("validating %s: %w", path, err)
		}

		r.workflows[wf.ID] = wf
	}

	return nil
}

// Get finds a workflow by ID.
func (r *Registry) Get(id string) (*Workflow, bool) {
	wf, ok := r.workflows[id]
	if !ok {
		return nil, false
	}
	// Return a deep copy to prevent mutation during concurrent executions
	copy := deepCopyWorkflow(wf)
	return copy, true
}

func (r *Registry) parseFile(path string) (*Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var wf Workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, err
	}

	for id, step := range wf.Steps {
		step.id = id
		step.state = StatePending
	}

	return &wf, nil
}

func (r *Registry) validate(wf *Workflow) error {
	if wf.ID == "" {
		return fmt.Errorf("workflow missing ID")
	}
	if len(wf.Steps) == 0 {
		return fmt.Errorf("workflow has no steps")
	}

	// Validate dependencies and check for cycles
	visited := make(map[string]bool)
	tempMark := make(map[string]bool)
	var sortOrder []string

	var visit func(string) error
	visit = func(stepID string) error {
		if tempMark[stepID] {
			return fmt.Errorf("cycle detected involving step %q", stepID)
		}
		if visited[stepID] {
			return nil
		}

		step, ok := wf.Steps[stepID]
		if !ok {
			return fmt.Errorf("reference to undefined step %q", stepID)
		}

		tempMark[stepID] = true
		for _, dep := range step.Depends {
			if err := visit(dep); err != nil {
				return err
			}
		}
		tempMark[stepID] = false
		visited[stepID] = true
		sortOrder = append(sortOrder, stepID)
		return nil
	}

	for id := range wf.Steps {
		if !visited[id] {
			if err := visit(id); err != nil {
				return err
			}
		}
	}

	return nil
}

// deepCopyWorkflow creates a new instance for execution.
func deepCopyWorkflow(orig *Workflow) *Workflow {
	data, _ := yaml.Marshal(orig)
	var copy Workflow
	yaml.Unmarshal(data, &copy)
	for id, step := range copy.Steps {
		step.id = id
		step.state = StatePending
	}
	return &copy
}

// ExecutionContext holds state during a workflow run.
type ExecutionContext struct {
	context.Context
	cancel    context.CancelFunc
	Workflow  *Workflow
	Vars      map[string]interface{} // Global variables available to all steps
	StepState map[string]interface{} // Output of completed steps
}
