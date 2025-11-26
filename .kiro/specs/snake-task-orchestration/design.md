# Design Document

## Overview

snake is an in-process task orchestration framework for Golang that models task workflows as Directed Acyclic Graphs (DAG). The framework automatically schedules tasks based on their dependencies, executes them in parallel when possible, and manages data flow between tasks through a shared Datastore. The design follows a middleware-based execution model similar to gin, providing extensibility for cross-cutting concerns like recovery, tracing, and logging.

The core workflow involves:
1. Creating an Engine with configuration options
2. Registering Tasks with explicit dependencies
3. Building and validating the DAG structure
4. Executing the DAG with automatic parallel scheduling
5. Retrieving execution results and task outputs

## Architecture

### High-Level Component Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                          Engine                              │
│  ┌────────────┐  ┌──────────────┐  ┌──────────────────┐   │
│  │   Tasks    │  │  DAG Builder │  │    Scheduler     │   │
│  │  Registry  │  │  & Validator │  │  (Topological)   │   │
│  └────────────┘  └──────────────┘  └──────────────────┘   │
│  ┌────────────┐  ┌──────────────┐  ┌──────────────────┐   │
│  │  Options   │  │  Datastore   │  │   Middlewares    │   │
│  │   Config   │  │   (Shared)   │  │    (Global)      │   │
│  └────────────┘  └──────────────┘  └──────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                            │
                            │ Execute(ctx)
                            ▼
        ┌───────────────────────────────────────┐
        │      Parallel Task Execution          │
        │  ┌─────────┐  ┌─────────┐  ┌─────────┐
        │  │ Task A  │  │ Task B  │  │ Task C  │
        │  │Context  │  │Context  │  │Context  │
        │  └─────────┘  └─────────┘  └─────────┘
        │       │            │            │       │
        │       └────────────┼────────────┘       │
        │                    │                     │
        │              ┌─────▼─────┐              │
        │              │ Datastore │              │
        │              │  (Shared) │              │
        │              └───────────┘              │
        └───────────────────────────────────────┘
                            │
                            ▼
                   ExecutionResult
```

### Component Responsibilities

**Engine**: Central orchestrator that manages the entire lifecycle
- Maintains task registry and DAG structure
- Validates graph for cycles and missing dependencies
- Schedules tasks based on topological ordering
- Manages shared resources (Datastore, Logger, Middlewares)
- Implements Fail-Fast error handling

**Task**: Represents a single executable unit in the workflow
- Declares dependencies on other tasks
- Implements business logic via Handler function
- Can have task-specific middleware and timeout
- Writes results to Datastore using its Task ID as key

**Datastore**: Thread-safe storage for inter-task data flow
- Stores task results as key-value pairs (TaskID -> result)
- Provides concurrent read/write access with RWMutex
- Single writer per key (each task writes only its own result)
- Multiple readers can access any task's result

**Context**: Task-level execution context
- Wraps standard library context for cancellation/timeout
- Provides access to Datastore, Logger, and task metadata
- Manages middleware execution chain with Next() mechanism
- Isolated per task execution (no sharing between tasks)

**Middleware**: Pluggable execution wrappers
- Global middleware applies to all tasks
- Task-specific middleware applies to individual tasks
- Execution order: global → task-specific → handler
- Common uses: recovery, tracing, logging, metrics

## Components and Interfaces

### Engine

```go
type Engine struct {
    tasks       map[string]*Task
    middlewares []HandlerFunc
    store       Datastore
    logger      Logger
    opts        Options
    
    // DAG structure
    adj         map[string][]string  // adjacency list
    indegree    map[string]int       // incoming edge count
}

type Options struct {
    ErrorStrategy    ErrorStrategy
    GlobalTimeout    time.Duration
    DefaultTaskTimeout time.Duration
    Logger           Logger
}

type ErrorStrategy int

const (
    FailFast ErrorStrategy = iota
)

func NewEngine(opts ...Option) *Engine
func (e *Engine) Use(middleware ...HandlerFunc)
func (e *Engine) Register(task *Task) error
func (e *Engine) Build() error
func (e *Engine) Execute(ctx context.Context) (*ExecutionResult, error)
```

### Task

```go
type Task struct {
    ID          string
    DependsOn   []string
    Handler     HandlerFunc
    Middlewares []HandlerFunc
    Timeout     time.Duration
}

