package examples

import (
	"context"
	"testing"

	"github.com/don7panic/snake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdvancedFeatures(t *testing.T) {
	// 1. Setup Typed Keys
	OrderScoreKey := snake.NewKey[int]("order_score")
	ShouldProcessKey := snake.NewKey[bool]("should_process")

	engine := snake.NewEngine()

	// Task A: Analyze Order (Sets Typed Keys)
	taskA := snake.NewTask("analyze", func(c context.Context, ctx *snake.Context) error {
		// Set Typed Value
		snake.SetTyped(ctx, OrderScoreKey, 85)
		snake.SetTyped(ctx, ShouldProcessKey, true)
		return nil
	})

	// Task B: High Value Processing (Condition: Score > 80)
	taskB := snake.NewTask("process_high_value", func(c context.Context, ctx *snake.Context) error {
		score, ok := snake.GetTyped(ctx, OrderScoreKey)
		if !ok {
			return nil // Should not happen
		}
		assert.Equal(t, 85, score)
		ctx.SetResult("processed_high", true)
		return nil
	},
		snake.WithDependsOn(taskA),
		snake.WithCondition(func(c context.Context, ctx *snake.Context) bool {
			score, ok := snake.GetTyped(ctx, OrderScoreKey)
			return ok && score > 80
		}),
	)

	// Task C: Low Value Processing (Condition: Score <= 80) -- Should be SKIPPED
	taskC := snake.NewTask("process_low_value", func(c context.Context, ctx *snake.Context) error {
		t.Error("Task C should have been skipped!")
		return nil
	},
		snake.WithDependsOn(taskA),
		snake.WithCondition(func(c context.Context, ctx *snake.Context) bool {
			score, ok := snake.GetTyped(ctx, OrderScoreKey)
			return ok && score <= 80
		}),
	)

	// Task D: Downstream of Skipped Task -- Should be Cascade SKIPPED
	taskD := snake.NewTask("notify_low_value", func(c context.Context, ctx *snake.Context) error {
		t.Error("Task D should have been cascade skipped!")
		return nil
	}, snake.WithDependsOn(taskC))

	err := engine.Register(taskA, taskB, taskC, taskD)
	require.NoError(t, err)

	err = engine.Build()
	require.NoError(t, err)

	result, err := engine.Execute(context.Background(), nil)
	require.NoError(t, err)

	// Verify Results
	assert.True(t, result.Success)

	// Task A: Success
	assert.Equal(t, snake.TaskStatusSuccess, result.Reports["analyze"].Status)

	// Task B: Success
	assert.Equal(t, snake.TaskStatusSuccess, result.Reports["process_high_value"].Status)

	// Task C: Skipped
	assert.Equal(t, snake.TaskStatusSkipped, result.Reports["process_low_value"].Status)

	// Task D: Skipped (Cascade)
	assert.Equal(t, snake.TaskStatusSkipped, result.Reports["notify_low_value"].Status)
}
