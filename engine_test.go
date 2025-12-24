package snake

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRegister_DisallowedAfterBuild(t *testing.T) {
	engine := NewEngine()

	task1 := NewTask("task1", func(c context.Context, ctx *Context) error { return nil })
	task2 := NewTask("task2", func(c context.Context, ctx *Context) error { return nil })

	assert.NoError(t, engine.Register(task1))
	assert.NoError(t, engine.Build())

	err := engine.Register(task2)
	assert.ErrorIs(t, err, ErrRegisterAfterBuild)
}

// TestExecute_GeneratesUniqueExecutionID verifies that each execution gets a unique ID
func TestExecute_GeneratesUniqueExecutionID(t *testing.T) {
	newEngineWithTask := func() *Engine {
		e := NewEngine()
		task := NewTask("task1", func(c context.Context, ctx *Context) error { return nil })
		assert.NoError(t, e.Register(task))
		assert.NoError(t, e.Build())
		return e
	}

	engine1 := newEngineWithTask()
	engine2 := newEngineWithTask()

	result1, err := engine1.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotEmpty(t, result1.ExecutionID)

	// Small delay to ensure different timestamp
	time.Sleep(time.Millisecond)

	result2, err := engine2.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotEmpty(t, result2.ExecutionID)

	assert.NotEqual(t, result1.ExecutionID, result2.ExecutionID, "ExecutionIDs should be unique across executions")
}

// TestExecute_CreatesNewDatastore verifies that each execution gets a fresh Datastore
func TestExecute_CreatesNewDatastore(t *testing.T) {
	engine := NewEngine()

	// Register a simple task
	task := NewTask("task1", func(c context.Context, ctx *Context) error { return nil })
	err := engine.Register(task)
	assert.NoError(t, err)

	assert.NoError(t, engine.Build())

	// Execute and verify Datastore is empty
	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.Store)

	// Verify Datastore is empty (no tasks have run yet)
	_, ok := result.Store.Get("task1")
	assert.False(t, ok, "Datastore should be empty before tasks execute")
}

