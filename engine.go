package snake

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ErrorStrategy defines how the engine handles task failures
type ErrorStrategy int

const (
	// FailFast stops execution immediately when any task fails
	FailFast ErrorStrategy = iota
	// RunAll attempts to execute all independent paths even if some tasks fail
	RunAll
)

// Option is a function that configures an Engine
type Option func(*Engine)

// WithErrorStrategy sets the error handling strategy
func WithErrorStrategy(strategy ErrorStrategy) Option {
	return func(e *Engine) {
		e.errorStrategy = strategy
	}
}

// WithDatastoreFactory sets a custom datastore factory
// The factory will be called for each execution to create a fresh Datastore instance
func WithDatastoreFactory(factory DatastoreFactory) Option {
	return func(e *Engine) {
		if factory != nil {
			e.storeFactory = factory
		}
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
		if logger != nil {
			e.logger = logger
		}
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
	storeFactory   DatastoreFactory
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
		tasks:       make(map[string]*Task),
		middlewares: make([]HandlerFunc, 0),
		storeFactory: func() Datastore {
			return newMemoryStore()
		},
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
		if task == nil {
			return ErrNilTask
		}
		if task.id == "" {
			return ErrEmptyTaskID
		}
		if task.handler == nil {
			return ErrEmptyTaskHandler
		}
		// Validate unique ID
		if _, exists := e.tasks[task.id]; exists {
			return fmt.Errorf("task %q already exists", task.id)
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
	readyQueue  chan string
	store       Datastore
	taskIDs     []string
	input       any
}

// Execute runs the DAG with the provided context and input
// It initializes execution state and schedules tasks for parallel execution
func (e *Engine) Execute(ctx context.Context, input any) (*ExecutionResult, error) {
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
		readyQueue:  readyQueue,
		store:       store,
		taskIDs:     taskIDs,
		input:       input,
	}

	// Execute tasks in parallel
	result, err := e.executeParallel(ctx, state, adjSnapshot, indegreeSnapshot)

	if result != nil {
		result.TopoOrder = topoSnapshot
		result.Store = store
	}

	return result, err
}

// taskResult represents the outcome of a task execution
type taskResult struct {
	TaskID string
	Report *TaskReport
	Err    error
}

// executeParallel schedules and executes tasks using a worker pool and event loop.
// It eliminates lock contention by centralizing state management in the coordinator loop.
func (e *Engine) executeParallel(ctx context.Context, state *executionState, adj map[string][]string, indegree map[string]int) (*ExecutionResult, error) {
	// 1. Setup Channels
	// readyQueue: Buffered to hold task IDs waiting for workers
	// resultsChan: Buffered to avoid workers blocking when sending results
	tasksCount := len(e.tasks)

	// Note: readyQueue is already created in Execute
	resultsChan := make(chan taskResult, tasksCount)

	// 2. Start Workers
	concurrency := e.maxConcurrency
	if concurrency <= 0 {
		concurrency = tasksCount
	}
	if concurrency > tasksCount {
		concurrency = tasksCount
	}

	var wg sync.WaitGroup
	// Create a worker context to cancel workers when execution is done
	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()

	// Start workers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.worker(workerCtx, state, resultsChan)
		}()
	}

	// 3. Coordinator Loop (Main Thread)
	// Manages state: reports, pendingDeps, scheduling

	reports := make(map[string]*TaskReport)

	tasksCompleted := 0
	tasksTotal := tasksCount

	var firstErr error
	var executionFailed bool

	// Helper to push ready tasks
	pushReady := func(taskIDs ...string) {
		for _, tid := range taskIDs {
			select {
			case state.readyQueue <- tid:
			default:
				// Should not happen if buffer is sufficient, but good to handle
				e.logger.Error(ctx, "ready_queue_full", fmt.Errorf("queue full for task %s", tid))
			}
		}
	}

	// checkDependents checks if a task's dependents are ready to run
	checkDependents := func(completedTaskID string) {
		for _, dependentID := range adj[completedTaskID] {
			indegree[dependentID]--
			if indegree[dependentID] == 0 {
				// Check upstream status for the dependent task
				depTask := e.tasks[dependentID]
				var upstreamErr error
				var upstreamSkipped bool

				for _, parentID := range depTask.dependsOn {
					parentReport := reports[parentID]
					if parentReport == nil {
						continue // Should not happen if logic is correct
					}
					// If parent failed/cancelled and allowFailure is false -> dependent fails
					if parentReport.Status == TaskStatusFailed || parentReport.Status == TaskStatusCancelled {
						if !e.tasks[parentID].allowFailure {
							upstreamErr = fmt.Errorf("upstream task %s failed or cancelled", parentID)
						}
					}
					// If any parent skipped -> dependent might skip
					if parentReport.Status == TaskStatusSkipped {
						upstreamSkipped = true
					}
				}

				if upstreamErr != nil {
					// Cancel propagation: Send synthetic result
					resultsChan <- taskResult{
						TaskID: dependentID,
						Report: &TaskReport{
							TaskID:    dependentID,
							Status:    TaskStatusCancelled,
							Err:       upstreamErr,
							StartTime: time.Now(),
							EndTime:   time.Now(),
							Duration:  0,
						},
					}
				} else if upstreamSkipped {
					// Skip propagation: Send synthetic result
					resultsChan <- taskResult{
						TaskID: dependentID,
						Report: &TaskReport{
							TaskID:    dependentID,
							Status:    TaskStatusSkipped,
							StartTime: time.Now(),
							EndTime:   time.Now(),
							Duration:  0,
						},
					}
				} else {
					// Ready to run
					pushReady(dependentID)
				}
			}
		}
	}

