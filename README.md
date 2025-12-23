# snake

Lightweight in-process DAG orchestrator for Go. Declare explicit dependencies, share data across tasks, run work in parallel, and extend execution with pluggable middleware (recovery, tracing, logging, metrics, and more).

```bash
go get github.com/don7panic/snake
```

For the Chinese version, see `README.zh.md`.

## Why snake

- Explicit dependency graph: declare upstream tasks by `TaskID`; the engine builds a DAG and schedules tasks in topo order with parallel fan-out.
- Built-in datastore: concurrent-safe `ctx.SetResult`/`ctx.GetResult` for passing results; keys can be task IDs or custom strings to support multiple outputs.
- Middleware chain: gin-style `HandlerFunc` chain with global and task-level middleware.
- Error strategies: choose between `FailFast` (default) or `RunAll` (execute all independent tasks).
- Failure tolerance: per-task `WithAllowFailure(true)` to skip global cancellation on specific task failures.
- Timeouts: global timeout (`WithGlobalTimeout`) plus default/task-level timeouts.
- Debug-friendly outputs: `ExecutionResult` exposes per-task reports and the exact `TopoOrder`.
- Reusable engine: build the DAG once, then execute it multiple times (even concurrently) with different inputs.

## Quick start

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/don7panic/snake"
)

// Input passed to a single Execute call
type WorkflowInput struct {
    Value string
}

func main() {
    engine := snake.NewEngine(
        snake.WithGlobalTimeout(30*time.Second),
        snake.WithDefaultTaskTimeout(2*time.Second),
    )

    // Global middleware for logging/tracing/recovery
    engine.Use(func(c context.Context, ctx *snake.Context) error {
        fmt.Println("start", ctx.TaskID())
        err := ctx.Next(c)
        fmt.Println("done", ctx.TaskID(), "err:", err)
        return err
    })

    a := snake.NewTask("A", func(c context.Context, ctx *snake.Context) error {
        input, ok := ctx.Input().(*WorkflowInput)
        if !ok {
            return fmt.Errorf("invalid input type")
        }

        ctx.SetResult("A", fmt.Sprintf("processed: %s", input.Value))
        return nil
    })

	b := snake.NewTask("B", func(c context.Context, ctx *snake.Context) error {
		v, _ := ctx.GetResult("A") // read upstream result
		ctx.SetResult("B", fmt.Sprintf("b got %v", v))
		return nil
	}, snake.WithDependsOn(a))

    if err := engine.Register(a, b); err != nil {
        panic(err)
    }

    // Validate DAG: checks missing deps and cycles
    if err := engine.Build(); err != nil {
        panic(err)
    }

    input := &WorkflowInput{Value: "hello"}
    result, err := engine.Execute(context.Background(), input)
    if err != nil {
        panic(err)
    }

    fmt.Println("success:", result.Success)
    fmt.Println("topo:", result.TopoOrder) // e.g. [A B]

    if v, ok := result.GetResult("B"); ok {
        fmt.Println("B result:", v)
    }

    // Reuse the same engine with new inputs
    input2 := &WorkflowInput{Value: "world"}
    result2, err := engine.Execute(context.Background(), input2)
    if err != nil {
        panic(err)
    }
    fmt.Println("second execution success:", result2.Success)
}
```

## Core concepts

- `Task`: handler signature `func(c context.Context, ctx *snake.Context) error`. Use `ctx.Next(c)` to step through middleware; place the business handler at the end of the chain.
- `Context`: exposes the execution input (`ctx.Input()`), datastore helpers (`SetResult`/`GetResult`), and task metadata (`TaskID`, deadlines, logger).
- `Datastore`: each `Execute` call gets a fresh store; customize via `WithDatastoreFactory` if you need a different backend.
- `Timeouts`: set defaults with `WithDefaultTaskTimeout`, override per task with `WithTimeout(d)`.
- `Error Handling`: `WithErrorStrategy(snake.FailFast)` (stop on first error) or `snake.RunAll` (run disjoint paths).
- `AllowFailure`: use `WithAllowFailure(true)` for non-critical tasks that shouldn't stop the workflow.
- `Fail-fast`: under FailFast strategy, the first failure cancels remaining work; unstarted tasks become `CANCELLED`.

## Advanced Features

### Conditional Execution

Tasks can be skipped based on runtime conditions using `WithCondition`.

```go
check := snake.NewTask("check", func(c context.Context, ctx *snake.Context) error {
    // ... logic ...
    return nil
}, snake.WithCondition(func(c context.Context, ctx *snake.Context) bool {
    // Return true to run the task, false to skip it
    return shouldRun()
}))
```

If a task is skipped, its status becomes `SKIPPED`. Dependent tasks will still run unless they depend on the skipped task's output (logic for this is determined by your application, but generally the engine propagates execution to dependents).

### Concurrency Control

Limit the number of parallel tasks using `WithMaxConcurrency`.

```go
engine := snake.NewEngine(
    snake.WithMaxConcurrency(5), // Run at most 5 tasks in parallel
)
```

### Middleware & Recovery

Snake supports middleware chains similar to web frameworks. A common use case is panic recovery.

```go
// Add recovery middleware to handle panics in tasks safely
engine.Use(snake.Recovery())
```

### Task Options

Full list of options for `NewTask`:

- `WithDependsOn(tasks ...*Task)`: Declare upstream dependencies.
- `WithTimeout(d time.Duration)`: Set a hard timeout for this task.
- `WithCondition(fn ConditionFunc)`: dynamic skipping logic.
- `WithAllowFailure(allow bool)`: If true, failure won't cancel the entire execution (under FailFast).
- `WithMiddlewares(m ...HandlerFunc)`: Add task-specific middleware.


## Observability and debugging

- `ExecutionResult.Reports` includes per-task status, error, and duration.
- `ExecutionResult.TopoOrder` shows the actual execution order for quick inspection.
- Extend logging/tracing/metrics/rate limiting through middleware.

## Development

```bash
# Run tests
go test ./...

# Coverage
go test -v -cover ./...

# Format code
go fmt ./...

# Static checks
go vet ./...
```

## Examples

See `examples/` for runnable samples:
- `examples/recovery_example.go` - recovery middleware
- `examples/custom_store_test.go` - custom datastore
- `examples/e2e/order_workflow.go` - end-to-end order flow

## Architecture

High-level design notes live in `docs/ARCHITECTURE.md`.

## Contributing

Issues and pull requests are welcome.

## License

MIT License
