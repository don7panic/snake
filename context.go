package snake

import (
	"context"
	"time"
)

// HandlerFunc is the unified function signature for both middleware and task handlers
type HandlerFunc func(c context.Context, ctx *Context) error

// Context represents a task-level execution context
type Context struct {
	taskID      string
	executionID string
	StartTime   time.Time

	store    Datastore
	handlers []HandlerFunc
	index    int

	// input holds the execution-specific input parameter
	input any

	// logger provides structured logging capability
	logger Logger
}

// Logger returns the logger associated with this context
func (ctx *Context) Logger() Logger {
	if ctx.logger == nil {
		return NewDefaultLogger()
	}
	return ctx.logger
}

// Next advances to the next handler in the middleware chain
// This method is used by middleware to invoke the next handler
func (ctx *Context) Next(c context.Context) error {
	ctx.index++
	if ctx.index < len(ctx.handlers) {
		// Check for context cancellation before executing the next handler
		if err := c.Err(); err != nil {
			return err
		}
		return ctx.handlers[ctx.index](c, ctx)
	}
	return nil
}

// SetResult writes a value to the Datastore using the key
func (ctx *Context) SetResult(key string, value any) {
	ctx.store.Set(key, value)
}

// GetResult retrieves a value from the Datastore by key
// Returns the value and true if found, nil and false if not found
func (ctx *Context) GetResult(key string) (any, bool) {
	return ctx.store.Get(key)
}

// TaskID returns the ID of the current task
func (ctx *Context) TaskID() string {
	return ctx.taskID
}

// Input returns the execution input parameter
// Users should perform type assertion to convert to the expected type
func (ctx *Context) Input() any {
	return ctx.input
}

// SetTyped writes a strongly-typed value to the Datastore
func SetTyped[T any](ctx *Context, key Key[T], value T) {
	ctx.store.Set(key.Name(), value)
}

// GetTyped retrieves a strongly-typed value from the Datastore
// It ensures that the retrieved value matches the type T
func GetTyped[T any](ctx *Context, key Key[T]) (T, bool) {
	val, ok := ctx.store.Get(key.Name())
	if !ok {
		var zero T
		return zero, false
	}
	typedVal, ok := val.(T)
	return typedVal, ok
}
