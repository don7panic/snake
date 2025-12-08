package snake

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestParallelExecution_SimpleDependency verifies tasks execute in correct order
func TestParallelExecution_SimpleDependency(t *testing.T) {
	engine := NewEngine()

	executionOrder := []string{}
	var mu sync.Mutex

	// task1 -> task2 -> task3
	task1 := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			mu.Lock()
			executionOrder = append(executionOrder, "task1")
			mu.Unlock()
			ctx.SetResult("task1", "result1")
			return nil
		},
	}

	task2 := &Task{
		id:        "task2",
		dependsOn: []string{"task1"},
		handler: func(c context.Context, ctx *Context) error {
			mu.Lock()
			executionOrder = append(executionOrder, "task2")
			mu.Unlock()
			// Verify we can read task1's result
			result, ok := ctx.GetResult("task1")
			assert.True(t, ok)
			assert.Equal(t, "result1", result)
			ctx.SetResult("task2", "result2")
			return nil
		},
	}

	task3 := &Task{
		id:        "task3",
		dependsOn: []string{"task2"},
		handler: func(c context.Context, ctx *Context) error {
			mu.Lock()
			executionOrder = append(executionOrder, "task3")
			mu.Unlock()
			// Verify we can read task2's result
			result, ok := ctx.GetResult("task2")
			assert.True(t, ok)
			assert.Equal(t, "result2", result)
			return nil
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)
	err = engine.Register(task3)
	assert.NoError(t, err)

	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success)

	// Verify all tasks completed
	assert.Len(t, result.Reports, 3)
	assert.Equal(t, TaskStatusSuccess, result.Reports["task1"].Status)
	assert.Equal(t, TaskStatusSuccess, result.Reports["task2"].Status)
	assert.Equal(t, TaskStatusSuccess, result.Reports["task3"].Status)

	// Verify execution order
	assert.Equal(t, []string{"task1", "task2", "task3"}, executionOrder)
}

func TestParallelExecution_RespectsMaxConcurrency(t *testing.T) {
	engine := NewEngine(WithMaxConcurrency(2))

	var running int64
	var maxSeen int64

	handler := func(c context.Context, ctx *Context) error {
		current := atomic.AddInt64(&running, 1)
		for {
			prev := atomic.LoadInt64(&maxSeen)
			if current <= prev || atomic.CompareAndSwapInt64(&maxSeen, prev, current) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt64(&running, -1)
		return nil
	}

	for i := 1; i <= 4; i++ {
		taskID := fmt.Sprintf("task%d", i)
		task := &Task{
			id:      taskID,
			handler: handler,
		}
		assert.NoError(t, engine.Register(task))
	}

	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.LessOrEqual(t, maxSeen, int64(2), "concurrent tasks should respect max concurrency")
}

// TestParallelExecution_ParallelTasks verifies independent tasks run concurrently
func TestParallelExecution_ParallelTasks(t *testing.T) {
	engine := NewEngine()

	startTimes := make(map[string]time.Time)
	var mu sync.Mutex

	// Create 3 independent tasks that sleep
	var err error
	for i := 1; i <= 3; i++ {
		taskID := "task" + string(rune('0'+i))
		task := &Task{
			id: taskID,
			handler: func(c context.Context, ctx *Context) error {
				mu.Lock()
				startTimes[ctx.taskID] = time.Now()
				mu.Unlock()
				time.Sleep(50 * time.Millisecond)
				return nil
			},
		}
		err = engine.Register(task)
		assert.NoError(t, err)
	}

	start := time.Now()
	result, err := engine.Execute(context.Background(), nil)
	duration := time.Since(start)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success)

	// If tasks ran in parallel, total time should be ~50ms, not ~150ms
	assert.Less(t, duration, 100*time.Millisecond, "Tasks should run in parallel")

	// Verify all tasks started within a short window (indicating parallel execution)
	var firstStart, lastStart time.Time
	for _, startTime := range startTimes {
		if firstStart.IsZero() || startTime.Before(firstStart) {
			firstStart = startTime
		}
		if lastStart.IsZero() || startTime.After(lastStart) {
			lastStart = startTime
		}
	}
	startWindow := lastStart.Sub(firstStart)
	assert.Less(t, startWindow, 20*time.Millisecond, "All tasks should start nearly simultaneously")
}

