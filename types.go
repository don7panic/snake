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