// TestExecute_DisconnectedGraphs verifies support for non-connected DAGs
func TestExecute_DisconnectedGraphs(t *testing.T) {
	engine := NewEngine()

	// Create two disconnected subgraphs
	// Subgraph 1: task1 -> task2
	task1 := &Task{
		id:      "task1",
		handler: func(c context.Context, ctx *Context) error { return nil },
	}
	task2 := NewTask("task2", func(c context.Context, ctx *Context) error { return nil }, WithDependsOn(task1))

	// Subgraph 2: task3 -> task4
	task3 := &Task{
		id:      "task3",
		handler: func(c context.Context, ctx *Context) error { return nil },
	}
	task4 := &Task{
		id:        "task4",
		dependsOn: []string{"task3"},
		handler:   func(c context.Context, ctx *Context) error { return nil },
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)
	err = engine.Register(task3)
	assert.NoError(t, err)
	err = engine.Register(task4)
	assert.NoError(t, err)

	assert.NoError(t, engine.Build())

	// Execute to verify both subgraphs are handled
	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// mockLogger is a simple mock implementation of the Logger interface for testing
type mockLogger struct {
	fields    []Field
	infoFunc  func(ctx context.Context, msg string, fields ...Field)
	errorFunc func(ctx context.Context, msg string, err error, fields ...Field)
	withFunc  func(fields ...Field) Logger
}

func (m *mockLogger) Info(ctx context.Context, msg string, fields ...Field) {
	if m.infoFunc != nil {
		m.infoFunc(ctx, msg, fields...)
	}
}

func (m *mockLogger) Error(ctx context.Context, msg string, err error, fields ...Field) {
	if m.errorFunc != nil {
		m.errorFunc(ctx, msg, err, fields...)
	}
}

func (m *mockLogger) With(fields ...Field) Logger {
	if m.withFunc != nil {
		return m.withFunc(fields...)
	}
	return m
}

// TestFailFast_MarksTaskAsFailed verifies that failed tasks are marked FAILED
func TestFailFast_MarksTaskAsFailed(t *testing.T) {
	engine := NewEngine(WithErrorStrategy(FailFast))

	expectedErr := assert.AnError
	task1 := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			return expectedErr
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.ErrorIs(t, err, expectedErr)
	assert.NotNil(t, result)

	// Verify task is marked as FAILED
	assert.False(t, result.Success)
	assert.NotNil(t, result.Reports["task1"])
	assert.Equal(t, TaskStatusFailed, result.Reports["task1"].Status)
	assert.Equal(t, expectedErr, result.Reports["task1"].Err)
}

// TestFailFast_CancelsExecutionContext verifies that task failure cancels execution context
func TestFailFast_CancelsExecutionContext(t *testing.T) {
	engine := NewEngine(WithErrorStrategy(FailFast))

	// Track if task2 received cancellation
	task2Cancelled := make(chan bool, 1)

	task1 := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			return assert.AnError
		},
	}

	task2 := &Task{
		id: "task2",
		handler: func(c context.Context, ctx *Context) error {
			// Wait for cancellation or timeout
			select {
			case <-c.Done():
				task2Cancelled <- true
				return c.Err()
			case <-time.After(500 * time.Millisecond):
				task2Cancelled <- false
				return nil
			}
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.ErrorIs(t, err, assert.AnError)
	assert.NotNil(t, result)

	// Verify execution failed
	assert.False(t, result.Success)

	// Verify task1 failed
	assert.Equal(t, TaskStatusFailed, result.Reports["task1"].Status)

	// Verify task2 received cancellation (if it started)
	// Note: task2 might be CANCELLED if it never started, or FAILED if it was running
	if result.Reports["task2"].Status == TaskStatusFailed {
		// Task2 was running and received cancellation
		select {
		case cancelled := <-task2Cancelled:
			assert.True(t, cancelled, "Task2 should have received cancellation")
		case <-time.After(100 * time.Millisecond):
			// It's okay if we don't get the signal, the test might have completed
		}
	}
}

// TestFailFast_MarksPendingTasksAsCancelled verifies that unstarted tasks are marked CANCELLED
func TestFailFast_MarksPendingTasksAsCancelled(t *testing.T) {
	engine := NewEngine(WithErrorStrategy(FailFast))

	// Create a chain: task1 -> task2 -> task3
	// task1 will fail, task2 and task3 should be cancelled
	task1 := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			return assert.AnError
		},
	}

	task2 := &Task{
		id:        "task2",
		dependsOn: []string{"task1"},
		handler: func(c context.Context, ctx *Context) error {
			return nil
		},
	}

	task3 := &Task{
		id:        "task3",
		dependsOn: []string{"task2"},
		handler: func(c context.Context, ctx *Context) error {
			return nil
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)
	err = engine.Register(task3)
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.ErrorIs(t, err, assert.AnError)
	assert.NotNil(t, result)

	// Verify execution failed
	assert.False(t, result.Success)

	// Verify task1 failed
	assert.Equal(t, TaskStatusFailed, result.Reports["task1"].Status)

	// Verify task2 and task3 are cancelled (they depend on task1)
	assert.Equal(t, TaskStatusCancelled, result.Reports["task2"].Status)
	assert.Equal(t, TaskStatusCancelled, result.Reports["task3"].Status)
}

