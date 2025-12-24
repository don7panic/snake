package snake

import (
	"context"
	"fmt"
	"runtime/debug"
)

// Recovery returns a middleware that recovers from any panics.
func Recovery() HandlerFunc {
	return func(c context.Context, ctx *Context) (err error) {
		defer func() {
			if r := recover(); r != nil {
				// Get the stack trace
				stackTrace := debug.Stack()

				// Log the panic using the context logger
				// We include the stack trace as a field
				ctx.Logger().Error(c, "task_panic_recovered", fmt.Errorf("%v", r),
					Field{Key: "task_id", Value: ctx.taskID},
					Field{Key: "stack_trace", Value: string(stackTrace)},
				)

				err = fmt.Errorf("task %s panic: %v", ctx.taskID, r)
			}
		}()

		// Call the next handler in the chain
		err = ctx.Next(c)
		return err
	}
}
