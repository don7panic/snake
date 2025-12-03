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
}

// Next advances to the next handler in the middleware chain
// This method is used by middleware to invoke the next handler
func (ctx *Context) Next(c context.Context) error {
	ctx.index++
	if ctx.index < len(ctx.handlers) {
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