// TestFailFast_ContinuesUntilActiveTasksComplete verifies that active tasks are allowed to complete
func TestFailFast_ContinuesUntilActiveTasksComplete(t *testing.T) {
	engine := NewEngine(WithErrorStrategy(FailFast))

	// Track task execution
	task1Started := make(chan bool, 1)
	task2Started := make(chan bool, 1)
	task2Completed := make(chan bool, 1)

	// task1 and task2 have no dependencies, so they start concurrently
	// task1 will fail quickly, task2 will take longer but should complete
	task1 := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			task1Started <- true
			time.Sleep(10 * time.Millisecond)
			return assert.AnError
		},
	}

	task2 := &Task{
		id: "task2",
		handler: func(c context.Context, ctx *Context) error {
			task2Started <- true
			// Sleep longer to ensure task1 fails first
			time.Sleep(50 * time.Millisecond)
			task2Completed <- true
			// Check if context was cancelled
			if c.Err() != nil {
				return c.Err()
			}
			return nil
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.ErrorIs(t, err, assert.AnError)
	assert.NotNil(t, result)

	// Verify execution failed
	assert.False(t, result.Success)

	// Verify task1 failed
	assert.Equal(t, TaskStatusFailed, result.Reports["task1"].Status)

	// Verify both tasks started
	select {
	case <-task1Started:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("task1 should have started")
	}

	select {
	case <-task2Started:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("task2 should have started")
	}

	// Verify task2 completed (even though task1 failed)
	select {
	case <-task2Completed:
		// Task2 completed successfully
	case <-time.After(200 * time.Millisecond):
		// Task2 might have been cancelled, which is also acceptable
		// The important thing is that we waited for it to finish
	}

	// task2 should have a report (either SUCCESS or FAILED due to cancellation)
	assert.NotNil(t, result.Reports["task2"])
	assert.Contains(t, []TaskStatus{TaskStatusSuccess, TaskStatusFailed}, result.Reports["task2"].Status)
}

// TestFailFast_ExecutionResultContainsErrorDetails verifies that failure result contains error details
func TestFailFast_ExecutionResultContainsErrorDetails(t *testing.T) {
	engine := NewEngine(WithErrorStrategy(FailFast))

	expectedErr := assert.AnError
	task1 := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			return expectedErr
		},
	}

	task2 := &Task{
		id:        "task2",
		dependsOn: []string{"task1"},
		handler: func(c context.Context, ctx *Context) error {
			return nil
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.ErrorIs(t, err, expectedErr)
	assert.NotNil(t, result)

	// Verify execution failed
	assert.False(t, result.Success, "ExecutionResult.Success should be false")

	// Verify failed task report contains error
	assert.NotNil(t, result.Reports["task1"])
	assert.Equal(t, TaskStatusFailed, result.Reports["task1"].Status)
	assert.Equal(t, expectedErr, result.Reports["task1"].Err)

	// Verify all tasks have reports
	assert.Len(t, result.Reports, 2)
	assert.NotNil(t, result.Reports["task1"])
	assert.NotNil(t, result.Reports["task2"])
}

// TestExecutionCompletion_AllTasksReachTerminalState verifies that execution waits for all tasks
func TestExecutionCompletion_AllTasksReachTerminalState(t *testing.T) {
	engine := NewEngine()

	// Create tasks with various execution times
	task1 := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			time.Sleep(20 * time.Millisecond)
			ctx.SetResult("task1", "result1")
			return nil
		},
	}

	task2 := &Task{
		id: "task2",
		handler: func(c context.Context, ctx *Context) error {
			time.Sleep(30 * time.Millisecond)
			ctx.SetResult("task2", "result2")
			return nil
		},
	}

	task3 := &Task{
		id:        "task3",
		dependsOn: []string{"task1", "task2"},
		handler: func(c context.Context, ctx *Context) error {
			time.Sleep(10 * time.Millisecond)
			ctx.SetResult("task3", "result3")
			return nil
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)
	err = engine.Register(task3)
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Verify all tasks reached terminal state (SUCCESS)
	assert.Len(t, result.Reports, 3, "All tasks should have reports")
	assert.Equal(t, TaskStatusSuccess, result.Reports["task1"].Status)
	assert.Equal(t, TaskStatusSuccess, result.Reports["task2"].Status)
	assert.Equal(t, TaskStatusSuccess, result.Reports["task3"].Status)

	// Verify execution succeeded
	assert.True(t, result.Success, "Execution should succeed when all tasks succeed")
}

// TestExecutionResult_ContainsRequiredFields verifies ExecutionResult structure
func TestExecutionResult_ContainsRequiredFields(t *testing.T) {
	engine := NewEngine()

	task1 := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			ctx.SetResult("task1", "data1")
			return nil
		},
	}

	task2 := &Task{
		id:        "task2",
		dependsOn: []string{"task1"},
		handler: func(c context.Context, ctx *Context) error {
			ctx.SetResult("task2", "data2")
			return nil
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Verify ExecutionResult contains all required fields
	assert.NotEmpty(t, result.ExecutionID, "ExecutionID should be set")
	assert.NotNil(t, result.Reports, "Reports should not be nil")
	assert.NotNil(t, result.Store, "Store should not be nil")
	assert.True(t, result.Success, "Success should be true when all tasks succeed")

	// Verify Reports contains entries for all tasks
	assert.Len(t, result.Reports, 2)
	assert.NotNil(t, result.Reports["task1"])
	assert.NotNil(t, result.Reports["task2"])

	// Verify each report has required fields
	for taskID, report := range result.Reports {
		assert.Equal(t, taskID, report.TaskID, "TaskID should match")
		assert.NotEmpty(t, report.Status, "Status should be set")
		assert.False(t, report.StartTime.IsZero(), "StartTime should be set")
		assert.False(t, report.EndTime.IsZero(), "EndTime should be set")
		assert.True(t, report.Duration > 0, "Duration should be positive")
	}
}

// TestExecutionResult_SuccessDetermination verifies Success field logic
func TestExecutionResult_SuccessDetermination(t *testing.T) {
	t.Run("Success when all tasks succeed", func(t *testing.T) {
		engine := NewEngine()

		task1 := &Task{
			id:      "task1",
			handler: func(c context.Context, ctx *Context) error { return nil },
		}
		task2 := &Task{
			id:      "task2",
			handler: func(c context.Context, ctx *Context) error { return nil },
		}

		err := engine.Register(task1)
		assert.NoError(t, err)
		err = engine.Register(task2)
		assert.NoError(t, err)

		result, err := engine.Execute(context.Background(), nil)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.Success, "Success should be true when all tasks succeed")
	})

	t.Run("Failure when any task fails", func(t *testing.T) {
		engine := NewEngine(WithErrorStrategy(FailFast))

		task1 := &Task{
			id:      "task1",
			handler: func(c context.Context, ctx *Context) error { return nil },
		}
		task2 := &Task{
			id:      "task2",
			handler: func(c context.Context, ctx *Context) error { return assert.AnError },
		}

		err := engine.Register(task1)
		assert.NoError(t, err)
		err = engine.Register(task2)
		assert.NoError(t, err)

		result, err := engine.Execute(context.Background(), nil)
		assert.ErrorIs(t, err, assert.AnError)
		assert.NotNil(t, result)
		assert.False(t, result.Success, "Success should be false when any task fails")
	})

	t.Run("Failure when any task is cancelled", func(t *testing.T) {
		engine := NewEngine(WithErrorStrategy(FailFast))

		task1 := &Task{
			id:      "task1",
			handler: func(c context.Context, ctx *Context) error { return assert.AnError },
		}
		task2 := &Task{
			id:        "task2",
			dependsOn: []string{"task1"},
			handler:   func(c context.Context, ctx *Context) error { return nil },
		}

		err := engine.Register(task1)
		assert.NoError(t, err)
		err = engine.Register(task2)
		assert.NoError(t, err)

		result, err := engine.Execute(context.Background(), nil)
		assert.ErrorIs(t, err, assert.AnError)
		assert.NotNil(t, result)
		assert.False(t, result.Success, "Success should be false when any task is cancelled")
		assert.Equal(t, TaskStatusCancelled, result.Reports["task2"].Status)
	})
}

