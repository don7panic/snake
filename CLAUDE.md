# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

### Common Development Tasks
- **Run tests**: `make test` or `go test -v ./...`
- **Run single test**: `go test -v -run TestFunctionName ./...`
- **Build project**: `make build` or `go build -v ./...`
- **Format code**: `make fmt` or `go fmt ./...`
- **Lint code**: `make lint` (requires staticcheck)
- **Run all checks**: `make check` (runs fmt, vet, lint, test)
- **Test with coverage**: `make test-coverage`
- **Clean artifacts**: `make clean`

### Dependencies
- **Install/update dependencies**: `make deps`

## Architecture Overview

Snake is a Go-based DAG (Directed Acyclic Graph) execution engine for orchestrating parallel task execution with dependency management.

### Core Components
- **Engine**: Central orchestrator that manages task registry, DAG construction/validation, and parallel execution
- **Task**: Self-contained execution units with dependencies, handlers, middlewares, and timeouts
- **Context**: Task-level execution context providing datastore access, logging, and middleware chain execution
- **Datastore**: Thread-safe key-value storage for inter-task communication (isolated per execution)
- **Logger**: Structured logging interface with task-specific metadata

### Key Architecture Patterns

**Task Creation Pattern**:
```go
task := NewTask("taskID", handlerFunc,
    WithDependsOn("dependency1", "dependency2"),
    WithTimeout(30*time.Second),
    WithMiddlewares(taskMiddleware...)
)
```

**Engine Configuration Pattern**:
```go
engine := NewEngine(
    WithFailFast(),
    WithDefaultTaskTimeout(10*time.Second),
    WithLogger(customLogger),
)
```

**Registration and Execution Flow**:
1. `engine.Register(task)` - Validate and add tasks to registry
2. `engine.Build()` - Build DAG structure and validate no cycles
3. `engine.Execute(ctx)` - Execute with fresh datastore and unique execution ID

### Concurrency and Execution Model
- Uses Kahn's algorithm for cycle detection and topological sorting
- Atomic counters track pending dependencies during parallel execution
- Channel-based ready queue for task scheduling
- Tasks become ready when all dependencies complete (pending count reaches zero)
- Supports disconnected DAGs and Fail-Fast error strategy

### Middleware Chain
- Global middleware via `engine.Use(middleware...)`
- Task-specific middleware via `WithMiddlewares(...)`
- Execution order: Global → Task-specific → Handler
- Use `ctx.Next()` to progress through middleware chain

### Data Flow
- Tasks communicate via the datastore using task IDs as keys
- `ctx.SetResult(value)` - Store task output
- `ctx.GetResult(taskID)` - Retrieve dependency outputs
- Results also available via `ExecutionResult.GetResult(taskID)` after execution

## Testing Organization

Tests are organized by component:
- `engine_test.go` - Core engine functionality and execution lifecycle
- `context_test.go` - Context behavior and middleware chain execution
- `parallel_test.go` - Parallel execution scenarios and concurrency safety
- `disconnected_test.go` - Non-connected DAG handling

Key testing patterns include execution order verification, dependency chain validation, and goroutine synchronization for concurrent execution testing.

## Important Notes

- All task dependencies must be registered before calling `engine.Build()`
- Execution results are isolated per run with fresh datastore instances
- Timeout of zero means no timeout (uses configured default)
- Task IDs must be unique across the entire engine registry