coordinatorLoop:
	// Loop until all tasks are accounted for
	for tasksCompleted < tasksTotal {
		select {
		case res := <-resultsChan:
			tasksCompleted++
			reports[res.TaskID] = res.Report

			// Process failure/cancellation first for FailFast check
			if res.Report.Status == TaskStatusFailed || res.Report.Status == TaskStatusCancelled {
				if res.Err != nil {
					if firstErr == nil {
						firstErr = res.Err
					}

					// Fail Fast Strategy
					task := e.tasks[res.TaskID]
					if e.errorStrategy == FailFast && !task.allowFailure && !executionFailed {
						executionFailed = true
						workerCancel() // Stop workers from starting new tasks
						break coordinatorLoop
					}
				}
			}

			// For any completion state (Success, Failed, Skipped, Cancelled), we verify dependents
			// If a task failed, its dependents will see the failure via reports[] check in checkDependents
			checkDependents(res.TaskID)

		case <-ctx.Done():
			// Context Cancelled (Global Timeout or External Cancel)
			if firstErr == nil {
				firstErr = ctx.Err()
			}
			executionFailed = true
			workerCancel()
			break coordinatorLoop
		}
	}

	// Close queue to ensure any stuck workers exit
	close(state.readyQueue)
	workerCancel()

	// Drain any tasks left in the readyQueue and mark them CANCELLED
	// This happens after close, so the range will terminate once queue is empty
	for taskID := range state.readyQueue {
		resultsChan <- taskResult{
			TaskID: taskID,
			Report: &TaskReport{
				TaskID:    taskID,
				Status:    TaskStatusCancelled,
				StartTime: time.Now(),
				EndTime:   time.Now(),
				Duration:  0,
			},
		}
	}

	wg.Wait()
	close(resultsChan)

	// Drain any remaining results from the channel
	for res := range resultsChan {
		reports[res.TaskID] = res.Report
		if res.Err != nil && firstErr == nil {
			firstErr = res.Err
		}
	}

	// Post-processing: Mark missing tasks as Cancelled
	if executionFailed {
		for taskID := range e.tasks {
			if _, exists := reports[taskID]; !exists {
				reports[taskID] = &TaskReport{
					TaskID:    taskID,
					Status:    TaskStatusCancelled,
					StartTime: time.Now(),
					EndTime:   time.Now(),
					Duration:  0,
				}
			}
		}
	}

	// Determine success
	success := true
	if executionFailed {
		success = false
	} else {
		for _, report := range reports {
			if report.Status == TaskStatusFailed || report.Status == TaskStatusCancelled {
				task := e.tasks[report.TaskID]
				if !task.allowFailure {
					success = false
					break
				}
			}
		}
	}

	result := &ExecutionResult{
		ExecutionID: state.executionID,
		Success:     success,
		Reports:     reports,
		Store:       state.store,
	}

	return result, firstErr
}

