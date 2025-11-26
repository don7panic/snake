# Requirements Document

## Introduction

snake is an in-process task orchestration framework for Golang that uses Directed Acyclic Graphs (DAG) to model task dependencies. The framework provides explicit dependency modeling, automatic data flow between tasks, parallel execution capabilities, and pluggable middleware similar to gin's middleware model. The system enables developers to define complex task workflows with automatic scheduling, data passing, and extensible execution pipelines.

## Glossary

- **Engine**: The core orchestration component responsible for DAG construction, validation, task scheduling, and execution management
- **Task**: A single executable business unit node in the DAG that declares dependencies and implements business logic
- **DAG (Directed Acyclic Graph)**: A graph structure representing task dependencies where edges point from dependencies to dependent tasks, with no cycles
- **Datastore**: A thread-safe key-value store that manages data flow between tasks, storing each task's execution results
- **Context**: A task-level execution context that encapsulates the standard library context, provides access to Datastore, Logger, and manages the middleware execution chain
- **Middleware**: Pluggable functions that wrap task execution to provide cross-cutting concerns like recovery, tracing, logging, and metrics
- **HandlerFunc**: The unified function signature `func(ctx *Context) error` used for both middleware and task handlers
- **Fail-Fast**: An error handling strategy where any task failure immediately cancels the entire execution
- **Task Status**: The execution state of a task (PENDING, SUCCESS, FAILED, SKIPPED)
- **Execution Result**: The comprehensive output of a DAG execution including overall status, individual task reports, and the Datastore
- **Topological Sort**: An algorithm for ordering DAG nodes such that dependencies always come before dependent tasks
- **Indegree**: The count of incoming edges (dependencies) for a task node

## Requirements

### Requirement 1

**User Story:** As a developer, I want to create an Engine with configurable options, so that I can control execution behavior like error handling, timeouts, logging, and middleware.

#### Acceptance Criteria

1. WHEN a developer calls NewEngine with option functions THEN the Engine SHALL apply all provided options to its internal configuration
2. THE Engine SHALL support an option for setting the error handling strategy to Fail-Fast mode
3. THE Engine SHALL support an option for configuring a global execution timeout duration
4. THE Engine SHALL support an option for setting a default task timeout duration
5. THE Engine SHALL support an option for injecting a custom Logger implementation
6. THE Engine SHALL support an option for registering global middleware functions that apply to all tasks

### Requirement 2

**User Story:** As a developer, I want to register tasks with the Engine, so that I can build a task dependency graph for execution.

#### Acceptance Criteria

1. WHEN a developer registers a Task with a unique ID THEN the Engine SHALL store the Task in its internal registry
2. WHEN a developer attempts to register a Task with a duplicate ID THEN the Engine SHALL return an error and reject the registration
3. WHEN a Task is registered THEN the Engine SHALL validate that the Task has a non-empty ID
4. WHEN a Task is registered THEN the Engine SHALL validate that the Task has a non-nil Handler function
5. THE Engine SHALL maintain a map of all registered Tasks indexed by Task ID

### Requirement 3

**User Story:** As a developer, I want the Engine to build and validate the DAG, so that I can detect configuration errors before execution.

#### Acceptance Criteria

1. WHEN the Engine builds the DAG THEN the Engine SHALL construct an adjacency list representation of task dependencies
2. WHEN the Engine builds the DAG THEN the Engine SHALL calculate the indegree for each task node
3. WHEN the Engine validates the DAG THEN the Engine SHALL detect circular dependencies and return an error if any cycle exists
4. WHEN the Engine validates the DAG THEN the Engine SHALL verify that all dependency Task IDs reference registered tasks
5. WHEN a Task declares a dependency on a non-existent Task ID THEN the Engine SHALL return an error during validation
6. WHEN the DAG is acyclic and all dependencies exist THEN the Engine SHALL complete validation successfully

### Requirement 4

**User Story:** As a developer, I want to execute the DAG with a context, so that I can run all tasks according to their dependencies with timeout and cancellation support.

#### Acceptance Criteria

1. WHEN a developer calls Execute with a context THEN the Engine SHALL create a new ExecutionID for this execution
2. WHEN Execute is called THEN the Engine SHALL initialize a new Datastore instance for this execution
3. WHEN Execute is called THEN the Engine SHALL initialize each Task's pending dependency count to its indegree value
4. WHEN Execute starts THEN the Engine SHALL identify all tasks with zero indegree and add them to the ready queue
5. THE Engine SHALL support non-connected graphs by allowing multiple tasks with zero indegree to execute concurrently
6. WHEN Execute completes THEN the Engine SHALL return an ExecutionResult containing overall status, task reports, and the Datastore

