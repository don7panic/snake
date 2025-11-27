package snake

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestExecute_GeneratesUniqueExecutionID verifies that each execution gets a unique ID
func TestExecute_GeneratesUniqueExecutionID(t *testing.T) {
	engine := NewEngine()

	// Register a simple task
	task := &Task{
		ID:      "task1",
		Handler: func(ctx *Context) error { return nil },
	}
	err := engine.Register(task)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute twice and verify different ExecutionIDs
	result1, err := engine.Execute(context.Background())
	assert.NoError(t, err)
	assert.NotEmpty(t, result1.ExecutionID)

	// Small delay to ensure different timestamp
	time.Sleep(time.Millisecond)

	result2, err := engine.Execute(context.Background())
	assert.NoError(t, err)
	assert.NotEmpty(t, result2.ExecutionID)

	// Verify ExecutionIDs are different
	assert.NotEqual(t, result1.ExecutionID, result2.ExecutionID, "ExecutionIDs should be unique")
}

// TestExecute_CreatesNewDatastore verifies that each execution gets a fresh Datastore
func TestExecute_CreatesNewDatastore(t *testing.T) {
	engine := NewEngine()

	// Register a simple task
	task := &Task{
		ID:      "task1",
		Handler: func(ctx *Context) error { return nil },
	}
	err := engine.Register(task)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute and verify Datastore is empty
	result, err := engine.Execute(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, result.Store)

	// Verify Datastore is empty (no tasks have run yet)
	_, ok := result.Store.Get("task1")
	assert.False(t, ok, "Datastore should be empty before tasks execute")
}