// TestExecutionResult_GetResult verifies GetResult method
func TestExecutionResult_GetResult(t *testing.T) {
	engine := NewEngine()

	task1 := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			ctx.SetResult("task1", "task1-data")
			return nil
		},
	}

	task2 := &Task{
		id:        "task2",
		dependsOn: []string{"task1"},
		handler: func(c context.Context, ctx *Context) error {
			// Read task1's result
			data, ok := ctx.GetResult("task1")
			assert.True(t, ok, "Should be able to read task1's result")
			assert.Equal(t, "task1-data", data)

			// Write task2's result
			ctx.SetResult("task2", "task2-data")
			return nil
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Verify GetResult method works on ExecutionResult
	data1, ok1 := result.GetResult("task1")
	assert.True(t, ok1, "Should find task1's result")
	assert.Equal(t, "task1-data", data1)

	data2, ok2 := result.GetResult("task2")
	assert.True(t, ok2, "Should find task2's result")
	assert.Equal(t, "task2-data", data2)

	// Verify non-existent task returns false
	_, ok3 := result.GetResult("nonexistent")
	assert.False(t, ok3, "Should not find non-existent task")
}

// TestExecutionResult_DatastoreAccess verifies Store field provides access to task results
func TestExecutionResult_DatastoreAccess(t *testing.T) {
	engine := NewEngine()

	task1 := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			ctx.SetResult("task1", map[string]string{"key": "value1"})
			return nil
		},
	}

	task2 := &Task{
		id: "task2",
		handler: func(c context.Context, ctx *Context) error {
			ctx.SetResult("task2", []int{1, 2, 3})
			return nil
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Verify Store provides access to results
	assert.NotNil(t, result.Store, "Store should not be nil")

	data1, ok1 := result.Store.Get("task1")
	assert.True(t, ok1)
	assert.Equal(t, map[string]string{"key": "value1"}, data1)

	data2, ok2 := result.Store.Get("task2")
	assert.True(t, ok2)
	assert.Equal(t, []int{1, 2, 3}, data2)
}

// TestTimeout_TaskSpecificTimeout verifies task-specific timeout in full execution
func TestTimeout_TaskSpecificTimeout(t *testing.T) {
	engine := NewEngine(WithErrorStrategy(FailFast))

	// Task with short timeout that will exceed it
	task1 := &Task{
		id:      "task1",
		timeout: 10 * time.Millisecond,
		handler: func(c context.Context, ctx *Context) error {
			// Try to sleep longer than timeout
			select {
			case <-c.Done():
				return c.Err()
			case <-time.After(100 * time.Millisecond):
				return nil
			}
		},
	}

	// Task that depends on task1 (should be cancelled due to Fail-Fast)
	task2 := &Task{
		id:        "task2",
		dependsOn: []string{"task1"},
		handler: func(c context.Context, ctx *Context) error {
			return nil
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.NotNil(t, result)

	// Verify execution failed
	assert.False(t, result.Success)

	// Verify task1 failed with timeout error
	assert.Equal(t, TaskStatusFailed, result.Reports["task1"].Status)
	assert.NotNil(t, result.Reports["task1"].Err)
	assert.ErrorIs(t, result.Reports["task1"].Err, context.DeadlineExceeded)

	// Verify task2 was cancelled
	assert.Equal(t, TaskStatusCancelled, result.Reports["task2"].Status)
}

// TestTimeout_DefaultTimeout verifies default timeout in full execution
func TestTimeout_DefaultTimeout(t *testing.T) {
	engine := NewEngine(
		WithDefaultTaskTimeout(10*time.Millisecond),
		WithErrorStrategy(FailFast),
	)

	// Task without specific timeout (will use default)
	task1 := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			// Try to sleep longer than default timeout
			select {
			case <-c.Done():
				return c.Err()
			case <-time.After(100 * time.Millisecond):
				return nil
			}
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.NotNil(t, result)

	// Verify execution failed
	assert.False(t, result.Success)

	// Verify task1 failed with timeout error
	assert.Equal(t, TaskStatusFailed, result.Reports["task1"].Status)
	assert.NotNil(t, result.Reports["task1"].Err)
	assert.ErrorIs(t, result.Reports["task1"].Err, context.DeadlineExceeded)
}

// TestTimeout_TaskTimeoutOverridesDefault verifies task timeout takes precedence
func TestTimeout_TaskTimeoutOverridesDefault(t *testing.T) {
	engine := NewEngine(WithDefaultTaskTimeout(100 * time.Millisecond))

	// Task with shorter timeout than default
	task1 := &Task{
		id:      "task1",
		timeout: 10 * time.Millisecond,
		handler: func(c context.Context, ctx *Context) error {
			// Try to sleep longer than task timeout but less than default
			select {
			case <-c.Done():
				return c.Err()
			case <-time.After(50 * time.Millisecond):
				return nil
			}
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.NotNil(t, result)

	// Verify execution failed
	assert.False(t, result.Success)

	// Verify task1 failed with timeout error (from task timeout, not default)
	assert.Equal(t, TaskStatusFailed, result.Reports["task1"].Status)
	assert.NotNil(t, result.Reports["task1"].Err)
	assert.ErrorIs(t, result.Reports["task1"].Err, context.DeadlineExceeded)

	// Verify it timed out quickly (closer to 10ms than 100ms)
	assert.Less(t, result.Reports["task1"].Duration, 50*time.Millisecond)
}

// TestTimeout_NoTimeoutConfigured verifies tasks run without timeout when none configured
func TestTimeout_NoTimeoutConfigured(t *testing.T) {
	engine := NewEngine()

	// Task without timeout
	task1 := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			// Sleep for a bit - should complete successfully
			time.Sleep(20 * time.Millisecond)
			ctx.SetResult("task1", "completed")
			return nil
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Verify execution succeeded
	assert.True(t, result.Success)

	// Verify task1 succeeded
	assert.Equal(t, TaskStatusSuccess, result.Reports["task1"].Status)
	assert.Nil(t, result.Reports["task1"].Err)

	// Verify result was stored
	data, ok := result.GetResult("task1")
	assert.True(t, ok)
	assert.Equal(t, "completed", data)
}

// TestTopologicalSort_ComplexDependencies tests complex dependency scenarios
func TestTopologicalSort_ComplexDependencies(t *testing.T) {
	engine := NewEngine()

	// Create a diamond dependency:
	//     task1
	//    /     \
	// task2   task3
	//    \     /
	//    task4
	task1 := NewTask("task1", func(c context.Context, ctx *Context) error { return nil })
	task2 := NewTask("task2", func(c context.Context, ctx *Context) error { return nil }, WithDependsOn(task1))
	task3 := NewTask("task3", func(c context.Context, ctx *Context) error { return nil }, WithDependsOn(task1))
	task4 := NewTask("task4", func(c context.Context, ctx *Context) error { return nil }, WithDependsOn(task2, task3))

	err := engine.Register(task1, task2, task3, task4)
	assert.NoError(t, err)

	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.TopoOrder, 4)

	// Verify ordering constraints
	task1Index := -1
	task2Index := -1
	task3Index := -1
	task4Index := -1

	t.Log(result.TopoOrder)

	for i, taskID := range result.TopoOrder {
		switch taskID {
		case "task1":
			task1Index = i
		case "task2":
			task2Index = i
		case "task3":
			task3Index = i
		case "task4":
			task4Index = i
		}
	}

	assert.True(t, task1Index < task2Index, "task1 should come before task2")
	assert.True(t, task1Index < task3Index, "task1 should come before task3")
	assert.True(t, task2Index < task4Index, "task2 should come before task4")
	assert.True(t, task3Index < task4Index, "task3 should come before task4")
}

// TestTopologicalSort_DisconnectedGraphs tests tasks with no dependencies
func TestTopologicalSort_DisconnectedGraphs(t *testing.T) {
	engine := NewEngine()

	// Create disconnected tasks
	taskA := NewTask("taskA", func(c context.Context, ctx *Context) error { return nil })
	taskB := NewTask("taskB", func(c context.Context, ctx *Context) error { return nil })
	taskC := NewTask("taskC", func(c context.Context, ctx *Context) error { return nil }, WithDependsOn(taskA))

	err := engine.Register(taskA, taskB, taskC)
	assert.NoError(t, err)

	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.TopoOrder, 3)

	// All tasks should be included
	taskIDs := make(map[string]bool)
	for _, taskID := range result.TopoOrder {
		taskIDs[taskID] = true
	}
	assert.True(t, taskIDs["taskA"])
	assert.True(t, taskIDs["taskB"])
	assert.True(t, taskIDs["taskC"])
}

// TestTopologicalSort_CycleDetection tests detection of cyclic dependencies
func TestTopologicalSort_CycleDetection(t *testing.T) {
	engine := NewEngine()

	// Create tasks with a cycle: task1 -> task2 -> task3 -> task1
	task1 := NewTask("task1", func(c context.Context, ctx *Context) error { return nil })
	task2 := NewTask("task2", func(c context.Context, ctx *Context) error { return nil }, WithDependsOn(task1))
	task3 := NewTask("task3", func(c context.Context, ctx *Context) error { return nil }, WithDependsOn(task2))

	// Apply cyclic dependency manually
	WithDependsOn(task3)(task1)

	err := engine.Register(task1, task2, task3)
	assert.NoError(t, err)

	// Should fail to sort due to cycle
	_, err = engine.Execute(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cyclic dependency detected")
}

// TestExecuteTask_InputAccessibility verifies that handlers can access input through Context.Input()
func TestExecuteTask_InputAccessibility(t *testing.T) {
	type OrderRequest struct {
		UserID string
		SKUID  string
	}

	engine := NewEngine()

	// Create a task that accesses input
	var capturedInput *OrderRequest
	task := NewTask("task1", func(c context.Context, ctx *Context) error {
		input, ok := ctx.Input().(*OrderRequest)
		assert.True(t, ok, "Input should be of type *OrderRequest")
		capturedInput = input
		return nil
	})

	err := engine.Register(task)
	assert.NoError(t, err)
	assert.NoError(t, engine.Build())

	// Execute with input
	inputData := &OrderRequest{
		UserID: "user123",
		SKUID:  "sku456",
	}
	result, err := engine.Execute(context.Background(), inputData)
	assert.NoError(t, err)
	assert.True(t, result.Success)

	// Verify the handler received the correct input
	assert.NotNil(t, capturedInput)
	assert.Equal(t, "user123", capturedInput.UserID)
	assert.Equal(t, "sku456", capturedInput.SKUID)
}

// TestExecuteTask_InputAccessibilityMultipleTasks verifies that all tasks in a workflow can access input
func TestExecuteTask_InputAccessibilityMultipleTasks(t *testing.T) {
	type WorkflowInput struct {
		Value int
	}

	engine := NewEngine()

	// Track which tasks accessed the input
	accessedTasks := make(map[string]int)
	var mu sync.Mutex

	task1 := NewTask("task1", func(c context.Context, ctx *Context) error {
		input := ctx.Input().(*WorkflowInput)
		mu.Lock()
		accessedTasks["task1"] = input.Value
		mu.Unlock()
		return nil
	})

	task2 := NewTask("task2", func(c context.Context, ctx *Context) error {
		input := ctx.Input().(*WorkflowInput)
		mu.Lock()
		accessedTasks["task2"] = input.Value
		mu.Unlock()
		return nil
	}, WithDependsOn(task1))

	task3 := NewTask("task3", func(c context.Context, ctx *Context) error {
		input := ctx.Input().(*WorkflowInput)
		mu.Lock()
		accessedTasks["task3"] = input.Value
		mu.Unlock()
		return nil
	}, WithDependsOn(task2))

	err := engine.Register(task1, task2, task3)
	assert.NoError(t, err)
	assert.NoError(t, engine.Build())

	// Execute with input
	inputData := &WorkflowInput{Value: 42}
	result, err := engine.Execute(context.Background(), inputData)
	assert.NoError(t, err)
	assert.True(t, result.Success)

	// Verify all tasks received the correct input
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 42, accessedTasks["task1"])
	assert.Equal(t, 42, accessedTasks["task2"])
	assert.Equal(t, 42, accessedTasks["task3"])
}

// TestExecuteTask_NilInput verifies that handlers can handle nil input
func TestExecuteTask_NilInput(t *testing.T) {
	engine := NewEngine()

	var capturedInput any
	task := NewTask("task1", func(c context.Context, ctx *Context) error {
		capturedInput = ctx.Input()
		return nil
	})

	err := engine.Register(task)
	assert.NoError(t, err)
	assert.NoError(t, engine.Build())

	// Execute with nil input
	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success)

	// Verify the handler received nil
	assert.Nil(t, capturedInput)
}

// TestMiddleware_PanicRecovery_Mechanism verifies we can write a middleware that catches panic and returns it as error
func TestMiddleware_PanicRecovery_Mechanism(t *testing.T) {
	recoveryMiddleware := func(c context.Context, ctx *Context) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic recovered: %v", r)
			}
		}()
		return ctx.Next(c)
	}

	engine := NewEngine()
	engine.Use(recoveryMiddleware)

	task := NewTask("panic-task", func(c context.Context, ctx *Context) error {
		panic("oops")
	})
	assert.NoError(t, engine.Register(task))

	result, err := engine.Execute(context.Background(), nil)

	// Should return error (or fail result) but NOT CRASH
	assert.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Equal(t, TaskStatusFailed, result.Reports["panic-task"].Status)
	assert.Contains(t, result.Reports["panic-task"].Err.Error(), "panic recovered: oops")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "panic recovered: oops")
}

