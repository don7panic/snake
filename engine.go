package snake

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
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
		e.errorStrategy = FailFast
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
		e.globalTimeout = timeout
	}
}

// WithDefaultTaskTimeout sets the default timeout for tasks that don't specify their own
func WithDefaultTaskTimeout(timeout time.Duration) Option {
	return func(e *Engine) {
		e.defaultTaskTimeout = timeout
	}
}

// WithLogger injects a custom Logger implementation
func WithLogger(logger Logger) Option {
	return func(e *Engine) {
		e.logger = logger
	}
}

// WithMaxConcurrency limits concurrent task executions. Non-positive means unlimited.
func WithMaxConcurrency(max int) Option {
	return func(e *Engine) {
		e.maxConcurrency = max
	}
}

// Engine is the core orchestration component
type Engine struct {
	tasks          map[string]*Task
	middlewares    []HandlerFunc
	store          Datastore
	logger         Logger
	maxConcurrency int

	// DAG structure
	adj      map[string][]string // adjacency list
	indegree map[string]int      // incoming edge count
	topo     []string            // cached topological order

	// options
	errorStrategy      ErrorStrategy
	globalTimeout      time.Duration
	defaultTaskTimeout time.Duration

	// concurrency control
	mu    sync.RWMutex // protects DAG structures and task registry during Build/Execute
	built bool
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
		errorStrategy: FailFast,
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

// Register adds tasks to the engine with validation
// Returns an error if:
// - The task ID is empty
// - The task ID is already registered
// - The task handler is nil
func (e *Engine) Register(tasks ...*Task) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.built {
		return ErrRegisterAfterBuild
	}

	for _, task := range tasks {
		// Validate non-empty ID
		if task.id == "" {
			return ErrEmptyTaskID
		}

		// Validate non-nil handler
		if task.handler == nil {
			return ErrEmptyTaskHandler
		}

		// Validate unique ID
		if _, exists := e.tasks[task.id]; exists {
			return ErrTaskAlreadyExists
		}

		// Store the task
		e.tasks[task.id] = task
	}
	return nil
}

// Build constructs and validates the DAG. Call this after all tasks are registered.
func (e *Engine) Build() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.buildLocked()
}

// buildLocked constructs and validates the DAG. Caller must hold e.mu.Lock().
func (e *Engine) buildLocked() error {
	// Reset DAG structures
	e.adj = make(map[string][]string)
	e.indegree = make(map[string]int)
	e.topo = nil

	// Initialize indegree for all tasks to 0
	for taskID := range e.tasks {
		e.indegree[taskID] = 0
	}

	// Build adjacency list and calculate indegree
	for taskID, task := range e.tasks {
		// For each dependency of this task
		for _, depID := range task.dependsOn {
			if _, exists := e.tasks[depID]; !exists {
				return fmt.Errorf("%w: %s depends on %s", ErrMissingDependency, taskID, depID)
			}
			e.adj[depID] = append(e.adj[depID], taskID)

			// Increment indegree for this task
			e.indegree[taskID]++
		}
	}

	// Topological sort with cycle detection
	topo, err := e.topologicalSort()
	if err != nil {
		return err
	}
	e.topo = topo
	e.built = true

	return nil
}

// topologicalSort performs a Kahn topological sort and returns the order or an error.
func (e *Engine) topologicalSort() ([]string, error) {
	indegreeCopy := make(map[string]int)
	for taskID, count := range e.indegree {
		indegreeCopy[taskID] = count
	}

	queue := make([]string, 0)
	for taskID, count := range indegreeCopy {
		if count == 0 {
			queue = append(queue, taskID)
		}
	}

	if len(queue) == 0 && len(indegreeCopy) > 0 {
		return nil, fmt.Errorf("%w: no task with zero indegree", ErrCyclicDependency)
	}

	result := make([]string, 0, len(indegreeCopy))

	for len(queue) > 0 {
		// Dequeue a task
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

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
	if len(result) != len(e.tasks) {
		// Collect tasks involved in the cycle
		cycleTaskIDs := make([]string, 0)
		for taskID, count := range indegreeCopy {
			if count > 0 {
				cycleTaskIDs = append(cycleTaskIDs, taskID)
			}
		}
		return nil, fmt.Errorf("%w: %v", ErrCyclicDependency, cycleTaskIDs)
	}

	return result, nil
}

// executionState holds the runtime state for a single DAG execution
type executionState struct {
	executionID string
	pendingDeps map[string]*atomic.Int32
	readyQueue  chan string
	store       Datastore
	taskIDs     []string
}

// Execute runs the DAG with the provided context
// It initializes execution state and schedules tasks for parallel execution
func (e *Engine) Execute(ctx context.Context) (*ExecutionResult, error) {
	// Quick snapshot
	e.mu.RLock()
	hasTasks := len(e.tasks) > 0
	built := e.built
	e.mu.RUnlock()

	if !hasTasks {
		return nil, ErrNoTasksRegistered
	}

	// Lazily build if not already built
	if !built {
		e.mu.Lock()
		if !e.built {
			if err := e.buildLocked(); err != nil {
				e.mu.Unlock()
				return nil, err
			}
		}
		e.mu.Unlock()
	}

	// Apply global timeout if configured
	if e.globalTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.globalTimeout)
		defer cancel()
	}

	// Use a fresh store per execution
	store := e.reset()

	// Snapshot DAG data for safe concurrent Execute calls
	e.mu.RLock()
	tasksCount := len(e.tasks)
	taskIDs := make([]string, 0, tasksCount)
	for id := range e.tasks {
		taskIDs = append(taskIDs, id)
	}
	indegreeSnapshot := make(map[string]int, len(e.indegree))
	for k, v := range e.indegree {
		indegreeSnapshot[k] = v
	}
	adjSnapshot := make(map[string][]string, len(e.adj))
	for k, v := range e.adj {
		neighbors := make([]string, len(v))
		copy(neighbors, v)
		adjSnapshot[k] = neighbors
	}
	topoSnapshot := append([]string(nil), e.topo...)
	e.mu.RUnlock()

	// Generate unique ExecutionID using UUID
	executionID := uuid.New().String()

	// Initialize pendingDeps map with atomic.Int32 for each task
	pendingDeps := make(map[string]*atomic.Int32)
	for taskID, indegree := range indegreeSnapshot {
		counter := &atomic.Int32{}
		counter.Store(int32(indegree))
		pendingDeps[taskID] = counter
	}

	// Create ready queue channel
	readyQueue := make(chan string, tasksCount)

	// Identify and queue all zero-indegree tasks
	for taskID, indegree := range indegreeSnapshot {
		if indegree == 0 {
			readyQueue <- taskID
		}
	}

	// Create execution state
	state := &executionState{
		executionID: executionID,
		pendingDeps: pendingDeps,
		readyQueue:  readyQueue,
		store:       store,
		taskIDs:     taskIDs,
	}

	// Execute tasks in parallel
	result := e.executeParallel(ctx, state, adjSnapshot, indegreeSnapshot)

	result.TopoOrder = topoSnapshot
	result.Store = store

	return result, nil
}