type HandlerFunc func(ctx *Context) error
```

### Datastore

```go
type Datastore interface {
    Set(taskID string, value any)
    Get(taskID string) (value any, ok bool)
}

type MapStore struct {
    mu   sync.RWMutex
    data map[string]any
}

func NewMapStore() *MapStore
func (s *MapStore) Set(taskID string, value any)
func (s *MapStore) Get(taskID string) (any, bool)
```

### Context

```go
type Context struct {
    ctx         context.Context
    
    TaskID      string
    ExecutionID string
    StartTime   time.Time
    
    Store       Datastore
    Logger      Logger
    
    handlers    []HandlerFunc
    index       int
}

func (c *Context) Next() error
func (c *Context) SetResult(value any)
func (c *Context) GetResult(taskID string) (any, bool)
func (c *Context) Context() context.Context
```

### Execution Result

```go
type TaskStatus string

const (
    TaskStatusPending  TaskStatus = "PENDING"
    TaskStatusSuccess  TaskStatus = "SUCCESS"
    TaskStatusFailed   TaskStatus = "FAILED"
    TaskStatusSkipped  TaskStatus = "SKIPPED"
)

type TaskReport struct {
    TaskID    string
    Status    TaskStatus
    Err       error
    StartTime time.Time
    EndTime   time.Time
    Duration  time.Duration
}

type ExecutionResult struct {
    ExecutionID string
    Success     bool
    Reports     map[string]*TaskReport
    Store       Datastore
}

func (r *ExecutionResult) GetResult(taskID string) (any, bool)
```

### Logger

```go
type Logger interface {
    Info(ctx context.Context, msg string, fields ...Field)
    Error(ctx context.Context, msg string, err error, fields ...Field)
    With(fields ...Field) Logger
}

type Field struct {
    Key   string
    Value any
}
```

## Data Models

### DAG Representation

The Engine maintains the DAG using two primary structures:

1. **Adjacency List** (`adj map[string][]string`): Maps each task to its list of dependent tasks (outgoing edges)
   - Key: Task ID
   - Value: List of Task IDs that depend on this task

2. **Indegree Map** (`indegree map[string]int`): Tracks the number of dependencies for each task
   - Key: Task ID
   - Value: Count of tasks that must complete before this task can run

### Task Execution State

During execution, the Engine maintains:

1. **Pending Dependencies** (`pendingDeps map[string]*atomic.Int32`): Atomic counters for remaining dependencies
   - Initialized to indegree value
   - Atomically decremented when dependencies complete
   - Task becomes ready when counter reaches zero

2. **Task Reports** (`reports map[string]*TaskReport`): Execution status for each task
   - Updated as tasks transition through states
   - Includes timing information and errors

3. **Ready Queue** (`chan string`): Channel of tasks ready to execute
   - Populated with zero-indegree tasks at start
   - Fed by completed tasks releasing their dependents

### Data Flow Model

```
Task A (produces data) → Datastore["A"] → Task B (consumes data)
                                        → Task C (consumes data)
