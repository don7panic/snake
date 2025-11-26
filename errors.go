package snake

import (
	"errors"
)

var (
	ErrEmptyTaskID       = errors.New("task ID cannot be empty")
	ErrEmptyTaskHandler  = errors.New("task handler cannot be empty")
	ErrTaskAlreadyExists = errors.New("task already exists")
	ErrCyclicDependency  = errors.New("cyclic dependency detected")
	ErrMissingDependency = errors.New("missing dependency")
)
