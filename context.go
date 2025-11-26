package snake

import (
	"context"
	"time"
)

// Context represents a task-level execution context
type Context struct {
	ctx context.Context

	TaskID      string
	ExecutionID string
	StartTime   time.Time

	Store  Datastore
	Logger Logger

	handlers []HandlerFunc
	index    int
}

// Next advances to the next handler in the middleware chain
// This method is used by middleware to invoke the next handler
func (c *Context) Next() error {
	c.index++
	if c.index < len(c.handlers) {
		return c.handlers[c.index](c)
	}
	return nil
}

// SetResult writes a value to the Datastore using the task's ID as the key
func (c *Context) SetResult(value any) {
	c.Store.Set(c.TaskID, value)
}

// GetResult retrieves a value from the Datastore by task ID
// Returns the value and true if found, nil and false if not found
func (c *Context) GetResult(taskID string) (any, bool) {
	return c.Store.Get(taskID)
}

// Context returns the underlying context.Context for cancellation and timeout handling
func (c *Context) Context() context.Context {
	return c.ctx
}
