package snake

import (
	"time"
)

// HandlerFunc is the unified function signature for both middleware and task handlers
type HandlerFunc func(ctx *Context) error

// TaskStatus represents the execution state of a task
type TaskStatus string

const (
	TaskStatusPending TaskStatus = "PENDING"
	TaskStatusSuccess TaskStatus = "SUCCESS"
	TaskStatusFailed  TaskStatus = "FAILED"
	TaskStatusSkipped TaskStatus = "SKIPPED"
)

// Task represents a single executable unit in the workflow
type Task struct {
	ID          string
	DependsOn   []string
	Handler     HandlerFunc
	Middlewares []HandlerFunc
	Timeout     time.Duration
}

// TaskReport contains execution information for a single task
type TaskReport struct {
	TaskID    string
	Status    TaskStatus
	Err       error
	StartTime time.Time
	EndTime   time.Time
	Duration  time.Duration
}

// ExecutionResult contains the comprehensive output of a DAG execution
type ExecutionResult struct {
	ExecutionID string
	Success     bool
	Reports     map[string]*TaskReport
	Store       Datastore
}

// GetResult retrieves a specific task's output by Task ID from the Datastore
func (r *ExecutionResult) GetResult(taskID string) (any, bool) {
	return r.Store.Get(taskID)
}
