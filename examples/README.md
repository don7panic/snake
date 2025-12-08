# Snake Examples

This directory contains example middleware implementations and usage demonstrations for the snake task orchestration framework.

## End-to-End, Build-Once Workflow

The `e2e` example (`examples/e2e`) shows how to:

- Build a complex DAG once at service startup (order validation → profile lookup → inventory reservation → pricing → payment → persistence → notifications → response).
- Reuse the built engine for every request by only calling `Execute` with request-scoped context.
- Keep request data in `context.Context` while letting tasks exchange intermediate results through the shared datastore.
- Add lightweight middleware to trace task timing without re-instantiating the engine.

Quick usage:

```go
import (
    "context"
    "fmt"

    "github.com/don7panic/snake/examples/e2e"
)

func main() {
    // Build once during bootstrap (thread-safe and cached).
    if _, err := e2e.EnsureOrderEngine(); err != nil {
        panic(err)
    }

    // Per-request execution reuses the same engine.
    resp, result, err := e2e.ExecuteOrder(context.Background(), e2e.OrderRequest{
        RequestID: "req-001",
        UserID:    "vip-42",
        Items: []e2e.OrderItem{
            {SKU: "sku-1", Quantity: 2, UnitPrice: 99},
        },
        CouponCode: "new-user",
    })
    if err != nil {
        panic(err)
    }

    fmt.Println("order:", resp.OrderID, "payment:", resp.PaymentID, "reservation:", resp.ReservationID)
    fmt.Println("topo order:", result.TopoOrder)
}
```

## Recovery Middleware

The recovery middleware catches panics in task handlers and converts them to errors, preventing the entire execution from crashing.

### Features

- **Panic Recovery**: Catches panics of any type (string, error, int, struct, etc.)
- **Error Conversion**: Converts panics to proper error values
- **Stack Trace Logging**: Logs panic details with full stack traces
- **Fail-Fast Integration**: Works seamlessly with Fail-Fast error handling mode
- **Flexible Application**: Can be applied globally or to specific tasks

### Usage

#### Global Application

Apply recovery middleware to all tasks in the engine:

```go
engine := snake.NewEngine()
engine.Use(examples.RecoveryMiddleware())

// All tasks will now have panic recovery
task := snake.NewTask("my-task", func(c context.Context, ctx *snake.Context) error {
    panic("something went wrong!")
    return nil
})

// Validate DAG
if err := engine.Build(); err != nil {
    panic(err)
}
```

#### Task-Specific Application

Apply recovery middleware to specific tasks only:

```go
engine := snake.NewEngine()

// Only this task has panic recovery
task := snake.NewTask("risky-task", 
    func(c context.Context, ctx *snake.Context) error {
        panic("risky operation failed")
        return nil
    },
    snake.WithMiddlewares(examples.RecoveryMiddleware()),
)

if err := engine.Build(); err != nil {
    panic(err)
}
```

### Behavior

When a panic occurs:

1. The panic is caught by the recovery middleware
2. The panic value is converted to an error with the format: `panic recovered: <value>`
3. The error is logged with:
   - Task ID
   - Execution ID
   - Panic value
   - Full stack trace
4. The task is marked as FAILED
5. In Fail-Fast mode, the execution is cancelled (same as any other task failure)
6. Dependent tasks are marked as CANCELLED

### Examples

See the following files for complete examples:

- `recovery_middleware.go` - The middleware implementation
- `recovery_example.go` - Usage demonstrations
- `recovery_middleware_test.go` - Test cases showing various scenarios

### Running Examples

To run the example demonstrations:

```go
package main

import "github.com/don7panic/snake/examples"

func main() {
    // Demonstrate basic panic recovery
    examples.DemoRecoveryMiddleware()
    
    // Demonstrate task-specific recovery
    examples.DemoTaskSpecificRecovery()
    
    // Demonstrate recovery with timeouts
    examples.DemoRecoveryWithTimeout()
}
```

### Testing

Run the recovery middleware tests:

```bash
go test -v ./examples/... -run TestRecoveryMiddleware
```

### Requirements Satisfied

This implementation satisfies the following requirements:

- **12.1**: Recovery middleware wraps Next() call in panic recovery block
- **12.2**: Panics are caught and converted to errors
- **12.3**: Failed tasks are marked as FAILED with error describing the panic
- **12.4**: Panic details including stack trace are logged
- **12.5**: In Fail-Fast mode, execution cancels as with any other task failure

### Implementation Notes

The recovery middleware uses Go's `defer` and `recover()` mechanism to catch panics. It handles various panic types:

- **string**: `panic("error message")`
- **error**: `panic(fmt.Errorf("error"))`
- **int/other types**: `panic(42)` or `panic(struct{...})`
- **nil**: `panic(nil)`

All panic types are converted to descriptive error messages and logged with full context.
