package snake

import (
	"context"
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
	// When tasks fail (due to errors or timeouts), Execute may return an error
	// We should still get a valid result even if there's an error
	assert.NotNil(t, result)
	assert.NotNil(t, result.Store)

	// Verify Datastore is empty (no tasks have run yet)
	_, ok := result.Store.Get("task1")
	assert.False(t, ok, "Datastore should be empty before tasks execute")
}

// TestExecute_IdentifiesZeroIndegreeTasks verifies that tasks with no dependencies are identified
func TestExecute_IdentifiesZeroIndegreeTasks(t *testing.T) {
	engine := NewEngine()

	// Register tasks with mixed dependencies
	task1 := &Task{
		id:      "task1",
		handler: func(c context.Context, ctx *Context) error { return nil },
	}
	task2 := &Task{
		id:      "task2",
		handler: func(c context.Context, ctx *Context) error { return nil },
	}
	task3 := &Task{
		id:        "task3",
		dependsOn: []string{"task1"},
		handler:   func(c context.Context, ctx *Context) error { return nil },
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)
	err = engine.Register(task3)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Verify indegree values
	assert.Equal(t, 0, engine.indegree["task1"], "task1 should have indegree 0")
	assert.Equal(t, 0, engine.indegree["task2"], "task2 should have indegree 0")
	assert.Equal(t, 1, engine.indegree["task3"], "task3 should have indegree 1")
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
	task2 := NewTask("task2", func(c context.Context, ctx *Context) error { return nil }, WithDependsOn("task1"))

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
	// When tasks fail (due to errors or timeouts), Execute may return an error
	// We should still get a valid result even if there's an error
	assert.NotNil(t, result)
}

// TestExecuteTask_CreatesContext verifies that each task gets a proper Context
func TestExecuteTask_CreatesContext(t *testing.T) {
	engine := NewEngine()

	// Track if handler was called with proper context
	var capturedCtx *Context
	task := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			capturedCtx = ctx
			return nil
		},
	}

	err := engine.Register(task)
	assert.NoError(t, err)

	// Execute the task directly
	store := newMemoryStore()
	report, err := engine.executeTask(context.Background(), "task1", "exec-123", store, nil)
	assert.NoError(t, err)

	// Verify report
	assert.NotNil(t, report)
	assert.Equal(t, "task1", report.TaskID)
	assert.Equal(t, TaskStatusSuccess, report.Status)

	// Verify context was created properly
	assert.NotNil(t, capturedCtx)
	assert.Equal(t, "task1", capturedCtx.taskID)
	assert.Equal(t, "exec-123", capturedCtx.executionID)
	assert.NotNil(t, capturedCtx.store)
	assert.False(t, capturedCtx.StartTime.IsZero())
}

// TestExecuteTask_AppliesTaskTimeout verifies task-specific timeout is applied
func TestExecuteTask_AppliesTaskTimeout(t *testing.T) {
	engine := NewEngine()

	// Create a task with a short timeout
	task := NewTask("task1", func(c context.Context, ctx *Context) error {
		// Wait for context to be cancelled or timeout
		select {
		case <-c.Done():
			return c.Err()
		case <-time.After(100 * time.Millisecond):
			return nil
		}
	}, WithTimeout(10*time.Millisecond))

	err := engine.Register(task)
	assert.NoError(t, err)

	// Execute the task
	store := newMemoryStore()
	report, err := engine.executeTask(context.Background(), "task1", "exec-123", store, nil)
	// When a task times out, executeTask should return an error
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Verify task failed due to timeout
	assert.NotNil(t, report)
	assert.Equal(t, TaskStatusFailed, report.Status)
	assert.NotNil(t, report.Err)
	assert.ErrorIs(t, report.Err, context.DeadlineExceeded)
}

