# Snake Architecture Reference

## Overview

**Snake** is a lightweight, in-process task orchestration framework for Go. It allows developers to model complex business logic as a **Directed Acyclic Graph (DAG)** of tasks. Snake handles dependency resolution, parallel execution, data propagation, and lifecycle management, while allowing developers to focus on the pure business logic of individual tasks.

Key features include:
*   **Explicit Dependency Modeling**: Define tasks and their dependencies; the engine resolves the execution order.
*   **Parallel Execution**: Independent tasks run concurrently, managed by a worker pool.
*   **Data Flow**: Automated data passing between tasks via a thread-safe `Datastore`.
*   **Pluggable Middleware**: A Gin-like middleware ecosystem for cross-cutting concerns (logging, tracing, metrics).
*   **Type-Safe Context**: Contextual access to input, execution scope, and shared data.

## Core Concepts

### Engine
The `Engine` is the central control unit. It is responsible for:
*   **Graph Management**: Registering tasks and building the DAG.
*   **Validation**: Detecting cycles and missing dependencies during the `Build()` phase.
*   **Execution**: Scheduling tasks and managing the worker pool during `Execute()`.

The Engine is designed to be **built once and executed multiple times**. Each `Execute()` call creates a completely isolated execution scope with its own `context`, `Datastore`, and input data.

### Task
A `Task` is the smallest unit of work. It contains:
*   **ID**: Unique identifier.
*   **Handler**: The business logic function.
*   **Dependencies**: List of other Task IDs that must complete successfully before this task triggers.
*   **Configuration**: Timeouts, specific middleware, and execution conditions (`ConditionFunc`).
*   **Failure Policy**: `AllowFailure` flag to determine if a task's failure should abort the entire workflow.

### Context
The `Context` bridges the engine and the user's handler. It wraps the standard `std/context` and provides:
*   **Data Access**: Methods to get/set data in the `Datastore`.
*   **Input Access**: Access to the initial input parameters (`ctx.Input()`).
*   **Control Flow**: `Next()` for middleware chaining.
*   **Metadata**: Task ID, Execution ID, startTime, etc.

### Datastore
The `Datastore` serves as the shared memory for an execution lifecycle. It allows:
*   **Write-Once-Read-Many**: Tasks write their results, which downstream dependents read.
*   **Type Safety**: Helper generics (`SetTyped[T]`, `GetTyped[T]`) reduce runtime type assertion errors.
*   **Isolation**: Every `Execute()` call instantiates a fresh Datastore.

## Execution Architecture

Snake employs an event-driven, worker-pool based architecture to maximize performance and minimize lock contention.

### The Coordinator Loop
Instead of guarding the graph state with heavy mutexes during execution, Snake uses a single **Coordinator Loop** (running in the main goroutine of the `Execute` call) to manage the entire state machine.

*   **Responsibility**:
    *   Tracks task completion events from workers.
    *   Updates dependency counters (indegree) for dependent tasks.
    *   Decides which tasks are ready to run.
    *   Handles "Fast-Fail" logic and error propagation.
    *   Detects global timeouts or cancellations.

### Worker Pool
*   **Concurrency Control**: Configurable `MaxConcurrency` limits the number of active goroutines.
*   **Mechanism**: A pool of workers consumes task IDs from a `ReadyQueue` channel.
*   **Isolation**: Each worker picks a task, creates a task-specific `Context` (with derived timeout), executes the middleware chain + handler, and sends the result back to the Coordinator via a `Results` channel.

### Execution Flow

1.  **Initialization**:
    *   Engine creates a snapshot of the DAG (adjacency list, initial indegrees).
    *   `ReadyQueue` is populated with root tasks (indegree 0).
    *   Workers are started.

2.  **Loop**:
    *   Coordinator waits for signals from `Results` channel or `Context.Done`.
    *   **On Task Completion**:
        *   Mark task as Success/Failed.
        *   If Success: Decrement indegree for all children. If a child's indegree hits 0, push to `ReadyQueue`.
        *   If Failed: If `FailFast` strategy is active, cancel all workers and return error.

3.  **Termination**:
    *   Execution finishes when all tasks are processed or a terminal error occurs.
    *   Returns an `ExecutionResult` containing reports for all tasks.


## Middleware Pipeline

Snake adopts the "onion model" for middleware, similar to modern web frameworks.

```
Global Middleware 1 (Start)
  -> Global Middleware 2 (Start)
    -> Task Middleware A (Start)
       -> TASK HANDLER BUSINESS LOGIC
    <- Task Middleware A (End)
  <- Global Middleware 2 (End)
<- Global Middleware 1 (End)
```

Usage:
*   **Global**: `engine.Use(LoggingMiddleware, RecoveryMiddleware)`
*   **Task-Specific**: `NewTask(..., WithMiddlewares(RateLimitMiddleware))`


## Error Handling & Conditional Execution

### Strategies
*   **FailFast (Default)**: The first task error cancels the context of all running tasks and aborts the workflow.
*   **RunAll**: The engine attempts to run all tasks that are not dependent on the failed task.

### Conditional Tasks
Tasks can define a `ConditionFunc`. If this function returns `false` during execution time, the task is marked as `SKIPPED`.
*   Skipped tasks are treated as "completed" for dependency resolution, but they do not produce output data.
*   Downstream tasks will still run unless they explicitly check for the upstream's specific output.


## Data Input & Output

### Input Injection
To separate DAG construction from runtime data, Snake injects input at `Execute()` time.

```go
// 1. Build Phase
engine.Register(task1, task2)
engine.Build()

// 2. Execution Phase
engine.Execute(ctx, &MyRequest{ID: 123})
```

Handlers access this via `ctx.Input()`.

### Output Management
Tasks write results to the Datastore. The `ExecutionResult` returned by `Execute` contains the final state of the Datastore, allowing the caller to retrieve any task's output.

```go
result, _ := engine.Execute(...)
val, _ := result.Store.Get("task_id")
```
