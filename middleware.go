package snake

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
)

// Recovery returns a middleware that recovers from any panics and writes a 500 if there was one.
func Recovery() HandlerFunc {
	return func(c context.Context, ctx *Context) (err error) {
		// Defer a function to catch any panics
		defer func() {
			if r := recover(); r != nil {
				// Get the stack trace
				stackTrace := debug.Stack()
				log.Printf("Panic recovered: %v\n%s", r, string(stackTrace))
				err = fmt.Errorf("task %s panic: %v", ctx.taskID, r)
			}
		}()

		// Call the next handler in the chain
		err = ctx.Next(c)
		return err
	}
}