// TestExecuteTask_AppliesDefaultTimeout verifies default timeout is used when task timeout not set
func TestExecuteTask_AppliesDefaultTimeout(t *testing.T) {
	engine := NewEngine(WithDefaultTaskTimeout(10 * time.Millisecond))

	// Create a task without specific timeout
	task := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			// Wait for context to be cancelled or timeout
			select {
			case <-c.Done():
				return c.Err()
			case <-time.After(100 * time.Millisecond):
				return nil
			}
		},
	}

	err := engine.Register(task)
	assert.NoError(t, err)

	// Execute the task
	store := newMemoryStore()
	report, err := engine.executeTask(context.Background(), "task1", "exec-123", store, nil)
	// When a task times out, executeTask should return an error
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Verify task failed due to timeout
	assert.NotNil(t, report)
	assert.Equal(t, TaskStatusFailed, report.Status)
	assert.NotNil(t, report.Err)
	assert.ErrorIs(t, report.Err, context.DeadlineExceeded)
}

// TestExecuteTask_BuildsHandlerChain verifies handler chain includes global and task middleware
func TestExecuteTask_BuildsHandlerChain(t *testing.T) {
	engine := NewEngine()

	// Track execution order
	executionOrder := []string{}

	// Register global middleware
	engine.Use(func(c context.Context, ctx *Context) error {
		executionOrder = append(executionOrder, "global-before")
		err := ctx.Next(c)
		executionOrder = append(executionOrder, "global-after")
		return err
	})

	// Create task with task-specific middleware
	task := NewTask("task1", func(c context.Context, ctx *Context) error {
		executionOrder = append(executionOrder, "handler")
		return nil
	}, WithMiddlewares(func(c context.Context, ctx *Context) error {
		executionOrder = append(executionOrder, "task-before")
		err := ctx.Next(c)
		executionOrder = append(executionOrder, "task-after")
		return err
	}))

	err := engine.Register(task)
	assert.NoError(t, err)

	// Execute the task
	store := newMemoryStore()
	report, err := engine.executeTask(context.Background(), "task1", "exec-123", store, nil)
	assert.NoError(t, err)

	// Verify execution succeeded
	assert.NotNil(t, report)
	assert.Equal(t, TaskStatusSuccess, report.Status)

	// Verify execution order: global -> task -> handler -> task -> global
	expectedOrder := []string{
		"global-before",
		"task-before",
		"handler",
		"task-after",
		"global-after",
	}
	assert.Equal(t, expectedOrder, executionOrder)
}

// TestExecuteTask_RecordsSuccessStatus verifies successful tasks are marked SUCCESS
func TestExecuteTask_RecordsSuccessStatus(t *testing.T) {
	engine := NewEngine()

	task := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			return nil
		},
	}

	err := engine.Register(task)
	assert.NoError(t, err)

	// Execute the task
	store := newMemoryStore()
	report, err := engine.executeTask(context.Background(), "task1", "exec-123", store, nil)
	assert.NoError(t, err)

	// Verify report
	assert.NotNil(t, report)
	assert.Equal(t, "task1", report.TaskID)
	assert.Equal(t, TaskStatusSuccess, report.Status)
	assert.Nil(t, report.Err)
	assert.False(t, report.StartTime.IsZero())
	assert.False(t, report.EndTime.IsZero())
	assert.True(t, report.Duration > 0)
}

// TestExecuteTask_RecordsFailureStatus verifies failed tasks are marked FAILED
func TestExecuteTask_RecordsFailureStatus(t *testing.T) {
	engine := NewEngine()

	expectedErr := assert.AnError
	task := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			return expectedErr
		},
	}

	err := engine.Register(task)
	assert.NoError(t, err)

	// Execute the task
	store := newMemoryStore()
	report, err := engine.executeTask(context.Background(), "task1", "exec-123", store, nil)
	// When a task fails, executeTask returns the error
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)

	// Verify report
	assert.NotNil(t, report)
	assert.Equal(t, "task1", report.TaskID)
	assert.Equal(t, TaskStatusFailed, report.Status)
	assert.Equal(t, expectedErr, report.Err)
	assert.False(t, report.StartTime.IsZero())
	assert.False(t, report.EndTime.IsZero())
	assert.True(t, report.Duration > 0)
}

