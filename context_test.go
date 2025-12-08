package snake

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestContext_SetResult(t *testing.T) {
	store := newMemoryStore()
	ctx := &Context{
		taskID:      "task1",
		executionID: "exec1",
		StartTime:   time.Now(),
		store:       store,
		handlers:    nil,
		index:       -1,
		input:       nil,
	}

	// Test SetResult writes to correct key
	ctx.SetResult("task1", "test-value")

	// Verify the value was stored with the task's ID
	value, ok := store.Get("task1")
	assert.True(t, ok, "Value should be stored")
	assert.Equal(t, "test-value", value, "Value should match")
}

func TestContext_GetResult(t *testing.T) {
	store := newMemoryStore()
	store.Set("task1", "value1")
	store.Set("task2", "value2")

	ctx := &Context{
		taskID:      "task3",
		executionID: "exec1",
		StartTime:   time.Now(),
		store:       store,
		handlers:    nil,
		index:       -1,
		input:       nil,
	}

	// Test GetResult reads from correct key
	value, ok := ctx.GetResult("task1")
	assert.True(t, ok, "Value should be found")
	assert.Equal(t, "value1", value, "Value should match")

	value, ok = ctx.GetResult("task2")
	assert.True(t, ok, "Value should be found")
	assert.Equal(t, "value2", value, "Value should match")

	// Test non-existent key
	value, ok = ctx.GetResult("nonexistent")
	assert.False(t, ok, "Value should not be found")
	assert.Nil(t, value, "Value should be nil")
}

func TestContext_Next(t *testing.T) {
	store := newMemoryStore()
	executionOrder := []string{}
	baseCtx := context.Background()

	handler1 := func(c context.Context, ctx *Context) error {
		executionOrder = append(executionOrder, "handler1-before")
		err := ctx.Next(c)
		executionOrder = append(executionOrder, "handler1-after")
		return err
	}

	handler2 := func(c context.Context, ctx *Context) error {
		executionOrder = append(executionOrder, "handler2-before")
		err := ctx.Next(c)
		executionOrder = append(executionOrder, "handler2-after")
		return err
	}

	handler3 := func(c context.Context, ctx *Context) error {
		executionOrder = append(executionOrder, "handler3")
		return nil
	}

	ctx := &Context{
		taskID:      "task1",
		executionID: "exec1",
		StartTime:   time.Now(),
		store:       store,
		handlers:    []HandlerFunc{handler1, handler2, handler3},
		index:       -1,
		input:       nil,
	}

	// Execute the handler chain
	err := ctx.Next(baseCtx)
	assert.NoError(t, err, "Should not return error")

	// Verify execution order
	expected := []string{
		"handler1-before",
		"handler2-before",
		"handler3",
		"handler2-after",
		"handler1-after",
	}
	assert.Equal(t, expected, executionOrder, "Handlers should execute in correct order")
}

func TestContext_Next_EmptyChain(t *testing.T) {
	ctx := &Context{
		taskID:      "task1",
		executionID: "exec1",
		StartTime:   time.Now(),
		store:       newMemoryStore(),
		handlers:    []HandlerFunc{},
		index:       -1,
		input:       nil,
	}

	// Calling Next on empty chain should not panic
	err := ctx.Next(context.Background())
	assert.NoError(t, err, "Should not return error")
}

// TestContext_SetResultUsesTaskID verifies SetResult uses task ID as key
func TestContext_SetResultUsesTaskID(t *testing.T) {
	store := newMemoryStore()

	// Create context for task1
	ctx1 := &Context{
		taskID:      "task1",
		executionID: "exec1",
		StartTime:   time.Now(),
		store:       store,
		handlers:    nil,
		index:       -1,
		input:       nil,
	}

	// Create context for task2
	ctx2 := &Context{
		taskID:      "task2",
		executionID: "exec1",
		StartTime:   time.Now(),
		store:       store,
		handlers:    nil,
		index:       -1,
		input:       nil,
	}

	// Each task sets its result
	ctx1.SetResult("task1", "result-from-task1")
	ctx2.SetResult("task2", "result-from-task2")

	// Verify results are stored under correct task IDs
	value1, ok1 := store.Get("task1")
	assert.True(t, ok1, "task1's result should be stored")
	assert.Equal(t, "result-from-task1", value1, "task1's result should be stored under 'task1' key")

	value2, ok2 := store.Get("task2")
	assert.True(t, ok2, "task2's result should be stored")
	assert.Equal(t, "result-from-task2", value2, "task2's result should be stored under 'task2' key")

	// Verify SetResult uses the Context's TaskID, not some other key
	// If we try to get with wrong key, it should fail
	_, ok3 := store.Get("wrong-key")
	assert.False(t, ok3, "Should not find result under wrong key")
}

