package snake

import (
	"context"
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

// Field represents a structured logging field
type Field struct {
	Key   string
	Value any
}

// Logger defines the interface for logging within the framework
type Logger interface {
	Info(ctx context.Context, msg string, fields ...Field)
	Error(ctx context.Context, msg string, err error, fields ...Field)
	With(fields ...Field) Logger
}

// Task represents a single executable unit in the workflow
type Task struct {
	ID          string
	DependsOn   []string
	Handler     HandlerFunc
	Middlewares []HandlerFunc
	Timeout     time.Duration
}
