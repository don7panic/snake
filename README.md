# snake

A lightweight, in-process DAG (Directed Acyclic Graph) task orchestrator for Go.


Snake allows you to define tasks with explicit dependencies, share data type-safely, and execute them in parallel. It follows a "code-as-configuration" approach and provides a Gin-like middleware system for easy extensibility.

[中文版](./README_CN.md) | [English](./README.md)


## Features

- **explicit Dependency Management**: Declare dependencies using Task IDs. The engine automatically builds the DAG and schedules tasks.
- **Parallel Execution**: Independent tasks run concurrently. Contains a built-in worker pool and event-driven scheduler.
- **Type-Safe Data Sharing**: Use generic `Key[T]` helpers to pass data between tasks without messy type assertions.
- **Middleware Support**: Extend functionality with global or task-level middleware (logging, tracing, recovery, etc.).
- **Context Propagation**: Each task gets a unified context carrying input, storage access, logger, and cancellation signals.
- **Flexible Error Handling**: Choose between `FailFast` (default) or `RunAll` strategies. Support for non-critical tasks via `AllowFailure`.

## Installation

```bash
go get github.com/don7panic/snake
```

## Quick Start

Here is a complete example showing how to define tasks, share data safely, and execute a workflow.

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/don7panic/snake"
)

// 1. Define typed keys for data sharing
var (
	KeyUserID   = snake.NewKey[int]("user_id")
	KeyUserName = snake.NewKey[string]("user_name")
)

type UserInput struct {
	ID int
}

func main() {
	// 2. Initialize Engine
	engine := snake.NewEngine(
		snake.WithLogger(snake.NewDefaultLogger()),
		snake.WithMaxConcurrency(5),
	)

	// 3. Define Tasks
	
	// Task A: Fetch user data
	taskFetch := snake.NewTask("fetch_user", func(c context.Context, ctx *snake.Context) error {
		input := ctx.Input().(*UserInput) // Get execution input
		
		// Simulate data fetching
		time.Sleep(100 * time.Millisecond)
		name := fmt.Sprintf("User-%d", input.ID)
		
		// Save result type-safely
		snake.SetTyped(ctx, KeyUserName, name)
		
		ctx.Logger().Info(c, "fetched user", snake.Field{Key: "name", Value: name})
		return nil
	})

	// Task B: Process user data (depends on A)
	taskProcess := snake.NewTask("process_user", func(c context.Context, ctx *snake.Context) error {
		// Read upstream result type-safely
		name, ok := snake.GetTyped(ctx, KeyUserName)
		if !ok {
			return fmt.Errorf("user name not found")
		}
		
		fmt.Printf("Processing user: %s\n", name)
		return nil
	}, snake.WithDependsOn(taskFetch)) // Declare dependency

	// 4. Register Tasks
	if err := engine.Register(taskFetch, taskProcess); err != nil {
		panic(err)
	}

	// 5. Execute
	// You can reuse the engine for distinct executions with different inputs
	input := &UserInput{ID: 42}
	result, err := engine.Execute(context.Background(), input)
	
	if err != nil {
		fmt.Printf("Execution failed: %v\n", err)
	} else {
		fmt.Printf("Success! Order: %v\n", result.TopoOrder)
	}
}
```

## Core Concepts

### Engine
The `Engine` is the central coordinator. It validates the DAG and manages the worker pool. It is thread-safe and designed to be built once and executed multiple times.

### Context
Each task execution receives a `*snake.Context`. It wraps the standard `context.Context` and provides:
- **Input**: Access the input data passed to `Execute`.
- **Store**: Read/Write access to the execution-scoped Datastore.
- **Logger**: A structured logger.
- **Next**: Logic to advance middleware chain.

### Type-Safe Data Sharing
Instead of using raw strings keys and `interface{}` values, use `snake.Key[T]`:

```go
var KeyCount = snake.NewKey[int]("count")

// Write
snake.SetTyped(ctx, KeyCount, 100)

// Read
count, ok := snake.GetTyped(ctx, KeyCount) // count is int
```

## Advanced Usage

### Middleware
Middleware allows you to wrap task execution with common logic.

```go
// Add panic recovery middleware
engine.Use(snake.Recovery())

// Custom middleware
engine.Use(func(c context.Context, ctx *snake.Context) error {
    start := time.Now()
    defer func() {
        fmt.Printf("Task %s took %v\n", ctx.TaskID(), time.Since(start))
    }()
    return ctx.Next(c)
})
```

### Conditional Execution
Skip tasks dynamically at runtime:

```go
task := snake.NewTask("optional_task", handler, 
    snake.WithCondition(func(c context.Context, ctx *snake.Context) bool {
        // Return false to skip this task
        return shouldRun()
    }),
)
```

## License

MIT
