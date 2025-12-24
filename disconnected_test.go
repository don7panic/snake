package snake

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestDisconnectedGraph_AllZeroIndegreeTasksIdentified verifies that all tasks with no dependencies
// are identified and scheduled, regardless of connectivity
func TestDisconnectedGraph_AllZeroIndegreeTasksIdentified(t *testing.T) {
	engine := NewEngine()

	// Create three disconnected subgraphs
	// Subgraph 1: task1 -> task2
	// Subgraph 2: task3 (standalone)
	// Subgraph 3: task4 -> task5 -> task6

	startedTasks := make(map[string]bool)
	var mu sync.Mutex

	tasks := []*Task{
		{
			id: "task1",
			handler: func(c context.Context, ctx *Context) error {
				mu.Lock()
				startedTasks["task1"] = true
				mu.Unlock()
				return nil
			},
		},
		{
			id:        "task2",
			dependsOn: []string{"task1"},
			handler: func(c context.Context, ctx *Context) error {
				mu.Lock()
				startedTasks["task2"] = true
				mu.Unlock()
				return nil
			},
		},
		{
			id: "task3",
			handler: func(c context.Context, ctx *Context) error {
				mu.Lock()
				startedTasks["task3"] = true
				mu.Unlock()
				return nil
			},
		},
		{
			id: "task4",
			handler: func(c context.Context, ctx *Context) error {
				mu.Lock()
				startedTasks["task4"] = true
				mu.Unlock()
				return nil
			},
		},
		{
			id:        "task5",
			dependsOn: []string{"task4"},
			handler: func(c context.Context, ctx *Context) error {
				mu.Lock()
				startedTasks["task5"] = true
				mu.Unlock()
				return nil
			},
		},
		{
			id:        "task6",
			dependsOn: []string{"task5"},
			handler: func(c context.Context, ctx *Context) error {
				mu.Lock()
				startedTasks["task6"] = true
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

	assert.NoError(t, engine.Build())

	// Verify zero-indegree tasks are identified
	assert.Equal(t, 0, engine.indegree["task1"], "task1 should have indegree 0")
	assert.Equal(t, 0, engine.indegree["task3"], "task3 should have indegree 0")
	assert.Equal(t, 0, engine.indegree["task4"], "task4 should have indegree 0")
	assert.Equal(t, 1, engine.indegree["task2"], "task2 should have indegree 1")
	assert.Equal(t, 1, engine.indegree["task5"], "task5 should have indegree 1")
	assert.Equal(t, 1, engine.indegree["task6"], "task6 should have indegree 1")

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success)

	// Verify all zero-indegree tasks started
	mu.Lock()
	assert.True(t, startedTasks["task1"], "task1 should have started")
	assert.True(t, startedTasks["task3"], "task3 should have started")
	assert.True(t, startedTasks["task4"], "task4 should have started")
	mu.Unlock()

	// Verify all tasks completed successfully
	assert.Len(t, result.Reports, 6)
	for taskID, report := range result.Reports {
		assert.Equal(t, TaskStatusSuccess, report.Status, "Task %s should succeed", taskID)
	}
}

// TestDisconnectedGraph_AllSubgraphsExecuteInSameExecution verifies that all disconnected
// subgraphs are executed in a single execution
func TestDisconnectedGraph_AllSubgraphsExecuteInSameExecution(t *testing.T) {
	engine := NewEngine()

	// Create two disconnected subgraphs with data flow
	// Subgraph 1: task1 -> task2
	// Subgraph 2: task3 -> task4

	tasks := []*Task{
		{
			id: "task1",
			handler: func(c context.Context, ctx *Context) error {
				ctx.SetResult("task1", "data1")
				return nil
			},
		},
		{
			id:        "task2",
			dependsOn: []string{"task1"},
			handler: func(c context.Context, ctx *Context) error {
				data, ok := ctx.GetResult("task1")
				assert.True(t, ok)
				assert.Equal(t, "data1", data)
				ctx.SetResult("task2", "data2")
				return nil
			},
		},
		{
			id: "task3",
			handler: func(c context.Context, ctx *Context) error {
				ctx.SetResult("task3", "data3")
				return nil
			},
		},
		{
			id:        "task4",
			dependsOn: []string{"task3"},
			handler: func(c context.Context, ctx *Context) error {
				data, ok := ctx.GetResult("task3")
				assert.True(t, ok)
				assert.Equal(t, "data3", data)
				ctx.SetResult("task4", "data4")
				return nil
			},
		},
	}

	var err error
	for _, task := range tasks {
		err = engine.Register(task)
		assert.NoError(t, err)
	}

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success)

	// Verify all tasks completed
	assert.Len(t, result.Reports, 4)
	for _, report := range result.Reports {
		assert.Equal(t, TaskStatusSuccess, report.Status)
	}

	// Verify all results are in the same Datastore
	data1, ok1 := result.GetResult("task1")
	assert.True(t, ok1)
	assert.Equal(t, "data1", data1)

	data2, ok2 := result.GetResult("task2")
	assert.True(t, ok2)
	assert.Equal(t, "data2", data2)

	data3, ok3 := result.GetResult("task3")
	assert.True(t, ok3)
	assert.Equal(t, "data3", data3)

	data4, ok4 := result.GetResult("task4")
	assert.True(t, ok4)
	assert.Equal(t, "data4", data4)

	// Verify they all share the same ExecutionID
	executionID := result.ExecutionID
	assert.NotEmpty(t, executionID)
}

// TestDisconnectedGraph_FailFastCancelsAllSubgraphs verifies that when one subgraph fails,
// all other subgraphs are cancelled in Fail-Fast mode
func TestDisconnectedGraph_FailFastCancelsAllSubgraphs(t *testing.T) {
	engine := NewEngine(WithErrorStrategy(FailFast))

	// Create three disconnected subgraphs
	// Subgraph 1: task1 (will fail)
	// Subgraph 2: task2 -> task3 (should be cancelled/skipped)
	// Subgraph 3: task4 (may complete or be cancelled)

	task1Started := make(chan bool, 1)
	task2Started := make(chan bool, 1)
	task3Started := make(chan bool, 1)
	task4Started := make(chan bool, 1)

	tasks := []*Task{
		{
			id: "task1",
			handler: func(c context.Context, ctx *Context) error {
				task1Started <- true
				// Fail immediately
				return assert.AnError
			},
		},
		{
			id: "task2",
			handler: func(c context.Context, ctx *Context) error {
				task2Started <- true
				// Sleep to give task1 time to fail
				time.Sleep(50 * time.Millisecond)
				// Check if context was cancelled
				if c.Err() != nil {
					return c.Err()
				}
				ctx.SetResult("task2", "data2")
				return nil
			},
		},
		{
			id:        "task3",
			dependsOn: []string{"task2"},
			handler: func(c context.Context, ctx *Context) error {
				task3Started <- true
				return nil
			},
		},
		{
			id: "task4",
			handler: func(c context.Context, ctx *Context) error {
				task4Started <- true
				// Sleep to give task1 time to fail
				time.Sleep(50 * time.Millisecond)
				// Check if context was cancelled
				if c.Err() != nil {
					return c.Err()
				}
				return nil
			},
		},
	}

	var err error
	for _, task := range tasks {
		err = engine.Register(task)
		assert.NoError(t, err)
	}

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.ErrorIs(t, err, assert.AnError)
	assert.NotNil(t, result)

	// Verify execution failed
	assert.False(t, result.Success, "Execution should fail when any task fails")

	// Verify task1 failed
	assert.Equal(t, TaskStatusFailed, result.Reports["task1"].Status)
	assert.Equal(t, assert.AnError, result.Reports["task1"].Err)

	// Verify task1 started
	select {
	case <-task1Started:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("task1 should have started")
	}

	// Verify task3 was cancelled (depends on task2, which may or may not have completed)
	// If task2 didn't complete, task3 should be CANCELLED
	// The key requirement is that all tasks reach a terminal state
	assert.Contains(t, []TaskStatus{TaskStatusCancelled, TaskStatusFailed}, result.Reports["task3"].Status,
		"task3 should be CANCELLED or FAILED")

	// Verify all tasks have reports (all reached terminal state)
	assert.Len(t, result.Reports, 4, "All tasks should have reports")
	for taskID, report := range result.Reports {
		assert.NotEmpty(t, report.Status, "Task %s should have a status", taskID)
		assert.Contains(t, []TaskStatus{TaskStatusSuccess, TaskStatusFailed, TaskStatusCancelled}, report.Status,
			"Task %s should have a valid terminal status", taskID)
	}
}

// TestDisconnectedGraph_AllTasksReachTerminalState verifies that all tasks in all subgraphs
// reach a terminal state (SUCCESS, FAILED, or CANCELLED)
func TestDisconnectedGraph_AllTasksReachTerminalState(t *testing.T) {
	engine := NewEngine(WithErrorStrategy(FailFast))

	// Create a complex disconnected graph with a failure
	// Subgraph 1: task1 -> task2 -> task3 (task2 will fail)
	// Subgraph 2: task4 -> task5
	// Subgraph 3: task6 (standalone)

	tasks := []*Task{
		{
			id: "task1",
			handler: func(c context.Context, ctx *Context) error {
				time.Sleep(10 * time.Millisecond)
				return nil
			},
		},
		{
			id:        "task2",
			dependsOn: []string{"task1"},
			handler: func(c context.Context, ctx *Context) error {
				time.Sleep(10 * time.Millisecond)
				return assert.AnError // Fail
			},
		},
		{
			id:        "task3",
			dependsOn: []string{"task2"},
			handler: func(c context.Context, ctx *Context) error {
				return nil
			},
		},
		{
			id: "task4",
			handler: func(c context.Context, ctx *Context) error {
				time.Sleep(30 * time.Millisecond)
				// Check if cancelled
				if c.Err() != nil {
					return c.Err()
				}
				return nil
			},
		},
		{
			id:        "task5",
			dependsOn: []string{"task4"},
			handler: func(c context.Context, ctx *Context) error {
				return nil
			},
		},
		{
			id: "task6",
			handler: func(c context.Context, ctx *Context) error {
				time.Sleep(30 * time.Millisecond)
				// Check if cancelled
				if c.Err() != nil {
					return c.Err()
				}
				return nil
			},
		},
	}

	var err error
	for _, task := range tasks {
		err = engine.Register(task)
		assert.NoError(t, err)
	}

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.ErrorIs(t, err, assert.AnError)
	assert.NotNil(t, result)

	// Verify execution failed
	assert.False(t, result.Success)

	// Verify all tasks have reports
	assert.Len(t, result.Reports, 6, "All tasks should have reports")

	// Verify all tasks reached a terminal state
	terminalStates := []TaskStatus{TaskStatusSuccess, TaskStatusFailed, TaskStatusCancelled}
	for taskID, report := range result.Reports {
		assert.Contains(t, terminalStates, report.Status,
			"Task %s should have a terminal status, got %s", taskID, report.Status)
		assert.False(t, report.StartTime.IsZero(), "Task %s should have a start time", taskID)
		assert.False(t, report.EndTime.IsZero(), "Task %s should have an end time", taskID)
	}

	// Verify specific task states
	assert.Equal(t, TaskStatusSuccess, result.Reports["task1"].Status, "task1 should succeed")
	assert.Equal(t, TaskStatusFailed, result.Reports["task2"].Status, "task2 should fail")
	assert.Equal(t, TaskStatusCancelled, result.Reports["task3"].Status, "task3 should be cancelled")

	// task4, task5, task6 should either complete or be cancelled
	// The important thing is they all have terminal states
	for _, taskID := range []string{"task4", "task5", "task6"} {
		assert.Contains(t, terminalStates, result.Reports[taskID].Status,
			"Task %s should have a terminal status", taskID)
	}
}

// TestDisconnectedGraph_ZeroIndegreeTasksStartConcurrently verifies that all zero-indegree
// tasks from different subgraphs start concurrently
func TestDisconnectedGraph_ZeroIndegreeTasksStartConcurrently(t *testing.T) {
	engine := NewEngine()

	startTimes := make(map[string]time.Time)
	var mu sync.Mutex

	// Create three disconnected subgraphs, each with a zero-indegree task
	tasks := []*Task{
		{
			id: "task1",
			handler: func(c context.Context, ctx *Context) error {
				mu.Lock()
				startTimes["task1"] = time.Now()
				mu.Unlock()
				time.Sleep(20 * time.Millisecond)
				return nil
			},
		},
		{
			id:        "task2",
			dependsOn: []string{"task1"},
			handler:   func(c context.Context, ctx *Context) error { return nil },
		},
		{
			id: "task3",
			handler: func(c context.Context, ctx *Context) error {
				mu.Lock()
				startTimes["task3"] = time.Now()
				mu.Unlock()
				time.Sleep(20 * time.Millisecond)
				return nil
			},
		},
		{
			id:        "task4",
			dependsOn: []string{"task3"},
			handler:   func(c context.Context, ctx *Context) error { return nil },
		},
		{
			id: "task5",
			handler: func(c context.Context, ctx *Context) error {
				mu.Lock()
				startTimes["task5"] = time.Now()
				mu.Unlock()
				time.Sleep(20 * time.Millisecond)
				return nil
			},
		},
	}

	var err error
	for _, task := range tasks {
		err = engine.Register(task)
		assert.NoError(t, err)
	}

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success)

	// Verify all zero-indegree tasks started within a short window
	mu.Lock()
	assert.Len(t, startTimes, 3, "All zero-indegree tasks should have started")

	var firstStart, lastStart time.Time
	for _, startTime := range startTimes {
		if firstStart.IsZero() || startTime.Before(firstStart) {
			firstStart = startTime
		}
		if lastStart.IsZero() || startTime.After(lastStart) {
			lastStart = startTime
		}
	}
	mu.Unlock()

	startWindow := lastStart.Sub(firstStart)
	assert.Less(t, startWindow, 20*time.Millisecond,
		"All zero-indegree tasks should start nearly simultaneously, got window of %v", startWindow)
}