```

- Each task writes to its own key (Task ID)
- Dependent tasks read from their dependencies' keys
- Datastore persists for entire execution lifetime
- All tasks share the same Datastore instance

## Correctness Properties

*A property is a characteristic or behavior that should hold true across all valid executions of a system—essentially, a formal statement about what the system should do. Properties serve as the bridge between human-readable specifications and machine-verifiable correctness guarantees.*


### Property 1: Options are applied correctly
*For any* set of Engine options, when creating an Engine with those options, the Engine's internal configuration should reflect all provided option values.
**Validates: Requirements 1.1**

### Property 2: Task registration stores tasks
*For any* Task with a unique ID and valid handler, when registering the task with an Engine, the task should be retrievable from the Engine's registry using its ID.
**Validates: Requirements 2.1**

### Property 3: Duplicate task IDs are rejected
*For any* two Tasks with the same ID, when registering the first task successfully and then attempting to register the second task, the second registration should return an error and the registry should contain only the first task.
**Validates: Requirements 2.2**

### Property 4: Empty task IDs are rejected
*For any* Task with an empty string ID, when attempting to register the task, the Engine should return an error and not store the task.
**Validates: Requirements 2.3**

### Property 5: Nil handlers are rejected
*For any* Task with a nil Handler function, when attempting to register the task, the Engine should return an error and not store the task.
**Validates: Requirements 2.4**

### Property 6: Adjacency list matches dependencies
*For any* set of registered tasks with dependencies, when building the DAG, the adjacency list should contain an edge from each dependency to each dependent task.
**Validates: Requirements 3.1**

### Property 7: Indegree matches dependency count
*For any* set of registered tasks, when building the DAG, each task's indegree should equal the number of tasks it depends on.
**Validates: Requirements 3.2**

### Property 8: Cycles are detected
*For any* set of tasks forming a circular dependency chain, when validating the DAG, the Engine should return an error indicating a cycle was detected.
**Validates: Requirements 3.3**

### Property 9: Missing dependencies are detected
*For any* task that declares a dependency on a non-registered task ID, when validating the DAG, the Engine should return an error indicating the missing dependency.
**Validates: Requirements 3.4**

### Property 10: Valid DAGs pass validation
*For any* acyclic set of tasks where all dependencies reference registered tasks, when validating the DAG, the Engine should complete validation without error.
**Validates: Requirements 3.6**

### Property 11: Execution IDs are unique
*For any* two separate calls to Execute, the ExecutionIDs in the returned ExecutionResults should be different.
**Validates: Requirements 4.1**

### Property 12: Datastore starts empty
*For any* execution, when Execute is called, the Datastore should be empty before any tasks run.
**Validates: Requirements 4.2**

### Property 13: Pending counts match indegree
*For any* DAG execution, when Execute initializes, each task's pending dependency count should equal its indegree value.
**Validates: Requirements 4.3**

### Property 14: Zero-indegree tasks start first
*For any* DAG execution, all tasks with zero indegree should be scheduled before any tasks with non-zero indegree.
**Validates: Requirements 4.4**

### Property 15: Disconnected graphs execute completely
*For any* DAG with multiple disconnected subgraphs, when Execute runs, all tasks in all subgraphs should reach a terminal state.
**Validates: Requirements 4.5**

### Property 16: Execution result contains required fields
*For any* execution, the returned ExecutionResult should contain an ExecutionID, Success boolean, Reports map, and Datastore reference.
**Validates: Requirements 4.6**

### Property 17: Ready tasks execute concurrently
*For any* DAG where multiple tasks have zero dependencies, when Execute runs, those tasks should start execution within a short time window of each other (indicating concurrent scheduling).
**Validates: Requirements 5.1**

### Property 18: Completion decrements dependent counts
*For any* task that completes successfully, all of its dependent tasks should have their pending dependency counts atomically decremented by one.
**Validates: Requirements 5.2**

### Property 19: Zero pending count triggers scheduling
*For any* task whose pending dependency count reaches zero, that task should be added to the ready queue and subsequently executed.
**Validates: Requirements 5.3**

### Property 20: Tasks execute in separate goroutines
*For any* set of ready tasks, each task should execute in its own goroutine without artificial concurrency limits.
**Validates: Requirements 5.4**

### Property 21: Execution completes when all tasks terminate
*For any* DAG execution, the Execute method should return only after all tasks have reached a terminal state (SUCCESS, FAILED, or SKIPPED).
**Validates: Requirements 5.5**

### Property 22: Failed tasks are marked FAILED
*For any* task whose handler returns a non-nil error, the task's status in the TaskReport should be FAILED.
**Validates: Requirements 6.1**

### Property 23: Failure cancels execution context
*For any* task that fails, the execution-level context should be cancelled, causing ctx.Done() to be closed for all other tasks.
**Validates: Requirements 6.2**

### Property 24: Unstarted tasks are marked SKIPPED
*For any* execution where a task fails, all tasks that have not yet started execution should have their status marked as SKIPPED in the final ExecutionResult.
**Validates: Requirements 6.3**

### Property 25: Failure result contains error details
*For any* execution where a task fails, the ExecutionResult Success field should be false and the failed task's TaskReport should contain the error.
**Validates: Requirements 6.5**

### Property 26: Context contains Datastore reference
*For any* task execution, the Context passed to the handler should contain a non-nil Datastore reference.
**Validates: Requirements 8.1**

### Property 27: SetResult uses task ID as key
*For any* task, when calling Context.SetResult(value), the value should be stored in the Datastore under the task's own ID.
**Validates: Requirements 8.2**

### Property 28: Datastore is thread-safe
*For any* set of concurrent operations on the Datastore (reads and writes), no data races should occur and all operations should complete successfully.
**Validates: Requirements 8.6**

### Property 29: Each task gets unique Context
*For any* two tasks executing concurrently, each should receive a distinct Context instance with its own TaskID and state.
**Validates: Requirements 10.1**

### Property 30: Context inherits execution context
*For any* task execution, when the execution-level context is cancelled, the task's Context should also be cancelled (ctx.Done() should be closed).
**Validates: Requirements 10.2**

### Property 31: Task timeout cancels context
*For any* task with a Timeout configured, if the task runs longer than the timeout, the task's context should be cancelled with a deadline exceeded error.
**Validates: Requirements 10.3**

### Property 32: Context contains correct task ID
*For any* task execution, the Context.TaskID field should match the ID of the task being executed.
**Validates: Requirements 10.4**

### Property 33: Context logger has task metadata
*For any* task execution, the Context.Logger should include fields for TaskID and ExecutionID.
**Validates: Requirements 10.6**

### Property 34: Context contains execution ID
*For any* task execution, the Context.ExecutionID should match the ExecutionID of the overall execution.
**Validates: Requirements 10.7**

### Property 35: Context has start time
*For any* task execution, the Context.StartTime should be set to a timestamp at or before the handler begins executing.
**Validates: Requirements 10.8**

### Property 36: Global middleware applies to all tasks
*For any* global middleware registered with the Engine, the middleware should execute for every task in the DAG.
**Validates: Requirements 11.1**

### Property 37: Task middleware applies only to that task
*For any* task with task-specific middleware, that middleware should execute only for that task and not for other tasks.
**Validates: Requirements 11.2**

### Property 38: Handler chain order is correct
*For any* task execution, the execution order should be: global middleware (in registration order), then task middleware (in registration order), then the task handler.
**Validates: Requirements 11.3**

### Property 39: Next invokes next handler
*For any* middleware that calls Context.Next(), the next function in the handler chain should be invoked.
**Validates: Requirements 11.5**

### Property 40: Middleware executes in reverse on return
*For any* middleware chain, code after Context.Next() should execute in reverse order (last middleware registered executes its post-Next code first).
**Validates: Requirements 11.6**

### Property 41: Initial status is PENDING
*For any* task before execution starts, the task's status should be PENDING.
**Validates: Requirements 13.1**

### Property 42: Successful tasks are marked SUCCESS
*For any* task whose handler returns nil error, the task's status in the TaskReport should be SUCCESS.
**Validates: Requirements 13.2**

### Property 43: Task reports contain required fields
*For any* task execution, the TaskReport should contain TaskID, Status, Err (may be nil), StartTime, EndTime, and Duration.
**Validates: Requirements 13.5**

### Property 44: All tasks have reports
*For any* execution, the ExecutionResult.Reports map should contain a TaskReport entry for every registered task.
**Validates: Requirements 13.6**

### Property 45: Result contains execution ID
*For any* execution, the ExecutionResult.ExecutionID should be non-empty and match the ID used during execution.
**Validates: Requirements 14.1**

### Property 46: Success is true when all tasks succeed
*For any* execution where all tasks complete with SUCCESS status, the ExecutionResult.Success field should be true.
**Validates: Requirements 14.2**

### Property 47: Success is false when any task fails
*For any* execution where at least one task has FAILED or SKIPPED status, the ExecutionResult.Success field should be false.
**Validates: Requirements 14.3**

### Property 48: Result provides Datastore access
*For any* execution, the ExecutionResult should provide access to the Datastore containing all task results.
**Validates: Requirements 14.5**

### Property 49: Default timeout is used when task timeout is unset
*For any* task without a Timeout field set, when the task executes, the Engine should apply the default task timeout from Engine options.
**Validates: Requirements 15.2**

### Property 50: Timeout cancels context with deadline error
*For any* task that exceeds its timeout, the task's context should be cancelled and ctx.Err() should return a deadline exceeded error.
**Validates: Requirements 15.3**

### Property 51: Timeout results in FAILED status
*For any* task that times out, the task's status should be FAILED and the error should indicate a timeout occurred.
**Validates: Requirements 15.4**

### Property 52: Logs include task metadata
*For any* log message written through Context.Logger, the log should include TaskID and ExecutionID fields.
**Validates: Requirements 16.5**

### Property 53: Failure in one subgraph cancels all subgraphs
*For any* disconnected DAG where a task in one subgraph fails, tasks in all other subgraphs should either complete or be marked SKIPPED due to context cancellation.
**Validates: Requirements 17.4**

## Error Handling

### Error Strategy: Fail-Fast

The MVP implementation supports only Fail-Fast error handling:

1. **Task Failure Detection**
   - Any task handler returning non-nil error triggers failure
   - Panics (if recovery middleware is used) are converted to errors
   - Timeouts result in context deadline exceeded errors

2. **Cancellation Propagation**
   - Failed task causes execution-level context cancellation
   - Context cancellation propagates to all running tasks via ctx.Done()
   - Tasks should check ctx.Done() or ctx.Err() periodically

3. **Task State Transitions**
   - Failed task: PENDING → FAILED
   - Running tasks: May complete or fail with context.Canceled
   - Pending tasks: PENDING → SKIPPED (never started)

4. **Error Information**
   - TaskReport.Err contains the original error
   - ExecutionResult.Success = false
   - All task reports available for inspection

### Timeout Handling

1. **Timeout Hierarchy**
   - Global execution timeout (from Execute context)
   - Default task timeout (from Engine options)
   - Task-specific timeout (from Task.Timeout field)

2. **Timeout Application**
   - Task timeout wraps execution context: `context.WithTimeout(execCtx, taskTimeout)`
   - Timeout expiration cancels task context
   - Task handler should respect context cancellation

3. **Timeout Errors**
   - Timeout results in context.DeadlineExceeded error
   - Task marked as FAILED
   - Triggers Fail-Fast cancellation

### Validation Errors

1. **Registration Errors**
   - Duplicate task ID
   - Empty task ID
   - Nil handler function
   - Return error immediately, do not store task

2. **Build/Validation Errors**
   - Circular dependency detected
   - Missing dependency reference
   - Return error before execution
   - Provide detailed error message with task IDs involved

## Testing Strategy

### Unit Testing

Unit tests will verify specific behaviors and edge cases:

1. **Engine Configuration**
   - Test each option function sets the correct field
   - Test option composition (multiple options)
   - Test default values when options not provided

2. **Task Registration**
   - Test successful registration
   - Test duplicate ID rejection
   - Test empty ID rejection
   - Test nil handler rejection

3. **DAG Building**
   - Test adjacency list construction for simple graphs
   - Test indegree calculation for various topologies
   - Test cycle detection with known cyclic graphs
   - Test missing dependency detection

4. **Datastore Operations**
   - Test Set and Get operations
   - Test Get on non-existent key returns false
   - Test concurrent access (using race detector)

5. **Context Operations**
   - Test SetResult writes to correct key
   - Test GetResult reads from correct key
   - Test Next() advances through handler chain
   - Test context cancellation propagation

6. **Middleware Chain**
   - Test global middleware execution
   - Test task-specific middleware execution
   - Test execution order (global → task → handler)
   - Test reverse order after Next()

7. **Error Handling**
   - Test task failure marks status as FAILED
   - Test Fail-Fast cancels execution
   - Test pending tasks marked as SKIPPED
   - Test timeout handling

### Property-Based Testing

Property-based tests will verify universal properties across many inputs using the **testify/quick** library for Go. Each test will run a minimum of 100 iterations.

1. **DAG Generation Strategy**
   - Generate random valid DAGs (acyclic, all dependencies exist)
   - Generate random invalid DAGs (with cycles, missing dependencies)
   - Generate random task counts (1-20 tasks)
   - Generate random dependency patterns (linear, tree, diamond, disconnected)

2. **Task Behavior Generation**
   - Generate tasks that succeed immediately
   - Generate tasks that fail with random errors
   - Generate tasks that sleep for random durations
   - Generate tasks that write random data to Datastore

3. **Property Test Configuration**
   - Minimum 100 iterations per property test
   - Use Go's race detector during property tests
   - Tag each property test with comment referencing design property

4. **Key Properties to Test**
   - Topological ordering: dependencies always execute before dependents
   - Data flow: dependent tasks can read dependency results
   - Concurrency: independent tasks execute concurrently
   - Fail-Fast: any failure stops execution and marks pending tasks SKIPPED
   - Completeness: all tasks reach terminal state
   - Thread safety: no data races in Datastore or Engine state

### Testing Tools

- **Testing Framework**: Go standard library `testing` package
- **Property-Based Testing**: `testing/quick` (standard library)
- **Race Detection**: `go test -race`
- **Assertions**: `github.com/stretchr/testify/assert` for cleaner test code
- **Mocking**: `github.com/stretchr/testify/mock` for Logger interface

### Test Organization

```
snake/
├── engine.go
├── engine_test.go          # Unit tests for Engine
├── task.go
├── task_test.go            # Unit tests for Task
├── datastore.go
├── datastore_test.go       # Unit tests + property tests for Datastore
├── context.go
├── context_test.go         # Unit tests for Context
├── middleware_test.go      # Unit tests for middleware chain
├── integration_test.go     # Property tests for full DAG execution
└── examples/
    └── recovery_middleware.go  # Example recovery middleware