// TestParallelExecution_DiamondDependency verifies diamond dependency pattern
func TestParallelExecution_DiamondDependency(t *testing.T) {
	engine := NewEngine()

	executionOrder := []string{}
	var mu sync.Mutex

	// Diamond pattern:
	//     task1
	//    /     \
	// task2   task3
	//    \     /
	//     task4

	task1 := &Task{
		id: "task1",
		handler: func(c context.Context, ctx *Context) error {
			mu.Lock()
			executionOrder = append(executionOrder, "task1")
			mu.Unlock()
			ctx.SetResult("task1", 1)
			return nil
		},
	}

	task2 := &Task{
		id:        "task2",
		dependsOn: []string{"task1"},
		handler: func(c context.Context, ctx *Context) error {
			mu.Lock()
			executionOrder = append(executionOrder, "task2")
			mu.Unlock()
			ctx.SetResult("task2", 2)
			return nil
		},
	}

	task3 := &Task{
		id:        "task3",
		dependsOn: []string{"task1"},
		handler: func(c context.Context, ctx *Context) error {
			mu.Lock()
			executionOrder = append(executionOrder, "task3")
			mu.Unlock()
			ctx.SetResult("task3", 3)
			return nil
		},
	}

	task4 := &Task{
		id:        "task4",
		dependsOn: []string{"task2", "task3"},
		handler: func(c context.Context, ctx *Context) error {
			mu.Lock()
			executionOrder = append(executionOrder, "task4")
			mu.Unlock()
			// Verify we can read both dependencies
			result2, ok := ctx.GetResult("task2")
			assert.True(t, ok)
			assert.Equal(t, 2, result2)
			result3, ok := ctx.GetResult("task3")
			assert.True(t, ok)
			assert.Equal(t, 3, result3)
			return nil
		},
	}

	err := engine.Register(task1)
	assert.NoError(t, err)
	err = engine.Register(task2)
	assert.NoError(t, err)
	err = engine.Register(task3)
	assert.NoError(t, err)
	err = engine.Register(task4)
	assert.NoError(t, err)

	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success)

	// Verify all tasks completed
	assert.Len(t, result.Reports, 4)
	for _, report := range result.Reports {
		assert.Equal(t, TaskStatusSuccess, report.Status)
	}

	// Verify task1 executed first
	assert.Equal(t, "task1", executionOrder[0])

	// Verify task4 executed last
	assert.Equal(t, "task4", executionOrder[3])

	// task2 and task3 should execute after task1 but before task4
	// They can be in any order relative to each other
	assert.Contains(t, executionOrder[1:3], "task2")
	assert.Contains(t, executionOrder[1:3], "task3")
}

// TestParallelExecution_ComplexDAG verifies a more complex dependency graph
func TestParallelExecution_ComplexDAG(t *testing.T) {
	engine := NewEngine()

	completed := make(map[string]bool)
	var mu sync.Mutex

	// Complex DAG:
	//   task1   task2
	//     |    /  |  \
	//   task3  task4  task5
	//      \    |    /
	//        task6

	tasks := []*Task{
		{
			id: "task1",
			handler: func(c context.Context, ctx *Context) error {
				mu.Lock()
				completed["task1"] = true
				mu.Unlock()
				return nil
			},
		},
		{
			id: "task2",
			handler: func(c context.Context, ctx *Context) error {
				mu.Lock()
				completed["task2"] = true
				mu.Unlock()
				return nil
			},
		},
		{
			id:        "task3",
			dependsOn: []string{"task1", "task2"},
			handler: func(c context.Context, ctx *Context) error {
				mu.Lock()
				// Verify dependencies completed
				assert.True(t, completed["task1"])
				assert.True(t, completed["task2"])
				completed["task3"] = true
				mu.Unlock()
				return nil
			},
		},
		{
			id:        "task4",
			dependsOn: []string{"task2"},
			handler: func(c context.Context, ctx *Context) error {
				mu.Lock()
				assert.True(t, completed["task2"])
				completed["task4"] = true
				mu.Unlock()
				return nil
			},
		},
		{
			id:        "task6",
			dependsOn: []string{"task3", "task4", "task5"},
			handler: func(c context.Context, ctx *Context) error {
				mu.Lock()
				// Verify all dependencies completed
				assert.True(t, completed["task3"])
				assert.True(t, completed["task4"])
				assert.True(t, completed["task5"])
				completed["task6"] = true
				mu.Unlock()
				return nil
			},
		},
		{
			id:        "task5",
			dependsOn: []string{"task2"},
			handler: func(c context.Context, ctx *Context) error {
				mu.Lock()
				assert.True(t, completed["task2"])
				completed["task5"] = true
				mu.Unlock()
				return nil
			},
		},
	}

	var err error
	for _, task := range tasks {
		err = engine.Register(task)
		assert.NoError(t, err)
	}

	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success)

	// Verify all tasks completed
	assert.Len(t, result.Reports, 6)
	for _, report := range result.Reports {
		assert.Equal(t, TaskStatusSuccess, report.Status)
	}

	// Verify all tasks marked as completed
	assert.Len(t, completed, 6)
	for i := 1; i <= 6; i++ {
		taskID := "task" + string(rune('0'+i))
		assert.True(t, completed[taskID], "Task %s should have completed", taskID)
	}
}

// TestParallelExecution_DisconnectedSubgraphs verifies multiple independent subgraphs
func TestParallelExecution_DisconnectedSubgraphs(t *testing.T) {
	engine := NewEngine()

	// Two independent chains:
	// Chain 1: task1 -> task2
	// Chain 2: task3 -> task4

	tasks := []*Task{
		{
			id:      "task1",
			handler: func(c context.Context, ctx *Context) error { return nil },
		},
		{
			id:        "task2",
			dependsOn: []string{"task1"},
			handler:   func(c context.Context, ctx *Context) error { return nil },
		},
		{
			id:      "task3",
			handler: func(c context.Context, ctx *Context) error { return nil },
		},
		{
			id:        "task4",
			dependsOn: []string{"task3"},
			handler:   func(c context.Context, ctx *Context) error { return nil },
		},
	}

	for _, task := range tasks {
		err := engine.Register(task)
		assert.NoError(t, err)
	}

	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success)

	// Verify all tasks completed
	assert.Len(t, result.Reports, 4)
	for _, report := range result.Reports {
		assert.Equal(t, TaskStatusSuccess, report.Status)
	}
}
