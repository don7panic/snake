package snake

// ExecutionResult contains the comprehensive output of a DAG execution
type ExecutionResult struct {
	ExecutionID string
	Success     bool
	Reports     map[string]*TaskReport
	Store       Datastore
	TopoOrder   []string
}

// GetResult retrieves a specific output by key from the Datastore
func (r *ExecutionResult) GetResult(key string) (any, bool) {
	return r.Store.Get(key)
}

// Key represents a strongly-typed key for accessing the Datastore
type Key[T any] struct {
	name string
}

// Name returns the string representation of the key
func (k Key[T]) Name() string {
	return k.name
}

// NewKey creates a new strongly-typed key
func NewKey[T any](name string) Key[T] {
	return Key[T]{name: name}
}