```

### Property Test Tagging Convention

Each property-based test must include a comment tag:

```go
// Property: snake-task-orchestration, Property 8: Cycles are detected
func TestProperty_CyclesDetected(t *testing.T) {
    // Property test implementation
}
```

## Implementation Notes

### Concurrency Model

1. **Goroutine Per Task**
   - Each ready task spawns a new goroutine
   - No worker pool or concurrency limit in MVP
   - Simplifies implementation and testing

2. **Synchronization Points**
   - Atomic operations for pending dependency counts
   - Mutex-protected Datastore access
   - Channel-based ready queue for task scheduling
   - WaitGroup for tracking active tasks

3. **Context Propagation**
   - Execution-level context passed to Execute()
   - Task-level context derived from execution context
   - Timeout contexts wrap task contexts when needed
   - Cancellation propagates down the context tree

### Middleware Execution Model

The middleware chain follows the gin pattern:

```go
// Middleware wraps the next handler
func LoggingMiddleware(ctx *Context) error {
    start := time.Now()
    ctx.Logger.Info(ctx.Context(), "Task starting")
    
    // Call next handler in chain
    err := ctx.Next()
    
    // Post-execution code runs in reverse order
    duration := time.Since(start)
    ctx.Logger.Info(ctx.Context(), "Task completed", 
        Field{"duration", duration})
    
    return err
}
```

Chain construction:
1. Collect global middlewares
2. Append task-specific middlewares
3. Append task handler as final element
4. Store in Context.handlers slice
5. Execute starting from index 0

### Datastore Design Decisions

1. **Simple Map Implementation**
   - MVP uses `map[string]any` with RWMutex
   - Sufficient for in-process execution
   - Easy to test and reason about

2. **No Type Safety**
   - Values stored as `any` type
   - Caller responsible for type assertions
   - Future: could add typed wrapper methods

3. **No Persistence**
   - Datastore lives only for one execution
   - Fresh Datastore for each Execute() call
   - Future: could add persistence layer

### Execution Algorithm

```
1. Initialize:
   - Create ExecutionID
   - Create empty Datastore
   - Initialize pendingDeps[taskID] = indegree[taskID] for all tasks
   - Create ready queue channel
   - Create WaitGroup for tracking active tasks