// executeParallel schedules and executes tasks in parallel based on their dependencies
// It spawns a goroutine for each ready task and manages the execution lifecycle
func (e *Engine) executeParallel(ctx context.Context, state *executionState, adj map[string][]string, indegree map[string]int) *ExecutionResult {
	var wg sync.WaitGroup
	reports := make(map[string]*TaskReport)
	var reportsMu sync.Mutex

	var sem chan struct{}
	if e.maxConcurrency > 0 {
		sem = make(chan struct{}, e.maxConcurrency)
	}

	// Track the number of tasks that have been scheduled
	tasksScheduled := &atomic.Int32{}
	tasksCompleted := &atomic.Int32{}

	// Count initial zero-indegree tasks
	initialTasks := int32(0)
	for _, indegreeCount := range indegree {
		if indegreeCount == 0 {
			initialTasks++
		}
	}
	tasksScheduled.Store(initialTasks)

	// Create cancellable context for Fail-Fast error handling
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Track if execution has failed
	executionFailed := &atomic.Bool{}

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
				for _, dependentID := range adj[taskID] {
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

			// Increment WaitGroup for this task
			wg.Add(1)

			if sem != nil {
				sem <- struct{}{}
			}

			// Spawn goroutine to execute task
			go func(tid string) {
				defer wg.Done()
				if sem != nil {
					defer func() { <-sem }()
				}

				// Execute the task
				report := e.executeTask(execCtx, tid, state.executionID, state.store)

				// Store the report
				reportsMu.Lock()
				reports[tid] = report
				reportsMu.Unlock()

				// Check if task failed and trigger Fail-Fast
				if report.Status == TaskStatusFailed && e.errorStrategy == FailFast {
					// Mark execution as failed
					executionFailed.Store(true)
					// Cancel execution context to signal all other tasks
					cancel()
				}

				// On task completion, process dependents
				for _, dependentID := range adj[tid] {
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
	for _, taskID := range state.taskIDs {
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
	if task.timeout > 0 {
		taskCtx, cancel = context.WithTimeout(execCtx, task.timeout)
	} else if e.defaultTaskTimeout > 0 {
		taskCtx, cancel = context.WithTimeout(execCtx, e.defaultTaskTimeout)
	} else {
		// No timeout, but we still need a cancel function for cleanup
		taskCtx, cancel = context.WithCancel(execCtx)
	}
	defer cancel()

	// Build handler chain: global middlewares + task middlewares + handler
	handlers := make([]HandlerFunc, 0, len(e.middlewares)+len(task.middlewares)+1)
	handlers = append(handlers, e.middlewares...)
	handlers = append(handlers, task.middlewares...)
	handlers = append(handlers, task.handler)

	// Create task Context
	ctx := &Context{
		taskID:      taskID,
		executionID: executionID,
		StartTime:   startTime,
		store:       store,
		handlers:    handlers,
		index:       -1, // Start at -1 so first Next() call advances to 0
	}

	// Execute handler chain starting from index 0
	err := ctx.Next(taskCtx)

	// Record end time and duration
	endTime := time.Now()
	report.EndTime = endTime
	report.Duration = endTime.Sub(startTime)

	// Update status based on execution result
	if err != nil {
		e.logger.Error(taskCtx, "task_failed", err,
			Field{Key: "task_id", Value: taskID},
			Field{Key: "execution_id", Value: executionID},
			Field{Key: "duration_ms", Value: report.Duration.Milliseconds()},
		)
		report.Status = TaskStatusFailed
		report.Err = err
		return report
	}

	e.logger.Info(taskCtx, "task_succeeded",
		Field{Key: "task_id", Value: taskID},
		Field{Key: "execution_id", Value: executionID},
		Field{Key: "duration_ms", Value: report.Duration.Milliseconds()},
	)
	report.Status = TaskStatusSuccess

	return report
}

// reset returns a clean Datastore for a new execution.
func (e *Engine) reset() Datastore {
	if e.store != nil {
		return e.store.Init()
	}
	return newMemoryStore()
}
