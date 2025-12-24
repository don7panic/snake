package snake

import (
	"errors"
)

var (
	ErrEmptyTaskID        = errors.New("task ID cannot be empty")
	ErrEmptyTaskHandler   = errors.New("task handler cannot be empty")
	ErrNilTask            = errors.New("task cannot be nil")
	ErrCyclicDependency   = errors.New("cyclic dependency detected")
	ErrMissingDependency  = errors.New("missing dependency")
	ErrNoTasksRegistered  = errors.New("no tasks registered")
	ErrRegisterAfterBuild = errors.New("cannot register after build")
)