// worker constantly consumes tasks from the ready queue and executes them
func (e *Engine) worker(ctx context.Context, state *executionState, resultsChan chan<- taskResult) {
	for {
		select {
		case <-ctx.Done():
			return
		case taskID, ok := <-state.readyQueue:
			if !ok {
				return
			}
			// Double-check context after dequeue to avoid executing tasks post-cancellation
			if ctx.Err() != nil {
				// Context already cancelled, send a synthetic cancelled result
				resultsChan <- taskResult{
					TaskID: taskID,
					Report: &TaskReport{
						TaskID:    taskID,
						Status:    TaskStatusCancelled,
						Err:       ctx.Err(),
						StartTime: time.Now(),
						EndTime:   time.Now(),
						Duration:  0,
					},
					Err: ctx.Err(),
				}
				continue
			}
			e.executeTaskAndReport(ctx, taskID, state, resultsChan)
		}
	}
}

func (e *Engine) executeTaskAndReport(ctx context.Context, taskID string, state *executionState, resultsChan chan<- taskResult) {
	report, err := e.executeTask(ctx, taskID, state.executionID, state.store, state.input)

	resultsChan <- taskResult{
		TaskID: taskID,
		Report: report,
		Err:    err,
	}
}

// executeTask executes a single task with proper context, timeout, and middleware chain
// It creates the task Context, applies timeouts, builds the handler chain, and records execution results
func (e *Engine) executeTask(execCtx context.Context, taskID string, executionID string, store Datastore, input any) (*TaskReport, error) {
	// Get the task
	task := e.tasks[taskID]

	// Record start time
	startTime := time.Now()

	// Create task context with timeout early to cover condition check
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

	// Create full task Context
	// Unified context for both Condition check and Execution
	ctx := &Context{
		taskID:      taskID,
		executionID: executionID,
		StartTime:   startTime,
		store:       store,
		handlers:    handlers,
		index:       -1, // Start at -1 so first Next() call advances to 0
		input:       input,
	}

	// Check Condition using the full context
	if task.condition != nil {
		// Execute condition function
		// Note: Condition should be pure logic, but we pass the full context for access to everything
		shouldRun := task.condition(taskCtx, ctx)
		if !shouldRun {
			return &TaskReport{
				TaskID:    taskID,
				Status:    TaskStatusSkipped,
				StartTime: startTime,
				EndTime:   time.Now(),
				Duration:  time.Since(startTime),
			}, nil
		}
	}

	// Execute handler chain starting from index 0
	err := ctx.Next(taskCtx)

	// Record end time and duration
	endTime := time.Now()
	report := &TaskReport{
		TaskID:    taskID,
		StartTime: startTime,
		EndTime:   endTime,
		Duration:  endTime.Sub(startTime),
	}

	// Update status based on execution result
	if err != nil {
		e.logger.Error(taskCtx, "task_failed", err,
			Field{Key: "task_id", Value: taskID},
			Field{Key: "execution_id", Value: executionID},
			Field{Key: "duration_ms", Value: report.Duration.Milliseconds()},
		)
		report.Status = TaskStatusFailed
		report.Err = err
		return report, err
	}

	e.logger.Info(taskCtx, "task_succeeded",
		Field{Key: "task_id", Value: taskID},
		Field{Key: "execution_id", Value: executionID},
		Field{Key: "duration_ms", Value: report.Duration.Milliseconds()},
	)
	report.Status = TaskStatusSuccess

	return report, nil
}

// reset returns a clean Datastore for a new execution by calling the factory.
func (e *Engine) reset() Datastore {
	return e.storeFactory()
}
