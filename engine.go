package snake

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ErrorStrategy defines how the engine handles task failures
type ErrorStrategy int

const (
	// FailFast stops execution immediately when any task fails
	FailFast ErrorStrategy = iota
)

// Option is a function that configures an Engine
type Option func(*Engine)

// WithFailFast sets the error handling strategy to Fail-Fast mode
func WithFailFast() Option {
	return func(e *Engine) {
		e.ErrorStrategy = FailFast
	}
}

func WithStore(store Datastore) Option {
	return func(e *Engine) {
		e.store = store
	}
}

// WithGlobalTimeout sets the global execution timeout duration
func WithGlobalTimeout(timeout time.Duration) Option {
	return func(e *Engine) {
		e.GlobalTimeout = timeout
	}
}

// WithDefaultTaskTimeout sets the default timeout for tasks that don't specify their own
func WithDefaultTaskTimeout(timeout time.Duration) Option {
	return func(e *Engine) {
		e.DefaultTaskTimeout = timeout
	}
}

// WithLogger injects a custom Logger implementation
func WithLogger(logger Logger) Option {
	return func(e *Engine) {
		e.logger = logger
	}
}

// Engine is the core orchestration component
type Engine struct {
	tasks       map[string]*Task
	middlewares []HandlerFunc
	store       Datastore
	logger      Logger

	// DAG structure
	adj      map[string][]string // adjacency list
	indegree map[string]int      // incoming edge count

	// options
	ErrorStrategy      ErrorStrategy
	GlobalTimeout      time.Duration
	DefaultTaskTimeout time.Duration
}

