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
		ctx:         context.Background(),
		TaskID:      "task1",
		ExecutionID: "exec1",
		StartTime:   time.Now(),
		Store:       store,
		Logger:      nil,
		handlers:    nil,
		index:       -1,
	}

	// Test SetResult writes to correct key
	ctx.SetResult("test-value")

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
		ctx:         context.Background(),
		TaskID:      "task3",
		ExecutionID: "exec1",
		StartTime:   time.Now(),
		Store:       store,
		Logger:      nil,
		handlers:    nil,
		index:       -1,
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

func TestContext_Context(t *testing.T) {
	baseCtx := context.Background()
	ctx := &Context{
		ctx:         baseCtx,
		TaskID:      "task1",
		ExecutionID: "exec1",
		StartTime:   time.Now(),
		Store:       newMemoryStore(),
		Logger:      nil,
		handlers:    nil,
		index:       -1,
	}

	// Test Context() returns underlying context
	assert.Equal(t, baseCtx, ctx.Context(), "Should return underlying context")
}

func TestContext_Next(t *testing.T) {
	store := newMemoryStore()
	executionOrder := []string{}

	handler1 := func(c *Context) error {
		executionOrder = append(executionOrder, "handler1-before")
		err := c.Next()
		executionOrder = append(executionOrder, "handler1-after")
		return err
	}

	handler2 := func(c *Context) error {
		executionOrder = append(executionOrder, "handler2-before")
		err := c.Next()
		executionOrder = append(executionOrder, "handler2-after")
		return err
	}

	handler3 := func(c *Context) error {
		executionOrder = append(executionOrder, "handler3")
		return nil
	}

	ctx := &Context{
		ctx:         context.Background(),
		TaskID:      "task1",
		ExecutionID: "exec1",
		StartTime:   time.Now(),
		Store:       store,
		Logger:      nil,
		handlers:    []HandlerFunc{handler1, handler2, handler3},
		index:       -1,
	}

	// Execute the handler chain
	err := ctx.Next()
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
		ctx:         context.Background(),
		TaskID:      "task1",
		ExecutionID: "exec1",
		StartTime:   time.Now(),
		Store:       newMemoryStore(),
		Logger:      nil,
		handlers:    []HandlerFunc{},
		index:       -1,
	}

	// Calling Next on empty chain should not panic
	err := ctx.Next()
	assert.NoError(t, err, "Should not return error")
}
