package examples

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"snake"

	"github.com/stretchr/testify/assert"
)

// TestRecoveryMiddleware_CatchesPanic tests that recovery middleware catches panics
func TestRecoveryMiddleware_CatchesPanic(t *testing.T) {
	engine := snake.NewEngine()
	engine.Use(RecoveryMiddleware())

	// Task that panics with a string
	task := snake.NewTask("panic-task", func(c context.Context, ctx *snake.Context) error {
		panic("test panic")
	})

	assert.NoError(t, engine.Register(task))

	result, err := engine.Execute(context.Background())

	// Execution should complete without error
	assert.NoError(t, err)

	// Task should be marked as FAILED
	report := result.Reports["panic-task"]
	assert.NotNil(t, report)
	assert.Equal(t, snake.TaskStatusFailed, report.Status)

	// Error should contain panic information
	assert.NotNil(t, report.Err)
	assert.Contains(t, report.Err.Error(), "panic recovered")
	assert.Contains(t, report.Err.Error(), "test panic")
}

// TestRecoveryMiddleware_CatchesPanicWithError tests panic with error type
func TestRecoveryMiddleware_CatchesPanicWithError(t *testing.T) {
	engine := snake.NewEngine()
	engine.Use(RecoveryMiddleware())

	originalErr := errors.New("original error")
	task := snake.NewTask("panic-error-task", func(c context.Context, ctx *snake.Context) error {
		panic(originalErr)
	})

	assert.NoError(t, engine.Register(task))

	result, err := engine.Execute(context.Background())

	assert.NoError(t, err)

	report := result.Reports["panic-error-task"]
	assert.Equal(t, snake.TaskStatusFailed, report.Status)
	assert.NotNil(t, report.Err)
	assert.Contains(t, report.Err.Error(), "panic recovered")
}

// TestRecoveryMiddleware_FailFastOnPanic tests that panic triggers Fail-Fast
func TestRecoveryMiddleware_FailFastOnPanic(t *testing.T) {
	engine := snake.NewEngine()
	engine.Use(RecoveryMiddleware())

	task1 := snake.NewTask("task1", func(c context.Context, ctx *snake.Context) error {
		return nil
	})

	task2 := snake.NewTask("task2", func(c context.Context, ctx *snake.Context) error {
		panic("task2 panic")
	}, snake.WithDependsOn("task1"))

	task3 := snake.NewTask("task3", func(c context.Context, ctx *snake.Context) error {
		return nil
	}, snake.WithDependsOn("task2"))

	assert.NoError(t, engine.Register(task1, task2, task3))

	result, err := engine.Execute(context.Background())

	assert.NoError(t, err)
	assert.False(t, result.Success)

	// Task1 should succeed
	assert.Equal(t, snake.TaskStatusSuccess, result.Reports["task1"].Status)

	// Task2 should fail due to panic
	assert.Equal(t, snake.TaskStatusFailed, result.Reports["task2"].Status)
	assert.NotNil(t, result.Reports["task2"].Err)

	// Task3 should be skipped due to Fail-Fast
	assert.Equal(t, snake.TaskStatusSkipped, result.Reports["task3"].Status)
}

// TestRecoveryMiddleware_TaskSpecific tests recovery middleware on specific tasks
func TestRecoveryMiddleware_TaskSpecific(t *testing.T) {
	engine := snake.NewEngine()

	// Task without recovery middleware
	task1 := snake.NewTask("safe-task", func(c context.Context, ctx *snake.Context) error {
		return nil
	})

	// Task with recovery middleware
	task2 := snake.NewTask("risky-task", func(c context.Context, ctx *snake.Context) error {
		panic("risky panic")
	},
		snake.WithDependsOn("safe-task"),
		snake.WithMiddlewares(RecoveryMiddleware()),
	)

	assert.NoError(t, engine.Register(task1, task2))

	result, err := engine.Execute(context.Background())

	assert.NoError(t, err)

	// Task1 should succeed
	assert.Equal(t, snake.TaskStatusSuccess, result.Reports["safe-task"].Status)

	// Task2 should fail but panic should be recovered
	assert.Equal(t, snake.TaskStatusFailed, result.Reports["risky-task"].Status)
	assert.NotNil(t, result.Reports["risky-task"].Err)
	assert.Contains(t, result.Reports["risky-task"].Err.Error(), "panic recovered")
}

// TestRecoveryMiddleware_NormalErrorPassthrough tests that normal errors
// are not affected by recovery middleware
func TestRecoveryMiddleware_NormalErrorPassthrough(t *testing.T) {
	engine := snake.NewEngine()
	engine.Use(RecoveryMiddleware())

	normalErr := errors.New("normal error")
	task := snake.NewTask("error-task", func(c context.Context, ctx *snake.Context) error {
		return normalErr
	})

	assert.NoError(t, engine.Register(task))

	result, err := engine.Execute(context.Background())

	assert.NoError(t, err)

	report := result.Reports["error-task"]
	assert.Equal(t, snake.TaskStatusFailed, report.Status)
	assert.Equal(t, normalErr, report.Err)
}

// TestRecoveryMiddleware_PanicWithDifferentTypes tests panic with various types
func TestRecoveryMiddleware_PanicWithDifferentTypes(t *testing.T) {
	testCases := []struct {
		name       string
		panicValue interface{}
	}{
		{"string", "panic string"},
		{"int", 42},
		{"struct", struct{ msg string }{"panic struct"}},
		{"nil", nil},
		{"error", fmt.Errorf("panic error")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			engine := snake.NewEngine()
			engine.Use(RecoveryMiddleware())

			task := snake.NewTask("panic-task", func(c context.Context, ctx *snake.Context) error {
				panic(tc.panicValue)
			})

			assert.NoError(t, engine.Register(task))

			result, err := engine.Execute(context.Background())

			assert.NoError(t, err)
			report := result.Reports["panic-task"]
			assert.Equal(t, snake.TaskStatusFailed, report.Status)
			assert.NotNil(t, report.Err)
			assert.Contains(t, report.Err.Error(), "panic recovered")
		})
	}
}

// TestRecoveryMiddleware_MiddlewareChain tests recovery middleware in a chain
func TestRecoveryMiddleware_MiddlewareChain(t *testing.T) {
	engine := snake.NewEngine()

	// Add a logging middleware before recovery
	loggingCalled := false
	loggingMiddleware := func(c context.Context, ctx *snake.Context) error {
		loggingCalled = true
		return ctx.Next(c)
	}

	engine.Use(loggingMiddleware)
	engine.Use(RecoveryMiddleware())

	task := snake.NewTask("panic-task", func(c context.Context, ctx *snake.Context) error {
		panic("test panic in chain")
	})

	assert.NoError(t, engine.Register(task))

	result, err := engine.Execute(context.Background())

	assert.NoError(t, err)
	assert.True(t, loggingCalled, "Logging middleware should be called")

	report := result.Reports["panic-task"]
	assert.Equal(t, snake.TaskStatusFailed, report.Status)
	assert.NotNil(t, report.Err)
}
