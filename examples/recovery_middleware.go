package examples

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"

	"snake"
)

// RecoveryMiddleware is a middleware that catches panics in task handlers
// and converts them to errors, preventing the entire execution from crashing.
//
// When a panic occurs:
// - The panic is caught and converted to an error
// - The error is logged with stack trace details
// - The task is marked as FAILED
// - In Fail-Fast mode, the execution is cancelled as with any other task failure
//
// Usage:
//
//	engine := snake.NewEngine()
//	engine.Use(examples.RecoveryMiddleware())
//
// Or for a specific task:
//
//	task := snake.NewTask("my-task", handler,
//	    snake.WithMiddlewares(examples.RecoveryMiddleware()))
func RecoveryMiddleware() snake.HandlerFunc {
	return func(c context.Context, ctx *snake.Context) (err error) {
		// Defer a function to catch any panics
		defer func() {
			if r := recover(); r != nil {
				// Get the stack trace
				stackTrace := debug.Stack()
				_ = stackTrace // Suppress unused variable warning

				// Convert panic to error
				switch v := r.(type) {
				case error:
					err = fmt.Errorf("panic recovered: %w", v)
				case string:
					err = fmt.Errorf("panic recovered: %s", v)
				default:
					err = fmt.Errorf("panic recovered: %v", r)
				}

				// Log the panic details with stack trace
				// In production, you could implement a different logging strategy
				// such as using a context-based logger, global logger, or dependency injection
				log.Printf("Panic recovered: %v\n%s", err, string(stackTrace))
			}
		}()

		// Call the next handler in the chain
		err = ctx.Next(c)
		return err
	}
}
