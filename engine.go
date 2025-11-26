package snake

import (
	"fmt"
	"time"
)

// ErrorStrategy defines how the engine handles task failures
type ErrorStrategy int

const (
	// FailFast stops execution immediately when any task fails
	FailFast ErrorStrategy = iota
)

// Options holds configuration for the Engine
type Options struct {
	ErrorStrategy      ErrorStrategy
	GlobalTimeout      time.Duration
	DefaultTaskTimeout time.Duration
	Logger             Logger
}

// Option is a function that configures an Engine
type Option func(*Options)

// WithFailFast sets the error handling strategy to Fail-Fast mode
func WithFailFast() Option {
	return func(o *Options) {
		o.ErrorStrategy = FailFast
	}
}

// WithGlobalTimeout sets the global execution timeout duration
func WithGlobalTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.GlobalTimeout = timeout
	}
}

// WithDefaultTaskTimeout sets the default timeout for tasks that don't specify their own
func WithDefaultTaskTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.DefaultTaskTimeout = timeout
	}
}

// WithLogger injects a custom Logger implementation
func WithLogger(logger Logger) Option {
	return func(o *Options) {
		o.Logger = logger
	}
}

// Engine is the core orchestration component
type Engine struct {
	tasks       map[string]*Task
	middlewares []HandlerFunc
	store       Datastore
	logger      Logger
	opts        Options

	// DAG structure
	adj      map[string][]string // adjacency list
	indegree map[string]int      // incoming edge count
}

// NewEngine creates a new Engine with the provided options
func NewEngine(opts ...Option) *Engine {
	// Initialize with default options
	options := Options{
		ErrorStrategy: FailFast,
	}

	// Apply all provided options
	for _, opt := range opts {
		opt(&options)
	}

	return &Engine{
		tasks:       make(map[string]*Task),
		middlewares: make([]HandlerFunc, 0),
		opts:        options,
		logger:      options.Logger,
		adj:         make(map[string][]string),
		indegree:    make(map[string]int),
	}
}

// Use registers global middleware functions that apply to all tasks
func (e *Engine) Use(middleware ...HandlerFunc) {
	e.middlewares = append(e.middlewares, middleware...)
}

// Register adds a task to the engine with validation
// Returns an error if:
// - The task ID is empty
// - The task ID is already registered
// - The task handler is nil
func (e *Engine) Register(task *Task) error {
	// Validate non-empty ID
	if task.ID == "" {
		return ErrEmptyTaskID
	}

	// Validate non-nil handler
	if task.Handler == nil {
		return ErrEmptyTaskHandler
	}

	// Validate unique ID
	if _, exists := e.tasks[task.ID]; exists {
		return ErrTaskAlreadyExists
	}

	// Store the task
	e.tasks[task.ID] = task
	return nil
}

// Build constructs the DAG structure from registered tasks
// It builds the adjacency list and calculates indegree for each task
func (e *Engine) Build() error {
	// Reset DAG structures
	e.adj = make(map[string][]string)
	e.indegree = make(map[string]int)

	// Initialize indegree for all tasks to 0
	for taskID := range e.tasks {
		e.indegree[taskID] = 0
	}

	// Build adjacency list and calculate indegree
	for taskID, task := range e.tasks {
		// For each dependency of this task
		for _, depID := range task.DependsOn {
			// Add edge from dependency to this task in adjacency list
			e.adj[depID] = append(e.adj[depID], taskID)

			// Increment indegree for this task
			e.indegree[taskID]++
		}
	}

	return nil
}

// Validate checks the DAG for cycles and missing dependencies
// It uses Kahn's algorithm for cycle detection via topological sort
// Returns an error if:
// - Any task depends on a non-existent task
// - The graph contains a cycle
func (e *Engine) Validate() error {
	// First, validate that all dependencies exist
	for taskID, task := range e.tasks {
		for _, depID := range task.DependsOn {
			if _, exists := e.tasks[depID]; !exists {
				return fmt.Errorf("task %s depends on non-existent task", taskID)
			}
		}
	}

	// Perform topological sort using Kahn's algorithm to detect cycles
	// Create a copy of indegree map to avoid modifying the original
	indegreeCopy := make(map[string]int)
	for taskID, count := range e.indegree {
		indegreeCopy[taskID] = count
	}

	// Queue for tasks with zero indegree
	queue := make([]string, 0)
	for taskID, count := range indegreeCopy {
		if count == 0 {
			queue = append(queue, taskID)
		}
	}

	// Process tasks in topological order
	processed := 0
	for len(queue) > 0 {
		// Dequeue a task
		current := queue[0]
		queue = queue[1:]
		processed++

		// For each dependent of the current task
		for _, dependent := range e.adj[current] {
			// Decrement the indegree
			indegreeCopy[dependent]--

			// If indegree becomes zero, add to queue
			if indegreeCopy[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// If we didn't process all tasks, there's a cycle
	if processed != len(e.tasks) {
		// Collect tasks involved in the cycle
		cycleTaskIDs := make([]string, 0)
		for taskID, count := range indegreeCopy {
			if count > 0 {
				cycleTaskIDs = append(cycleTaskIDs, taskID)
			}
		}

		return fmt.Errorf("cyclic dependency detected involving tasks: %v", cycleTaskIDs)
	}

	return nil
}