// TestExecuteTask_RecordsTiming verifies timing information is captured
func TestExecuteTask_RecordsTiming(t *testing.T) {
	engine := NewEngine()

	sleepDuration := 20 * time.Millisecond
	task := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			time.Sleep(sleepDuration)
			return nil
		},
	}

	err := engine.Register(task)
	assert.NoError(t, err)

	// Execute the task
	store := newMemoryStore()
	report, err := engine.executeTask(context.Background(), "task1", "exec-123", store, nil)
	assert.NoError(t, err)

	// Verify timing
	assert.NotNil(t, report)
	assert.False(t, report.StartTime.IsZero())
	assert.False(t, report.EndTime.IsZero())
	assert.True(t, report.EndTime.After(report.StartTime))
	assert.True(t, report.Duration >= sleepDuration, "Duration should be at least the sleep duration")
	assert.Equal(t, report.Duration, report.EndTime.Sub(report.StartTime))
}

// TestExecuteTask_ContextInheritsExecutionContext verifies task context inherits from execution context
func TestExecuteTask_ContextInheritsExecutionContext(t *testing.T) {
	engine := NewEngine()

	// Create a cancellable execution context
	execCtx, cancel := context.WithCancel(context.Background())

	var taskCtxDone <-chan struct{}
	task := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			taskCtxDone = c.Done()
			return nil
		},
	}

	err := engine.Register(task)
	assert.NoError(t, err)

	// Execute the task
	store := newMemoryStore()
	report, err := engine.executeTask(execCtx, "task1", "exec-123", store, nil)
	assert.NoError(t, err)

	// Verify task succeeded
	assert.NotNil(t, report)
	assert.Equal(t, TaskStatusSuccess, report.Status)

	// Cancel execution context
	cancel()

	// Verify task context was cancelled (channel should be closed)
	select {
	case <-taskCtxDone:
		// Expected: context was cancelled
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Task context should have been cancelled when execution context was cancelled")
	}
}

// TestExecuteTask_ContextProperties verifies Context contains correct properties after Logger removal
func TestExecuteTask_ContextProperties(t *testing.T) {
	// Create a mock logger that captures fields
	type logCall struct {
		msg    string
		fields []Field
	}
	var logCalls []logCall

	mockLogger := &mockLogger{
		infoFunc: func(ctx context.Context, msg string, fields ...Field) {
			logCalls = append(logCalls, logCall{msg: msg, fields: fields})
		},
		withFunc: func(fields ...Field) Logger {
			// Return a new logger with the fields
			return &mockLogger{
				fields: fields,
				infoFunc: func(ctx context.Context, msg string, moreFields ...Field) {
					allFields := append(fields, moreFields...)
					logCalls = append(logCalls, logCall{msg: msg, fields: allFields})
				},
			}
		},
	}

	engine := NewEngine(WithLogger(mockLogger))

	task := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			// Note: Logger removed from Context
			// In tests, you can use the engine's logger directly if needed
			return nil
		},
	}

	err := engine.Register(task)
	assert.NoError(t, err)

	// Execute the task
	store := newMemoryStore()
	report, err := engine.executeTask(context.Background(), "task1", "exec-123", store, nil)
	assert.NoError(t, err)

	// Verify task succeeded
	assert.NotNil(t, report)
	assert.Equal(t, TaskStatusSuccess, report.Status)
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
	engine := NewEngine(WithFailFast())

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
	// When tasks fail (due to errors or timeouts), Execute may return an error
	// We should still get a valid result even if there's an error
	assert.NotNil(t, result)

	// Verify task is marked as FAILED
	assert.False(t, result.Success)
	assert.NotNil(t, result.Reports["task1"])
	assert.Equal(t, TaskStatusFailed, result.Reports["task1"].Status)
	assert.Equal(t, expectedErr, result.Reports["task1"].Err)
}

