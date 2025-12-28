# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

snake is a lightweight in-process DAG (Directed Acyclic Graph) orchestrator for Go. It allows you to declare explicit task dependencies, share data across tasks, run work in parallel, and extend execution with pluggable middleware.

## Core Architecture

The codebase follows a modular architecture with these key components:

- **Engine**: Central orchestrator that manages task registry, DAG construction/validation, and parallel execution
- **Task**: Self-contained execution units with dependencies, handlers, middlewares, and timeouts
- **Context**: Task-level execution context providing datastore access, logging, and middleware chain execution
- **Datastore**: Concurrent-safe key-value store for passing results between tasks

## Development Commands

### Building and Testing
```bash
# Build the project
go build -v ./...

# Run all tests
go test -v ./...

# Run tests with race detector
go test -race ./...

# Run tests with coverage
go test -v -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### Code Quality
```bash
# Format code
go fmt ./...

# Check formatting without changes
make fmt-check

# Run go vet
go vet ./...

# Run static analysis
make lint

# Run all checks
make check
```

### Using Makefile
```bash
# Default target (fmt, vet, test, build)
make

# Install dependencies
make deps

# Clean build artifacts
make clean
```

## Key Design Patterns

### Engine Usage Pattern
1. Create Engine with options (`NewEngine`)
2. Register Tasks with dependencies (`Register`)
3. Build and validate DAG (`Build`)
4. Execute with input parameters (`Execute`)

Engine supports reuse - build once, execute multiple times with different inputs.

### Data Flow
- Tasks declare dependencies through `WithDependsOn`
- Data flows through `Datastore` using `SetResult`/`GetResult`
- Input parameters are injected at execution time via `Context.Input()`

### Middleware System
- Global middleware applied to all tasks via `engine.Use()`
- Task-specific middleware via `WithMiddlewares()`
- Chain execution managed through `ctx.Next()` pattern

## Error Handling Strategies

- **FailFast** (default): Stop execution immediately on first task failure
- **RunAll**: Attempt to execute all independent paths even if some tasks fail

## Testing Approach

The test suite is comprehensive and covers:
- DAG execution with various dependency patterns
- Concurrent execution scenarios
- Error handling strategies
- Middleware functionality
- Datastore operations
- Timeout and cancellation behavior

Run specific test files with: `go test -v ./engine_test.go`

## Important Implementation Details

- Tasks are identified by unique string IDs
- Datastore uses `map[string]any` with `sync.RWMutex` for thread safety
- Execution results include per-task reports and topological order
- Support for conditional execution via `WithCondition`
- Concurrency control via `WithMaxConcurrency`

## File Organization

- Core components in root: `engine.go`, `task.go`, `context.go`, `datastore.go`, `types.go`
- Tests follow Go conventions with `_test.go` suffix
- Examples in `examples/` directory
- Architecture documentation in `docs/ARCHITECTURE.md`

When modifying the codebase, ensure:
- All tests continue to pass
- New functionality includes appropriate tests
- Code follows existing patterns for Task creation and Engine configuration
- Concurrency safety is maintained in Datastore operations