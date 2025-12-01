package snake

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