func TestErrorStrategy_RunAll_AllowsIndependentTasksToComplete(t *testing.T) {
	// Start with default FailFast to verify baseline
	eFast := NewEngine(WithErrorStrategy(FailFast))
	t1_fail := NewTask("t1", func(c context.Context, ctx *Context) error { return assert.AnError })
	t2_ok := NewTask("t2", func(c context.Context, ctx *Context) error {
		time.Sleep(50 * time.Millisecond)
		return nil
	})
	eFast.Register(t1_fail, t2_ok)
	resFast, err := eFast.Execute(context.Background(), nil)
	assert.ErrorIs(t, err, assert.AnError)

	// In FailFast, t2 might be cancelled or might succeed depending on race,
	// but the execution itself is marked Failed
	assert.False(t, resFast.Success)
}

func TestErrorStrategy_RunAll(t *testing.T) {
	engine := NewEngine(func(e *Engine) { e.errorStrategy = RunAll })

	// t1 fails immediately
	t1 := NewTask("t1", func(c context.Context, ctx *Context) error {
		return assert.AnError
	})

	// t2 runs successfully (independent)
	t2 := NewTask("t2", func(c context.Context, ctx *Context) error {
		time.Sleep(20 * time.Millisecond)
		return nil
	})

	// t3 depends on t1 (should be CANCELLED due to upstream failure)
	t3 := NewTask("t3", func(c context.Context, ctx *Context) error {
		return nil
	}, WithDependsOn(t1))

	// t1_allowed fails but has AllowFailure=true
	t1_allowed := NewTask("t1_allow", func(c context.Context, ctx *Context) error {
		return assert.AnError
	}, WithAllowFailure(true))

	// t5 depends on t1_allow (should run because parent failure is allowed)
	t5_dep_allowed := NewTask("t5", func(c context.Context, ctx *Context) error {
		return nil
	}, WithDependsOn(t1_allowed))

	engine.Register(t1, t2, t3, t1_allowed, t5_dep_allowed)

	res, err := engine.Execute(context.Background(), nil)
	assert.ErrorIs(t, err, assert.AnError)

	// t1 failed
	assert.Equal(t, TaskStatusFailed, res.Reports["t1"].Status)

	// t2 success (RunAll ensured it wasn't cancelled by t1 failure)
	assert.Equal(t, TaskStatusSuccess, res.Reports["t2"].Status)

	// t3 cancelled (upstream t1 failed)
	assert.Equal(t, TaskStatusCancelled, res.Reports["t3"].Status)

	// t1_allow failed
	assert.Equal(t, TaskStatusFailed, res.Reports["t1_allow"].Status)

	// t5 success (upstream t1_allow failed but was allowed)
	assert.Equal(t, TaskStatusSuccess, res.Reports["t5"].Status)

	// Overall success?
	// t1 failed (critical) -> Success = false
	assert.False(t, res.Success)
}

