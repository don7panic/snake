package snake

import (
	"context"
	"fmt"
	"log"
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

// defaultLogger is a simple console-based implementation of the Logger interface
type defaultLogger struct {
	fields []Field
}

// newDefaultLogger creates a new default console logger
func newDefaultLogger() *defaultLogger {
	return &defaultLogger{}
}

// NewDefaultLogger creates a new default console logger (exported for public use)
func NewDefaultLogger() Logger {
	return &defaultLogger{}
}

// Info logs an informational message with the provided context and fields
func (l *defaultLogger) Info(ctx context.Context, msg string, fields ...Field) {
	// Combine existing fields with new fields
	allFields := append(l.fields, fields...)

	// Build the message with fields
	logMsg := fmt.Sprintf("[INFO] %s", msg)
	for _, field := range allFields {
		logMsg += fmt.Sprintf(" %s=%v", field.Key, field.Value)
	}

	log.Println(logMsg)
}

// Error logs an error message with the provided context, error, and fields
func (l *defaultLogger) Error(ctx context.Context, msg string, err error, fields ...Field) {
	// Combine existing fields with new fields
	allFields := append(l.fields, fields...)

	// Build the message with fields
	logMsg := fmt.Sprintf("[ERROR] %s: %v", msg, err)
	for _, field := range allFields {
		logMsg += fmt.Sprintf(" %s=%v", field.Key, field.Value)
	}

	log.Println(logMsg)
}

// With returns a new logger instance with the provided fields added
func (l *defaultLogger) With(fields ...Field) Logger {
	// Create a new logger with combined fields
	newFields := append(l.fields, fields...)
	return &defaultLogger{fields: newFields}
}