2. Seed ready queue:
   - For each task where indegree == 0:
     - Add task ID to ready queue

3. Main execution loop:
   - Spawn goroutine to read from ready queue
   - For each ready task:
     - Increment WaitGroup
     - Spawn goroutine to execute task:
       a. Create task Context
       b. Build handler chain
       c. Execute chain
       d. Record result in TaskReport
       e. If success: write to Datastore
       f. If error: cancel execution context
       g. For each dependent:
          - Decrement pendingDeps atomically
          - If pendingDeps == 0: add to ready queue
       h. Decrement WaitGroup

4. Wait for completion:
   - WaitGroup.Wait() blocks until all tasks done
   - Close ready queue channel
   - Return ExecutionResult

5. Handle cancellation:
   - Mark unstarted tasks as SKIPPED
   - Collect all TaskReports
   - Set ExecutionResult.Success based on task statuses
```

### Future Extensions

While not part of MVP, the design accommodates:

1. **Conditional Execution**
   - Special error type `ErrSkipTask` to skip without failing
   - Dependent tasks could check upstream status

2. **Retry Logic**
   - Task.Retry field already defined
   - Retry middleware could wrap handler

3. **Concurrency Limits**
   - Worker pool instead of unlimited goroutines
   - Semaphore before task execution

4. **Dynamic DAG**
   - Tasks could register new tasks during execution
   - Would require careful synchronization

5. **Observability**
   - Export DAG as Graphviz DOT
   - Export execution trace as JSON
   - Integration with OpenTelemetry

6. **Persistence**
   - Pluggable Datastore implementations
   - Save/restore execution state
   - Durable task queue