### Requirement 5

**User Story:** As a developer, I want tasks to execute in parallel when dependencies allow, so that I can maximize throughput and minimize total execution time.

#### Acceptance Criteria

1. WHEN multiple tasks have zero pending dependencies THEN the Engine SHALL schedule all ready tasks for concurrent execution
2. WHEN a task completes successfully THEN the Engine SHALL atomically decrement the pending dependency count for all dependent tasks
3. WHEN a dependent task's pending dependency count reaches zero THEN the Engine SHALL add that task to the ready queue
4. THE Engine SHALL execute each ready task in a separate goroutine without limiting maximum concurrency
5. WHEN all tasks reach a terminal state THEN the Engine SHALL complete execution

### Requirement 6

**User Story:** As a developer, I want the Engine to implement Fail-Fast error handling, so that execution stops immediately when any task fails.

#### Acceptance Criteria

1. WHEN a task handler returns a non-nil error THEN the Engine SHALL mark that task's status as FAILED
2. WHEN a task fails THEN the Engine SHALL cancel the execution-level context to signal all other tasks
3. WHEN the execution context is cancelled THEN the Engine SHALL mark all pending tasks that have not started as SKIPPED
4. WHEN a task is executing and receives a cancelled context THEN the task SHALL have the opportunity to handle the cancellation
5. WHEN execution completes after a failure THEN the ExecutionResult SHALL indicate overall failure and include error details in the failed task's report

### Requirement 7

**User Story:** As a developer, I want to define Tasks with explicit dependencies, so that the framework can automatically schedule execution in the correct order.

#### Acceptance Criteria

1. WHEN a Task is created THEN the Task SHALL have a unique string ID
2. WHEN a Task is created THEN the Task SHALL have a DependsOn field containing a list of Task IDs
3. WHEN a Task is created THEN the Task SHALL have a Handler function of type HandlerFunc
4. THE Task SHALL support an optional Timeout field to override the default task timeout
5. THE Task SHALL support an optional Middlewares field containing task-specific middleware functions
6. WHEN a Task's dependencies are satisfied THEN the Engine SHALL make the Task eligible for execution

### Requirement 8

**User Story:** As a developer, I want Tasks to access a shared Datastore through Context, so that tasks can pass data to their dependents.

#### Acceptance Criteria

1. WHEN a Task handler executes THEN the handler SHALL receive a Context containing a reference to the Datastore
2. WHEN a Task writes its result THEN the Task SHALL use its own Task ID as the key in the Datastore
3. WHEN a Task reads dependency data THEN the Task SHALL use the dependency's Task ID as the key in the Datastore
4. THE Datastore SHALL provide a Set method that accepts a Task ID and a value of any type
5. THE Datastore SHALL provide a Get method that accepts a Task ID and returns the value and a boolean indicating existence
6. THE Datastore SHALL be thread-safe for concurrent reads and writes from multiple tasks

### Requirement 9

**User Story:** As a developer, I want the Datastore to be thread-safe, so that concurrent task execution does not cause data races or corruption.

#### Acceptance Criteria

1. WHEN multiple tasks write to the Datastore concurrently THEN the Datastore SHALL serialize writes using a write lock
2. WHEN multiple tasks read from the Datastore concurrently THEN the Datastore SHALL allow concurrent reads using a read lock
3. WHEN a task writes while others are reading THEN the Datastore SHALL block the write until reads complete
4. THE Datastore SHALL use a sync.RWMutex to protect its internal map
5. THE Datastore SHALL store values in a map with string keys and any type values

### Requirement 10

**User Story:** As a developer, I want each Task execution to have its own Context, so that task-specific information and resources are properly isolated.

#### Acceptance Criteria

1. WHEN the Engine executes a Task THEN the Engine SHALL create a new Context instance for that Task
2. WHEN a Task Context is created THEN the Context SHALL contain the execution-level context as a child context
3. WHEN a Task has a timeout configured THEN the Context SHALL wrap the execution context with a timeout context
4. THE Context SHALL contain the current Task's ID
5. THE Context SHALL contain a reference to the shared Datastore
6. THE Context SHALL contain a Logger instance tagged with the Task ID and ExecutionID
7. THE Context SHALL contain the ExecutionID for this DAG execution
8. THE Context SHALL contain a start time timestamp for the task execution

### Requirement 11

**User Story:** As a developer, I want to implement middleware functions, so that I can add cross-cutting concerns like recovery, tracing, and logging to task execution.

#### Acceptance Criteria