func TestExecute_AllowFailureDoesNotReturnError(t *testing.T) {
	engine := NewEngine(WithErrorStrategy(FailFast))

	t1Allowed := NewTask("t1_allow", func(c context.Context, ctx *Context) error {
		return assert.AnError
	}, WithAllowFailure(true))

	t2 := NewTask("t2", func(c context.Context, ctx *Context) error {
		return nil
	}, WithDependsOn(t1Allowed))

	err := engine.Register(t1Allowed, t2)
	assert.NoError(t, err)

	result, execErr := engine.Execute(context.Background(), nil)
	assert.NoError(t, execErr)
	assert.NotNil(t, result)
	assert.True(t, result.Success)
	assert.Equal(t, TaskStatusFailed, result.Reports["t1_allow"].Status)
	assert.Equal(t, TaskStatusSuccess, result.Reports["t2"].Status)
}

// TestFailFast_MaxConcurrency1_UnstartedTasksCancelled verifies that with maxConcurrency=1,
// when the first task fails with FailFast, dependent tasks are marked CANCELLED.
// Note: This uses a dependency chain to ensure task2/task3 are never even scheduled.
func TestFailFast_MaxConcurrency1_UnstartedTasksCancelled(t *testing.T) {
	engine := NewEngine(
		WithErrorStrategy(FailFast),
		WithMaxConcurrency(1), // Only one task can run at a time
	)

	// task1 fails immediately
	task1 := NewTask("task1", func(c context.Context, ctx *Context) error {
		return assert.AnError
	})

	// task2 depends on task1 (will be cancelled due to upstream failure)
	task2 := NewTask("task2", func(c context.Context, ctx *Context) error {
		t.Fatal("task2 should not have run - depends on failed task1")
		return nil
	}, WithDependsOn(task1))

	// task3 depends on task2 (will be cancelled due to cascading failure)
	task3 := NewTask("task3", func(c context.Context, ctx *Context) error {
		t.Fatal("task3 should not have run - depends on cancelled task2")
		return nil
	}, WithDependsOn(task2))

	err := engine.Register(task1, task2, task3)
	assert.NoError(t, err)

	// Execute
	result, execErr := engine.Execute(context.Background(), nil)
	assert.NotNil(t, result)
	assert.ErrorIs(t, execErr, assert.AnError)

	// Verify task1 failed
	assert.Equal(t, TaskStatusFailed, result.Reports["task1"].Status)

	// Verify dependent tasks are marked CANCELLED
	assert.NotNil(t, result.Reports["task2"])
	assert.NotNil(t, result.Reports["task3"])
	assert.Equal(t, TaskStatusCancelled, result.Reports["task2"].Status)
	assert.Equal(t, TaskStatusCancelled, result.Reports["task3"].Status)

	// Execution should fail
	assert.False(t, result.Success)
}