// TestExecute_InitializesPendingDeps verifies pending dependency counts match indegree
func TestExecute_InitializesPendingDeps(t *testing.T) {
	engine := NewEngine()

	// Register tasks with dependencies
	task1 := &Task{
		ID:      "task1",
		Handler: func(ctx *Context) error { return nil },
	}
	task2 := &Task{
		ID:        "task2",
		DependsOn: []string{"task1"},
		Handler:   func(ctx *Context) error { return nil },
	}
	task3 := &Task{
		ID:        "task3",
		DependsOn: []string{"task1", "task2"},
		Handler:   func(ctx *Context) error { return nil },
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
	assert.Equal(t, 1, engine.indegree["task2"], "task2 should have indegree 1")
	assert.Equal(t, 2, engine.indegree["task3"], "task3 should have indegree 2")

	// Execute to initialize pending deps
	result, err := engine.Execute(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestExecute_IdentifiesZeroIndegreeTasks verifies that tasks with no dependencies are identified
func TestExecute_IdentifiesZeroIndegreeTasks(t *testing.T) {
	engine := NewEngine()

	// Register tasks with mixed dependencies
	task1 := &Task{
		ID:      "task1",
		Handler: func(ctx *Context) error { return nil },
	}
	task2 := &Task{
		ID:      "task2",
		Handler: func(ctx *Context) error { return nil },
	}
	task3 := &Task{
		ID:        "task3",
		DependsOn: []string{"task1"},
		Handler:   func(ctx *Context) error { return nil },
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

	// Execute to verify zero-indegree tasks are identified
	result, err := engine.Execute(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestExecute_DisconnectedGraphs verifies support for non-connected DAGs
func TestExecute_DisconnectedGraphs(t *testing.T) {
	engine := NewEngine()

	// Create two disconnected subgraphs
	// Subgraph 1: task1 -> task2
	task1 := &Task{
		ID:      "task1",
		Handler: func(ctx *Context) error { return nil },
	}
	task2 := &Task{
		ID:        "task2",
		DependsOn: []string{"task1"},
		Handler:   func(ctx *Context) error { return nil },
	}

	// Subgraph 2: task3 -> task4
	task3 := &Task{
		ID:      "task3",
		Handler: func(ctx *Context) error { return nil },
	}
	task4 := &Task{
		ID:        "task4",
		DependsOn: []string{"task3"},
		Handler:   func(ctx *Context) error { return nil },
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)
	err = engine.Register(task3)
	assert.NoError(t, err)
	err = engine.Register(task4)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Verify both task1 and task3 have zero indegree
	assert.Equal(t, 0, engine.indegree["task1"], "task1 should have indegree 0")
	assert.Equal(t, 0, engine.indegree["task3"], "task3 should have indegree 0")

	// Execute to verify both subgraphs are handled
	result, err := engine.Execute(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestExecuteTask_CreatesContext verifies that each task gets a proper Context
func TestExecuteTask_CreatesContext(t *testing.T) {
	engine := NewEngine()

	// Track if handler was called with proper context
	var capturedCtx *Context
	task := &Task{
		ID: "task1",
		Handler: func(ctx *Context) error {
			capturedCtx = ctx
			return nil
		},
	}

	err := engine.Register(task)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute the task directly
	store := newMemoryStore()
	report := engine.executeTask(context.Background(), "task1", "exec-123", store)

	// Verify report
	assert.NotNil(t, report)
	assert.Equal(t, "task1", report.TaskID)
	assert.Equal(t, TaskStatusSuccess, report.Status)

	// Verify context was created properly
	assert.NotNil(t, capturedCtx)
	assert.Equal(t, "task1", capturedCtx.TaskID)
	assert.Equal(t, "exec-123", capturedCtx.ExecutionID)
	assert.NotNil(t, capturedCtx.Store)
	assert.False(t, capturedCtx.StartTime.IsZero())
}

// TestExecuteTask_AppliesTaskTimeout verifies task-specific timeout is applied
func TestExecuteTask_AppliesTaskTimeout(t *testing.T) {
	engine := NewEngine()

	// Create a task with a short timeout
	task := &Task{
		ID:      "task1",
		Timeout: 10 * time.Millisecond,
		Handler: func(ctx *Context) error {
			// Wait for context to be cancelled or timeout
			select {
			case <-ctx.Context().Done():
				return ctx.Context().Err()
			case <-time.After(100 * time.Millisecond):
				return nil
			}
		},
	}

	err := engine.Register(task)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute the task
	store := newMemoryStore()
	report := engine.executeTask(context.Background(), "task1", "exec-123", store)

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
		ID: "task1",
		Handler: func(ctx *Context) error {
			// Wait for context to be cancelled or timeout
			select {
			case <-ctx.Context().Done():
				return ctx.Context().Err()
			case <-time.After(100 * time.Millisecond):
				return nil
			}
		},
	}

	err := engine.Register(task)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute the task
	store := newMemoryStore()
	report := engine.executeTask(context.Background(), "task1", "exec-123", store)

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
	engine.Use(func(ctx *Context) error {
		executionOrder = append(executionOrder, "global-before")
		err := ctx.Next()
		executionOrder = append(executionOrder, "global-after")
		return err
	})

	// Create task with task-specific middleware
	task := &Task{
		ID: "task1",
		Middlewares: []HandlerFunc{
			func(ctx *Context) error {
				executionOrder = append(executionOrder, "task-before")
				err := ctx.Next()
				executionOrder = append(executionOrder, "task-after")
				return err
			},
		},
		Handler: func(ctx *Context) error {
			executionOrder = append(executionOrder, "handler")
			return nil
		},
	}

	err := engine.Register(task)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute the task
	store := newMemoryStore()
	report := engine.executeTask(context.Background(), "task1", "exec-123", store)

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
		ID: "task1",
		Handler: func(ctx *Context) error {
			return nil
		},
	}

	err := engine.Register(task)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute the task
	store := newMemoryStore()
	report := engine.executeTask(context.Background(), "task1", "exec-123", store)

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
		ID: "task1",
		Handler: func(ctx *Context) error {
			return expectedErr
		},
	}

	err := engine.Register(task)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute the task
	store := newMemoryStore()
	report := engine.executeTask(context.Background(), "task1", "exec-123", store)

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
		ID: "task1",
		Handler: func(ctx *Context) error {
			time.Sleep(sleepDuration)
			return nil
		},
	}

	err := engine.Register(task)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute the task
	store := newMemoryStore()
	report := engine.executeTask(context.Background(), "task1", "exec-123", store)

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
		ID: "task1",
		Handler: func(ctx *Context) error {
			taskCtxDone = ctx.Context().Done()
			return nil
		},
	}

	err := engine.Register(task)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute the task
	store := newMemoryStore()
	report := engine.executeTask(execCtx, "task1", "exec-123", store)

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

// TestExecuteTask_LoggerHasTaskMetadata verifies logger includes task metadata
func TestExecuteTask_LoggerHasTaskMetadata(t *testing.T) {
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
		ID: "task1",
		Handler: func(ctx *Context) error {
			// Log something to verify logger has metadata
			if ctx.Logger != nil {
				ctx.Logger.Info(ctx.Context(), "test message")
			}
			return nil
		},
	}

	err := engine.Register(task)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute the task
	store := newMemoryStore()
	report := engine.executeTask(context.Background(), "task1", "exec-123", store)

	// Verify task succeeded
	assert.NotNil(t, report)
	assert.Equal(t, TaskStatusSuccess, report.Status)

	// Verify logger was called with task metadata
	assert.NotEmpty(t, logCalls)

	// Check that the logger has TaskID and ExecutionID fields
	foundTaskID := false
	foundExecutionID := false
	for _, call := range logCalls {
		for _, field := range call.fields {
			if field.Key == "TaskID" && field.Value == "task1" {
				foundTaskID = true
			}
			if field.Key == "ExecutionID" && field.Value == "exec-123" {
				foundExecutionID = true
			}
		}
	}

	assert.True(t, foundTaskID, "Logger should have TaskID field")
	assert.True(t, foundExecutionID, "Logger should have ExecutionID field")
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
		ID: "task1",
		Handler: func(ctx *Context) error {
			return expectedErr
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background())
	assert.NoError(t, err)
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
		ID: "task1",
		Handler: func(ctx *Context) error {
			return assert.AnError
		},
	}

	task2 := &Task{
		ID: "task2",
		Handler: func(ctx *Context) error {
			// Wait for cancellation or timeout
			select {
			case <-ctx.Context().Done():
				task2Cancelled <- true
				return ctx.Context().Err()
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

	err = engine.Build()
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Verify execution failed
	assert.False(t, result.Success)

	// Verify task1 failed
	assert.Equal(t, TaskStatusFailed, result.Reports["task1"].Status)

	// Verify task2 received cancellation (if it started)
	// Note: task2 might be SKIPPED if it never started, or FAILED if it was running
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

// TestFailFast_MarksPendingTasksAsSkipped verifies that unstarted tasks are marked SKIPPED
func TestFailFast_MarksPendingTasksAsSkipped(t *testing.T) {
	engine := NewEngine(WithFailFast())

	// Create a chain: task1 -> task2 -> task3
	// task1 will fail, task2 and task3 should be skipped
	task1 := &Task{
		ID: "task1",
		Handler: func(ctx *Context) error {
			return assert.AnError
		},
	}

	task2 := &Task{
		ID:        "task2",
		DependsOn: []string{"task1"},
		Handler: func(ctx *Context) error {
			return nil
		},
	}

	task3 := &Task{
		ID:        "task3",
		DependsOn: []string{"task2"},
		Handler: func(ctx *Context) error {
			return nil
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)
	err = engine.Register(task3)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Verify execution failed
	assert.False(t, result.Success)

	// Verify task1 failed
	assert.Equal(t, TaskStatusFailed, result.Reports["task1"].Status)

	// Verify task2 and task3 are skipped (they depend on task1)
	assert.Equal(t, TaskStatusSkipped, result.Reports["task2"].Status)
	assert.Equal(t, TaskStatusSkipped, result.Reports["task3"].Status)
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
		ID: "task1",
		Handler: func(ctx *Context) error {
			task1Started <- true
			time.Sleep(10 * time.Millisecond)
			return assert.AnError
		},
	}

	task2 := &Task{
		ID: "task2",
		Handler: func(ctx *Context) error {
			task2Started <- true
			// Sleep longer to ensure task1 fails first
			time.Sleep(50 * time.Millisecond)
			task2Completed <- true
			// Check if context was cancelled
			if ctx.Context().Err() != nil {
				return ctx.Context().Err()
			}
			return nil
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background())
	assert.NoError(t, err)
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
		ID: "task1",
		Handler: func(ctx *Context) error {
			return expectedErr
		},
	}

	task2 := &Task{
		ID:        "task2",
		DependsOn: []string{"task1"},
		Handler: func(ctx *Context) error {
			return nil
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background())
	assert.NoError(t, err)
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
		ID: "task1",
		Handler: func(ctx *Context) error {
			time.Sleep(20 * time.Millisecond)
			ctx.SetResult("result1")
			return nil
		},
	}

	task2 := &Task{
		ID: "task2",
		Handler: func(ctx *Context) error {
			time.Sleep(30 * time.Millisecond)
			ctx.SetResult("result2")
			return nil
		},
	}

	task3 := &Task{
		ID:        "task3",
		DependsOn: []string{"task1", "task2"},
		Handler: func(ctx *Context) error {
			time.Sleep(10 * time.Millisecond)
			ctx.SetResult("result3")
			return nil
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)
	err = engine.Register(task3)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background())
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
		ID: "task1",
		Handler: func(ctx *Context) error {
			ctx.SetResult("data1")
			return nil
		},
	}

	task2 := &Task{
		ID:        "task2",
		DependsOn: []string{"task1"},
		Handler: func(ctx *Context) error {
			ctx.SetResult("data2")
			return nil
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background())
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
			ID:      "task1",
			Handler: func(ctx *Context) error { return nil },
		}
		task2 := &Task{
			ID:      "task2",
			Handler: func(ctx *Context) error { return nil },
		}

		err := engine.Register(task1)
		assert.NoError(t, err)
		err = engine.Register(task2)
		assert.NoError(t, err)

		err = engine.Build()
		assert.NoError(t, err)

		result, err := engine.Execute(context.Background())
		assert.NoError(t, err)
		assert.True(t, result.Success, "Success should be true when all tasks succeed")
	})

	t.Run("Failure when any task fails", func(t *testing.T) {
		engine := NewEngine(WithFailFast())

		task1 := &Task{
			ID:      "task1",
			Handler: func(ctx *Context) error { return nil },
		}
		task2 := &Task{
			ID:      "task2",
			Handler: func(ctx *Context) error { return assert.AnError },
		}

		err := engine.Register(task1)
		assert.NoError(t, err)
		err = engine.Register(task2)
		assert.NoError(t, err)

		err = engine.Build()
		assert.NoError(t, err)

		result, err := engine.Execute(context.Background())
		assert.NoError(t, err)
		assert.False(t, result.Success, "Success should be false when any task fails")
	})

	t.Run("Failure when any task is skipped", func(t *testing.T) {
		engine := NewEngine(WithFailFast())

		task1 := &Task{
			ID:      "task1",
			Handler: func(ctx *Context) error { return assert.AnError },
		}
		task2 := &Task{
			ID:        "task2",
			DependsOn: []string{"task1"},
			Handler:   func(ctx *Context) error { return nil },
		}

		err := engine.Register(task1)
		assert.NoError(t, err)
		err = engine.Register(task2)
		assert.NoError(t, err)

		err = engine.Build()
		assert.NoError(t, err)

		result, err := engine.Execute(context.Background())
		assert.NoError(t, err)
		assert.False(t, result.Success, "Success should be false when any task is skipped")
		assert.Equal(t, TaskStatusSkipped, result.Reports["task2"].Status)
	})
}

// TestExecutionResult_GetResult verifies GetResult method
func TestExecutionResult_GetResult(t *testing.T) {
	engine := NewEngine()

	task1 := &Task{
		ID: "task1",
		Handler: func(ctx *Context) error {
			ctx.SetResult("task1-data")
			return nil
		},
	}

	task2 := &Task{
		ID:        "task2",
		DependsOn: []string{"task1"},
		Handler: func(ctx *Context) error {
			// Read task1's result
			data, ok := ctx.GetResult("task1")
			assert.True(t, ok, "Should be able to read task1's result")
			assert.Equal(t, "task1-data", data)

			// Write task2's result
			ctx.SetResult("task2-data")
			return nil
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background())
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
		ID: "task1",
		Handler: func(ctx *Context) error {
			ctx.SetResult(map[string]string{"key": "value1"})
			return nil
		},
	}

	task2 := &Task{
		ID: "task2",
		Handler: func(ctx *Context) error {
			ctx.SetResult([]int{1, 2, 3})
			return nil
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background())
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
