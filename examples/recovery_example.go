package examples

import (
	"context"
	"fmt"
	"time"

	"github.com/don7panic/snake"
)

// DemoRecoveryMiddleware demonstrates how to use the recovery middleware
// to handle panics in task execution gracefully.
func DemoRecoveryMiddleware() {
	// Create an engine with recovery middleware applied globally
	engine := snake.NewEngine()

	// Register recovery middleware globally - it will apply to all tasks
	engine.Use(RecoveryMiddleware())

	// Task 1: A normal task that succeeds
	task1 := snake.NewTask("task1", func(c context.Context, ctx *snake.Context) error {
		fmt.Printf("[%s] Task 1 executing normally\n", ctx.TaskID())
		ctx.SetResult("task1", "task1 completed successfully")
		return nil
	})

	// Task 2: A task that panics with a string
	task2 := snake.NewTask("task2", func(c context.Context, ctx *snake.Context) error {
		fmt.Printf("[%s] Task 2 about to panic\n", ctx.TaskID())
		panic("something went wrong!")
	}, snake.WithDependsOn(task1))

	// Task 3: A task that panics with an error
	task3 := snake.NewTask("task3", func(c context.Context, ctx *snake.Context) error {
		fmt.Printf("[%s] Task 3 about to panic with error\n", ctx.TaskID())
		panic(fmt.Errorf("critical error occurred"))
	}, snake.WithDependsOn(task1))

	// Task 4: A task that would run after task2, but will be skipped due to panic
	task4 := snake.NewTask("task4", func(c context.Context, ctx *snake.Context) error {
		fmt.Printf("[%s] Task 4 executing\n", ctx.TaskID())
		ctx.SetResult("task4", "task4 completed")
		return nil
	}, snake.WithDependsOn(task2))

	// Register all tasks in a batch
	err := engine.Register(task1, task2, task3, task4)
	if err != nil {
		fmt.Printf("Failed to register tasks: %v\n", err)
		return
	}

	if err := engine.Build(); err != nil {
		fmt.Printf("Failed to build DAG: %v\n", err)
		return
	}

	fmt.Println("=== Demo: Global Recovery Middleware ===")
	fmt.Println("Engine will catch panics in any task and continue execution.")

	// Execute the workflow
	result, err := engine.Execute(context.Background(), nil)
	if err != nil {
		fmt.Printf("Execution failed: %v\n", err)
		return
	}

	// Display results
	fmt.Printf("\nExecution completed with Success: %t\n", result.Success)
	fmt.Printf("Execution ID: %s\n", result.ExecutionID)
	fmt.Println("\nTask Results:")

	for taskID, report := range result.Reports {
		status := "SUCCESS"
		if report.Status == snake.TaskStatusFailed {
			status = "FAILED"
			fmt.Printf("  %s: %s - Error: %v\n", taskID, status, report.Err)
		} else {
			fmt.Printf("  %s: %s\n", taskID, status)
		}
	}
}

// DemoTaskSpecificRecovery demonstrates applying recovery middleware to a specific task
func DemoTaskSpecificRecovery() {
	engine := snake.NewEngine()

	// Task 1: Normal task without recovery middleware
	task1 := snake.NewTask("safe-task", func(c context.Context, ctx *snake.Context) error {
		fmt.Printf("[%s] Safe task executing\n", ctx.TaskID())
		ctx.SetResult("safe-task", "safe task result")
		return nil
	})

	// Task 2: Risky task with recovery middleware applied specifically
	task2 := snake.NewTask("risky-task", func(c context.Context, ctx *snake.Context) error {
		fmt.Printf("[%s] Risky task executing\n", ctx.TaskID())
		// Simulate some risky operation that might panic
		panic("unexpected panic in risky operation")
	},
		snake.WithDependsOn(task1),
		snake.WithMiddlewares(RecoveryMiddleware()), // Recovery only for this task
	)

	// Task 3: Another safe task
	task3 := snake.NewTask("another-safe-task", func(c context.Context, ctx *snake.Context) error {
		fmt.Printf("[%s] Another safe task executing\n", ctx.TaskID())
		// This task would be skipped due to task2 failure in Fail-Fast mode
		return nil
	}, snake.WithDependsOn(task2))

	// Register tasks
	err := engine.Register(task1, task2, task3)
	if err != nil {
		fmt.Printf("Failed to register tasks: %v\n", err)
		return
	}

	if err := engine.Build(); err != nil {
		fmt.Printf("Failed to build DAG: %v\n", err)
		return
	}

	fmt.Println("=== Demo: Task-Specific Recovery Middleware ===")
	fmt.Println("Only 'risky-task' has recovery middleware.")

	// Execute
	result, err := engine.Execute(context.Background(), nil)
	if err != nil {
		fmt.Printf("Execution failed: %v\n", err)
		return
	}

	// Display results
	fmt.Printf("\nExecution completed with Success: %t\n", result.Success)
	for taskID, report := range result.Reports {
		fmt.Printf("  %s: %s\n", taskID, report.Status)
	}
}

// DemoRecoveryWithTimeout demonstrates recovery behavior with timeouts
func DemoRecoveryWithTimeout() {
	engine := snake.NewEngine(snake.WithDefaultTaskTimeout(1 * time.Second))

	// Task that panics before timeout
	task1 := snake.NewTask("panic-task", func(c context.Context, ctx *snake.Context) error {
		time.Sleep(100 * time.Millisecond)
		panic("panic before timeout")
	})

	// Task that would timeout (but we're testing panic recovery)
	task2 := snake.NewTask("slow-task", func(c context.Context, ctx *snake.Context) error {
		// This would normally timeout, but let's make it panic first
		time.Sleep(100 * time.Millisecond)
		panic("panic in slow task")
	}, snake.WithDependsOn(task1))

	engine.Use(RecoveryMiddleware())
	err := engine.Register(task1, task2)
	if err != nil {
		fmt.Printf("Failed to register tasks: %v\n", err)
		return
	}

	if err := engine.Build(); err != nil {
		fmt.Printf("Failed to build DAG: %v\n", err)
		return
	}

	fmt.Println("=== Demo: Recovery with Timeouts ===")
	fmt.Println("Demonstrating panic recovery in the context of timeouts.")

	result, err := engine.Execute(context.Background(), nil)
	if err != nil {
		fmt.Printf("Execution failed: %v\n", err)
		return
	}

	fmt.Printf("Execution completed: %t\n", result.Success)
	for taskID, report := range result.Reports {
		if report.Err != nil {
			fmt.Printf("  %s: %s - %v (took %v)\n", taskID, report.Status, report.Err, report.Duration)
		} else {
			fmt.Printf("  %s: %s (took %v)\n", taskID, report.Status, report.Duration)
		}
	}
}