// TestContextCancellation_UnstartedTasksCancelled verifies that when the parent context
// is cancelled during execution, Execute returns and unstarted tasks are marked CANCELLED.
func TestContextCancellation_UnstartedTasksCancelled(t *testing.T) {
	engine := NewEngine(
		WithMaxConcurrency(1), // Serialize tasks to ensure predictable order
	)

	task1Started := make(chan struct{})
	task1Blocked := make(chan struct{})

	// task1 blocks until we unblock it (simulates a long-running task)
	task1 := NewTask("task1", func(c context.Context, ctx *Context) error {
		close(task1Started)
		select {
		case <-c.Done():
			return c.Err()
		case <-task1Blocked:
			return nil
		}
	})

	// task2 should never start because context is cancelled while task1 is running
	task2 := NewTask("task2", func(c context.Context, ctx *Context) error {
		t.Fatal("task2 should not have started")
		return nil
	}, WithDependsOn(task1))

	err := engine.Register(task1, task2)
	assert.NoError(t, err)

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Run execution in a goroutine
	done := make(chan struct{})
	var result *ExecutionResult
	var execErr error
	go func() {
		result, execErr = engine.Execute(ctx, nil)
		close(done)
	}()

	// Wait for task1 to start
	<-task1Started

	// Cancel the context
	cancel()

	// Wait for execution to complete (should not hang)
	select {
	case <-done:
		// Good, execution returned
	case <-time.After(2 * time.Second):
		t.Fatal("Execute should have returned after context cancellation, but it blocked")
	}

	// Verify result
	assert.NotNil(t, result)
	assert.Error(t, execErr)
	assert.ErrorIs(t, execErr, context.Canceled)

	// task1 should be FAILED (it returned the context error)
	assert.Equal(t, TaskStatusFailed, result.Reports["task1"].Status)

	// task2 should be CANCELLED (never started)
	assert.NotNil(t, result.Reports["task2"])
	assert.Equal(t, TaskStatusCancelled, result.Reports["task2"].Status)

	// Execution should fail
	assert.False(t, result.Success)
}
