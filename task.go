package snake

import (
	"context"
	"time"
)

// TaskStatus represents the execution state of a task
type TaskStatus string

const (
	TaskStatusSuccess   TaskStatus = "SUCCESS"
	TaskStatusFailed    TaskStatus = "FAILED"
	TaskStatusSkipped   TaskStatus = "SKIPPED"
	TaskStatusCancelled TaskStatus = "CANCELLED"
)

// ConditionFunc is a function that determines if a task should be executed
type ConditionFunc func(c context.Context, ctx *Context) bool

// Task represents a single executable unit in the workflow
type Task struct {
	id           string
	dependsOn    []string
	handler      HandlerFunc
	middlewares  []HandlerFunc
	timeout      time.Duration
	condition    ConditionFunc
	allowFailure bool
}

// NewTask creates a new Task with the given ID and handler, applying any provided options
func NewTask(id string, handler HandlerFunc, options ...TaskOption) *Task {
	task := &Task{
		id:           id,
		handler:      handler,
		dependsOn:    []string{},
		middlewares:  []HandlerFunc{},
		timeout:      0, // zero means no timeout
		allowFailure: false,
	}

	for _, opt := range options {
		opt(task)
	}

	return task
}

// WithDependsOn sets the dependencies for a task
func WithDependsOn(deps ...*Task) TaskOption {
	return func(t *Task) {
		for _, dep := range deps {
			if dep != nil {
				t.dependsOn = append(t.dependsOn, dep.id)
			}
		}
	}
}

// WithTimeout sets the timeout for a task
func WithTimeout(timeout time.Duration) TaskOption {
	return func(t *Task) {
		t.timeout = timeout
	}
}

// WithMiddlewares sets the middlewares for a task
func WithMiddlewares(middlewares ...HandlerFunc) TaskOption {
	return func(t *Task) {
		t.middlewares = append(t.middlewares, middlewares...)
	}
}

// WithCondition sets the condition function for a task
func WithCondition(cond ConditionFunc) TaskOption {
	return func(t *Task) {
		t.condition = cond
	}
}

// WithAllowFailure sets whether the task is allowed to fail without cancelling the workflow
func WithAllowFailure(allow bool) TaskOption {
	return func(t *Task) {
		t.allowFailure = allow
	}
}

// TaskOption is a function that configures an Task
type TaskOption func(*Task)

// TaskReport contains execution information for a single task
type TaskReport struct {
	TaskID    string
	Status    TaskStatus
	Err       error
	StartTime time.Time
	EndTime   time.Time
	Duration  time.Duration
}