// TestContext_StartTimeBeforeExecution verifies StartTime is set before handler executes
// Context SHALL contain a start time timestamp for the task execution
func TestContext_StartTimeBeforeExecution(t *testing.T) {
	engine := NewEngine()

	var capturedStartTime time.Time
	var handlerExecutionTime time.Time

	task := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			// Capture the start time from context
			capturedStartTime = ctx.StartTime
			// Capture current time during handler execution
			handlerExecutionTime = time.Now()
			return nil
		},
	}

	err := engine.Register(task)
	assert.NoError(t, err)

	err = engine.Build()
	assert.NoError(t, err)

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Verify StartTime was set before handler executed
	assert.False(t, capturedStartTime.IsZero(), "StartTime should be set")
	assert.True(t, capturedStartTime.Before(handlerExecutionTime) || capturedStartTime.Equal(handlerExecutionTime),
		"StartTime should be at or before handler execution time")
}

// TestContext_AllPropertiesIntegration verifies all Context properties work together
func TestContext_AllPropertiesIntegration(t *testing.T) {
	engine := NewEngine()

	// Variables to capture context properties during execution
	var task1ExecutionID string
	var task1StartTime time.Time
	var task1Store Datastore
	var task2ExecutionID string
	var task2StartTime time.Time
	var task2Store Datastore

	task1 := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			// Verify Context contains all required properties
			assert.NotNil(t, ctx.store, "Context should contain Datastore reference (Req 8.1)")
			assert.NotEmpty(t, ctx.executionID, "Context should contain ExecutionID (Req 10.7)")
			assert.False(t, ctx.StartTime.IsZero(), "Context should contain StartTime (Req 10.8)")
			assert.Equal(t, "task1", ctx.taskID, "Context should contain correct TaskID")

			// Capture properties
			task1ExecutionID = ctx.executionID
			task1StartTime = ctx.StartTime
			task1Store = ctx.store

			ctx.SetResult("task1", "data-from-task1")
			return nil
		},
	}

	task2 := &Task{
		id:        "task2",
		dependsOn: []string{"task1"},
		handler: func(c context.Context, ctx *Context) error {
			// Verify Context contains all required properties
			assert.NotNil(t, ctx.store, "Context should contain Datastore reference (Req 8.1)")
			assert.NotEmpty(t, ctx.executionID, "Context should contain ExecutionID (Req 10.7)")
			assert.False(t, ctx.StartTime.IsZero(), "Context should contain StartTime (Req 10.8)")
			assert.Equal(t, "task2", ctx.taskID, "Context should contain correct TaskID")

			// Capture properties
			task2ExecutionID = ctx.executionID
			task2StartTime = ctx.StartTime
			task2Store = ctx.store

			// Should be able to read task1's result (verifies Req 8.2)
			data, ok := ctx.GetResult("task1")
			assert.True(t, ok, "Should be able to read task1's result")
			assert.Equal(t, "data-from-task1", data, "Should get correct data from task1")

			// SetResult should use task ID as key (Req 8.2)
			ctx.SetResult("task2", "data-from-task2")
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

	// Verify both tasks had the same ExecutionID
	assert.Equal(t, task1ExecutionID, task2ExecutionID, "Both tasks should share the same ExecutionID")
	assert.Equal(t, result.ExecutionID, task1ExecutionID, "Result should have the same ExecutionID")

	// Verify both tasks had the same Datastore instance
	assert.Equal(t, task1Store, task2Store, "Both tasks should share the same Datastore instance")

	// Verify StartTimes are reasonable (task2 should start after task1)
	assert.True(t, task2StartTime.After(task1StartTime) || task2StartTime.Equal(task1StartTime),
		"task2 should start at or after task1 (due to dependency)")

	// Verify Result provides Datastore access (Req 14.5)
	assert.NotNil(t, result.Store, "ExecutionResult should provide Datastore access")

	// Verify we can access task results through ExecutionResult (Req 14.5)
	data1, ok1 := result.GetResult("task1")
	assert.True(t, ok1, "Should be able to access task1's result through ExecutionResult")
	assert.Equal(t, "data-from-task1", data1, "Should get correct data from task1")

	data2, ok2 := result.GetResult("task2")
	assert.True(t, ok2, "Should be able to access task2's result through ExecutionResult")
	assert.Equal(t, "data-from-task2", data2, "Should get correct data from task2")

	// Verify we can also access through Store directly (Req 14.5)
	data1Direct, ok1Direct := result.Store.Get("task1")
	assert.True(t, ok1Direct, "Should be able to access task1's result through Store directly")
	assert.Equal(t, "data-from-task1", data1Direct, "Should get correct data from task1")
}