// NewEngine creates a new Engine with the provided options
func NewEngine(opts ...Option) *Engine {
	// Initialize with default options
	e := &Engine{
		tasks:         make(map[string]*Task),
		middlewares:   make([]HandlerFunc, 0),
		store:         newMemoryStore(),
		logger:        newDefaultLogger(),
		adj:           make(map[string][]string),
		indegree:      make(map[string]int),
		ErrorStrategy: FailFast,
	}

	// Apply all provided options
	for _, opt := range opts {
		opt(e)
	}

	return e
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

// executionState holds the runtime state for a single DAG execution
type executionState struct {
	executionID string
	store       Datastore
	pendingDeps map[string]*atomic.Int32
	readyQueue  chan string
}

// Execute runs the DAG with the provided context
// It initializes execution state and schedules tasks for parallel execution
func (e *Engine) Execute(ctx context.Context) (*ExecutionResult, error) {
	// Generate unique ExecutionID using timestamp-based approach
	executionID := fmt.Sprintf("exec-%d", time.Now().UnixNano())

	// Create new Datastore instance for this execution
	store := newMemoryStore()

	// Initialize pendingDeps map with atomic.Int32 for each task
	pendingDeps := make(map[string]*atomic.Int32)
	for taskID, indegree := range e.indegree {
		counter := &atomic.Int32{}
		counter.Store(int32(indegree))
		pendingDeps[taskID] = counter
	}

	// Create ready queue channel
	readyQueue := make(chan string, len(e.tasks))

	// Identify and queue all zero-indegree tasks
	for taskID, indegree := range e.indegree {
		if indegree == 0 {
			readyQueue <- taskID
		}
	}

	// Create execution state
	state := &executionState{
		executionID: executionID,
		store:       store,
		pendingDeps: pendingDeps,
		readyQueue:  readyQueue,
	}

	// Execute tasks in parallel
	result := e.executeParallel(ctx, state)

	return result, nil
}

// executeParallel schedules and executes tasks in parallel based on their dependencies
// It spawns a goroutine for each ready task and manages the execution lifecycle
func (e *Engine) executeParallel(ctx context.Context, state *executionState) *ExecutionResult {
	var wg sync.WaitGroup
	reports := make(map[string]*TaskReport)
	var reportsMu sync.Mutex

	// Track the number of tasks that have been scheduled
	tasksScheduled := &atomic.Int32{}
	tasksCompleted := &atomic.Int32{}

	// Count initial zero-indegree tasks
	initialTasks := int32(0)
	for _, indegree := range e.indegree {
		if indegree == 0 {
			initialTasks++
		}
	}
	tasksScheduled.Store(initialTasks)

	// Create cancellable context for Fail-Fast error handling
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Track if execution has failed
	executionFailed := &atomic.Bool{}

	// Track which tasks have been started
	startedTasks := make(map[string]bool)
	var startedMu sync.Mutex

	// Spawn a goroutine to process tasks from the ready queue
	done := make(chan struct{})
	go func() {
		defer close(done)
		for taskID := range state.readyQueue {
			// Check if execution has already failed
			if executionFailed.Load() {
				// Mark this task as SKIPPED since it hasn't started
				reportsMu.Lock()
				reports[taskID] = &TaskReport{
					TaskID:    taskID,
					Status:    TaskStatusSkipped,
					StartTime: time.Now(),
					EndTime:   time.Now(),
					Duration:  0,
				}
				reportsMu.Unlock()

				// Still need to process dependents to allow completion
				for _, dependentID := range e.adj[taskID] {
					newCount := state.pendingDeps[dependentID].Add(-1)
					if newCount == 0 {
						tasksScheduled.Add(1)
						state.readyQueue <- dependentID
					}
				}

				// Mark as completed
				completed := tasksCompleted.Add(1)
				if completed == tasksScheduled.Load() {
					close(state.readyQueue)
				}
				continue
			}

			// Mark task as started
			startedMu.Lock()
			startedTasks[taskID] = true
			startedMu.Unlock()

			// Increment WaitGroup for this task
			wg.Add(1)

			// Spawn goroutine to execute task
			go func(tid string) {
				defer wg.Done()

				// Execute the task
				report := e.executeTask(execCtx, tid, state.executionID, state.store)

				// Store the report
				reportsMu.Lock()
				reports[tid] = report
				reportsMu.Unlock()

				// Check if task failed and trigger Fail-Fast
				if report.Status == TaskStatusFailed && e.ErrorStrategy == FailFast {
					// Mark execution as failed
					executionFailed.Store(true)
					// Cancel execution context to signal all other tasks
					cancel()
				}

				// On task completion, process dependents
				for _, dependentID := range e.adj[tid] {
					// Atomically decrement pendingDeps for this dependent
					newCount := state.pendingDeps[dependentID].Add(-1)

					// If pending count reaches zero, add to ready queue
					if newCount == 0 {
						tasksScheduled.Add(1)
						state.readyQueue <- dependentID
					}
				}

				// Mark this task as completed
				completed := tasksCompleted.Add(1)

				// If all scheduled tasks are completed, close the queue
				if completed == tasksScheduled.Load() {
					close(state.readyQueue)
				}
			}(taskID)
		}
	}()

	// Wait for all tasks to complete
	wg.Wait()

	// Wait for the processing goroutine to finish
	<-done

	// Mark any tasks that were never started as SKIPPED
	reportsMu.Lock()
	for taskID := range e.tasks {
		if _, exists := reports[taskID]; !exists {
			reports[taskID] = &TaskReport{
				TaskID:    taskID,
				Status:    TaskStatusSkipped,
				StartTime: time.Now(),
				EndTime:   time.Now(),
				Duration:  0,
			}
		}
	}
	reportsMu.Unlock()

	// Determine overall success
	success := true
	for _, report := range reports {
		if report.Status != TaskStatusSuccess {
			success = false
			break
		}
	}

	return &ExecutionResult{
		ExecutionID: state.executionID,
		Success:     success,
		Reports:     reports,
		Store:       state.store,
	}
}

// executeTask executes a single task with proper context, timeout, and middleware chain
// It creates the task Context, applies timeouts, builds the handler chain, and records execution results
func (e *Engine) executeTask(execCtx context.Context, taskID string, executionID string, store Datastore) *TaskReport {
	// Get the task
	task := e.tasks[taskID]

	// Record start time
	startTime := time.Now()

	// Initialize task report
	report := &TaskReport{
		TaskID:    taskID,
		Status:    TaskStatusPending,
		StartTime: startTime,
	}

	// Create task context with timeout
	taskCtx := execCtx
	var cancel context.CancelFunc

	// Apply task timeout if configured, otherwise use default timeout
	if task.Timeout > 0 {
		taskCtx, cancel = context.WithTimeout(execCtx, task.Timeout)
	} else if e.DefaultTaskTimeout > 0 {
		taskCtx, cancel = context.WithTimeout(execCtx, e.DefaultTaskTimeout)
	} else {
		// No timeout, but we still need a cancel function for cleanup
		taskCtx, cancel = context.WithCancel(execCtx)
	}
	defer cancel()

	// Create logger with task metadata
	var taskLogger Logger
	if e.logger != nil {
		taskLogger = e.logger.With(
			Field{Key: "TaskID", Value: taskID},
			Field{Key: "ExecutionID", Value: executionID},
		)
	}

	// Build handler chain: global middlewares + task middlewares + handler
	handlers := make([]HandlerFunc, 0, len(e.middlewares)+len(task.Middlewares)+1)
	handlers = append(handlers, e.middlewares...)
	handlers = append(handlers, task.Middlewares...)
	handlers = append(handlers, task.Handler)

	// Create task Context
	ctx := &Context{
		ctx:         taskCtx,
		TaskID:      taskID,
		ExecutionID: executionID,
		StartTime:   startTime,
		Store:       store,
		Logger:      taskLogger,
		handlers:    handlers,
		index:       -1, // Start at -1 so first Next() call advances to 0
	}

	// Execute handler chain starting from index 0
	err := ctx.Next()

	// Record end time and duration
	endTime := time.Now()
	report.EndTime = endTime
	report.Duration = endTime.Sub(startTime)

	// Update status based on execution result
	if err != nil {
		report.Status = TaskStatusFailed
		report.Err = err
	} else {
		report.Status = TaskStatusSuccess
	}

	return report
}