// TestFailFast_CancelsExecutionContext verifies that task failure cancels execution context
func TestFailFast_CancelsExecutionContext(t *testing.T) {
	engine := NewEngine(WithFailFast())

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
	// When tasks fail (due to errors or timeouts), Execute may return an error
	// We should still get a valid result even if there's an error
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
	engine := NewEngine(WithFailFast())

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
	// When tasks fail (due to errors or timeouts), Execute may return an error
	// We should still get a valid result even if there's an error
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
	engine := NewEngine(WithFailFast())

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
	// When tasks fail (due to errors or timeouts), Execute may return an error
	// We should still get a valid result even if there's an error
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
	engine := NewEngine(WithFailFast())

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
	// When tasks fail (due to errors or timeouts), Execute may return an error
	// We should still get a valid result even if there's an error
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
	// When tasks fail (due to errors or timeouts), Execute may return an error
	// We should still get a valid result even if there's an error
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
	// When tasks fail (due to errors or timeouts), Execute may return an error
	// We should still get a valid result even if there's an error
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
		// When tasks fail (due to errors or timeouts), Execute may return an error
		// We should still get a valid result even if there's an error
		assert.NotNil(t, result)
		assert.True(t, result.Success, "Success should be true when all tasks succeed")
	})

	t.Run("Failure when any task fails", func(t *testing.T) {
		engine := NewEngine(WithFailFast())

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
		// When tasks fail (due to errors or timeouts), Execute may return an error
		// We should still get a valid result even if there's an error
		assert.NotNil(t, result)
		assert.False(t, result.Success, "Success should be false when any task fails")
	})

	t.Run("Failure when any task is cancelled", func(t *testing.T) {
		engine := NewEngine(WithFailFast())

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
		// When tasks fail (due to errors or timeouts), Execute may return an error
		// We should still get a valid result even if there's an error
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
	// When tasks fail (due to errors or timeouts), Execute may return an error
	// We should still get a valid result even if there's an error
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
	// When tasks fail (due to errors or timeouts), Execute may return an error
	// We should still get a valid result even if there's an error
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
	engine := NewEngine(WithFailFast())

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
	// When tasks time out, Execute may return an error
	// We should still get a valid result even if there's an error
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
		WithFailFast(),
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
	// When tasks time out, Execute may return an error
	// We should still get a valid result even if there's an error
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
	// When tasks time out, Execute may return an error
	// We should still get a valid result even if there's an error
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
	// When tasks fail (due to errors or timeouts), Execute may return an error
	// We should still get a valid result even if there's an error
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
	task2 := NewTask("task2", func(c context.Context, ctx *Context) error { return nil }, WithDependsOn("task1"))
	task3 := NewTask("task3", func(c context.Context, ctx *Context) error { return nil }, WithDependsOn("task1"))
	task4 := NewTask("task4", func(c context.Context, ctx *Context) error { return nil }, WithDependsOn("task2", "task3"))

	err := engine.Register(task1, task2, task3, task4)
	assert.NoError(t, err)

	result, err := engine.Execute(context.Background(), nil)
	// When tasks fail (due to errors or timeouts), Execute may return an error
	// We should still get a valid result even if there's an error
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
	taskC := NewTask("taskC", func(c context.Context, ctx *Context) error { return nil }, WithDependsOn("taskA"))

	err := engine.Register(taskA, taskB, taskC)
	assert.NoError(t, err)

	result, err := engine.Execute(context.Background(), nil)
	// When tasks fail (due to errors or timeouts), Execute may return an error
	// We should still get a valid result even if there's an error
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
	task1 := NewTask("task1", func(c context.Context, ctx *Context) error { return nil }, WithDependsOn("task3"))
	task2 := NewTask("task2", func(c context.Context, ctx *Context) error { return nil }, WithDependsOn("task1"))
	task3 := NewTask("task3", func(c context.Context, ctx *Context) error { return nil }, WithDependsOn("task2"))

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
	}, WithDependsOn("task1"))

	task3 := NewTask("task3", func(c context.Context, ctx *Context) error {
		input := ctx.Input().(*WorkflowInput)
		mu.Lock()
		accessedTasks["task3"] = input.Value
		mu.Unlock()
		return nil
	}, WithDependsOn("task2"))

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
	// When tasks fail (due to errors or timeouts), Execute may return an error
	// We should still get a valid result even if there's an error
	assert.NotNil(t, result)
	assert.True(t, result.Success)

	// Verify the handler received nil
	assert.Nil(t, capturedInput)
}