1. WHEN middleware is registered globally THEN the middleware SHALL apply to all Task executions
2. WHEN middleware is registered on a specific Task THEN the middleware SHALL apply only to that Task's execution
3. WHEN a Task executes THEN the Engine SHALL build a handler chain consisting of global middleware, task middleware, and the task handler
4. THE middleware and handler functions SHALL all use the HandlerFunc signature
5. WHEN a middleware function calls Context.Next THEN the Context SHALL invoke the next function in the handler chain
6. WHEN the handler chain completes THEN control SHALL return through the middleware chain in reverse order
7. THE Context SHALL maintain an index to track the current position in the handler chain

### Requirement 12

**User Story:** As a developer, I want to implement a recovery middleware, so that a panic in one task does not crash the entire execution.

#### Acceptance Criteria

1. WHEN a recovery middleware is registered THEN the middleware SHALL wrap the Next call in a panic recovery block
2. WHEN a task handler panics THEN the recovery middleware SHALL catch the panic and convert it to an error
3. WHEN a panic is recovered THEN the middleware SHALL mark the task as FAILED with an error describing the panic
4. WHEN a panic is recovered THEN the middleware SHALL log the panic details including stack trace
5. WHEN a panic is recovered in Fail-Fast mode THEN the execution SHALL cancel as with any other task failure

### Requirement 13

**User Story:** As a developer, I want the Engine to track task execution status, so that I can understand what happened during execution.

#### Acceptance Criteria

1. WHEN a Task has not yet started THEN the Task status SHALL be PENDING
2. WHEN a Task completes without error THEN the Task status SHALL be SUCCESS
3. WHEN a Task returns an error THEN the Task status SHALL be FAILED
4. WHEN a Task is not executed due to upstream failure or cancellation THEN the Task status SHALL be SKIPPED
5. THE Engine SHALL maintain a TaskReport for each Task containing TaskID, Status, and Error
6. WHEN execution completes THEN the ExecutionResult SHALL contain a map of all TaskReports indexed by Task ID

### Requirement 14

**User Story:** As a developer, I want to retrieve execution results, so that I can access task outputs and understand execution outcomes.

#### Acceptance Criteria

1. WHEN execution completes THEN the ExecutionResult SHALL contain an ExecutionID
2. WHEN all tasks succeed THEN the ExecutionResult Success field SHALL be true
3. WHEN any task fails or is skipped THEN the ExecutionResult Success field SHALL be false
4. THE ExecutionResult SHALL contain a Reports map with TaskReport entries for all tasks
5. THE ExecutionResult SHALL contain a reference to the Datastore with all task results
6. THE ExecutionResult SHALL provide a method to retrieve a specific task's output by Task ID

### Requirement 15

**User Story:** As a developer, I want to configure task-specific timeouts, so that I can control execution time limits for individual tasks.

#### Acceptance Criteria

1. WHEN a Task has a Timeout field set THEN the Engine SHALL use that timeout for the task's context
2. WHEN a Task has no Timeout field set THEN the Engine SHALL use the default task timeout from Engine options
3. WHEN a task's timeout expires THEN the task's context SHALL be cancelled with a deadline exceeded error
4. WHEN a task times out THEN the task status SHALL be FAILED with a timeout error
5. WHEN a task times out in Fail-Fast mode THEN the execution SHALL cancel as with any other task failure

### Requirement 16

**User Story:** As a developer, I want to integrate custom logging, so that I can use my preferred logging framework and format.

#### Acceptance Criteria

1. WHEN an Engine is created with a Logger option THEN the Engine SHALL use the provided Logger implementation
2. THE Logger interface SHALL define methods for Info and Error logging with context and structured fields
3. THE Logger interface SHALL define a With method that returns a new Logger with additional fields
4. WHEN a Task Context is created THEN the Engine SHALL create a child Logger with Task ID and ExecutionID fields
5. WHEN middleware or handlers log messages THEN the logs SHALL include the Task ID and ExecutionID automatically

### Requirement 17

**User Story:** As a developer, I want the Engine to support non-connected DAGs, so that I can execute multiple independent task workflows in a single execution.

#### Acceptance Criteria

1. WHEN the DAG contains multiple tasks with zero indegree THEN the Engine SHALL identify all such tasks as starting points
2. WHEN execution begins THEN the Engine SHALL schedule all zero-indegree tasks concurrently
3. WHEN the DAG contains disconnected subgraphs THEN the Engine SHALL execute all subgraphs in the same execution
4. WHEN one subgraph fails in Fail-Fast mode THEN the Engine SHALL cancel all subgraphs
5. THE Engine SHALL complete execution when all tasks in all subgraphs reach terminal states
