# Implementation Plan

- [x] 1. Set up project structure and core types
  - Create Go module with `go.mod`
  - Define core types: HandlerFunc, TaskStatus, Field
  - Define Logger interface
  - Set up testing framework with testify
  - _Requirements: All requirements depend on these foundations_

- [x] 2. Implement Datastore
  - Create Datastore interface with Set and Get methods
  - Implement MapStore with sync.RWMutex for thread safety
  - _Requirements: 8.4, 8.5, 9.4, 9.5_

- [ ]* 2.1 Write property test for Datastore thread safety
  - **Property 28: Datastore is thread-safe**
  - **Validates: Requirements 8.6**

- [x] 3. Implement Task structure
  - Define Task struct with ID, DependsOn, Handler, Middlewares, Timeout fields
  - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5_

- [x] 4. Implement Context
  - Create Context struct with ctx, TaskID, ExecutionID, StartTime, Store, Logger, handlers, index fields
  - Implement Next() method for middleware chain execution
  - Implement SetResult() method that writes to Datastore using TaskID as key
  - Implement GetResult() method that reads from Datastore
  - Implement Context() method to access underlying context.Context
  - _Requirements: 10.1, 10.2, 10.4, 10.6, 10.7, 10.8, 8.1, 8.2, 8.3_

- [ ]* 4.1 Write unit tests for Context operations
  - Test SetResult writes to correct key
  - Test GetResult reads from correct key
  - Test Next() advances through handler chain
  - _Requirements: 8.2, 8.3, 11.5_

- [ ]* 4.2 Write property test for Context task ID correctness
  - **Property 32: Context contains correct task ID**
  - **Validates: Requirements 10.4**

- [x] 5. Implement Engine options
  - Define Options struct with ErrorStrategy, GlobalTimeout, DefaultTaskTimeout, Logger fields
  - Define Option function type
  - Implement option functions: WithFailFast, WithGlobalTimeout, WithDefaultTaskTimeout, WithLogger
  - Implement NewEngine that applies options
  - _Requirements: 1.2, 1.3, 1.4, 1.5_

- [ ]* 5.1 Write property test for options application
  - **Property 1: Options are applied correctly**
  - **Validates: Requirements 1.1**

- [x] 6. Implement Engine task registration
  - Implement Engine struct with tasks map, middlewares slice, store, logger, opts fields
  - Implement Use() method to register global middleware
  - Implement Register() method with validation (unique ID, non-empty ID, non-nil handler)
  - _Requirements: 1.6, 2.1, 2.2, 2.3, 2.4, 2.5_

- [ ]* 6.1 Write property test for task registration
  - **Property 2: Task registration stores tasks**
  - **Validates: Requirements 2.1**

- [ ]* 6.2 Write property test for duplicate ID rejection
  - **Property 3: Duplicate task IDs are rejected**
  - **Validates: Requirements 2.2**

- [ ]* 6.3 Write property test for empty ID rejection
  - **Property 4: Empty task IDs are rejected**
  - **Validates: Requirements 2.3**

- [ ]* 6.4 Write property test for nil handler rejection
  - **Property 5: Nil handlers are rejected**
  - **Validates: Requirements 2.4**

- [x] 7. Implement DAG building
  - Add adj (adjacency list) and indegree maps to Engine
  - Implement Build() method that constructs adjacency list from task dependencies
  - Calculate indegree for each task
  - _Requirements: 3.1, 3.2_

- [ ]* 7.1 Write property test for adjacency list correctness
  - **Property 6: Adjacency list matches dependencies**
  - **Validates: Requirements 3.1**

- [ ]* 7.2 Write property test for indegree calculation
  - **Property 7: Indegree matches dependency count**
  - **Validates: Requirements 3.2**

- [x] 8. Implement DAG validation
  - Implement cycle detection using Kahn algo 
  - Implement the topological sort
  - Implement dependency existence validation
  - Return detailed errors with task IDs involved
  - _Requirements: 3.3, 3.4, 3.5_

- [ ]* 8.1 Write property test for cycle detection
  - **Property 8: Cycles are detected**
  - **Validates: Requirements 3.3**

- [ ]* 8.2 Write property test for missing dependency detection
  - **Property 9: Missing dependencies are detected**
  - **Validates: Requirements 3.4**

- [ ]* 8.3 Write property test for valid DAG acceptance
  - **Property 10: Valid DAGs pass validation**
  - **Validates: Requirements 3.6**

- [x] 9. Implement execution initialization
  - Generate unique ExecutionID (use UUID or timestamp-based ID)
  - Create new Datastore instance
  - Initialize pendingDeps map with atomic.Int32 for each task
  - Create ready queue channel
  - Identify and queue all zero-indegree tasks
  - _Requirements: 4.1, 4.2, 4.3, 4.4_

- [ ]* 9.1 Write property test for execution ID uniqueness
  - **Property 11: Execution IDs are unique**
  - **Validates: Requirements 4.1**

- [ ]* 9.2 Write property test for Datastore initialization
  - **Property 12: Datastore starts empty**
  - **Validates: Requirements 4.2**

- [ ]* 9.3 Write property test for pending count initialization
  - **Property 13: Pending counts match indegree**
  - **Validates: Requirements 4.3**

- [ ]* 9.4 Write property test for zero-indegree task identification
  - **Property 14: Zero-indegree tasks start first**
  - **Validates: Requirements 4.4**

- [x] 10. Implement task execution
  - Create task Context with execution context, task metadata, Datastore, Logger
  - Apply task timeout if configured, otherwise use default timeout
  - Build handler chain: global middlewares + task middlewares + handler
  - Execute handler chain starting from index 0
  - Record execution in TaskReport (status, error, timing)
  - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 10.7, 10.8, 11.3_

- [ ]* 10.1 Write property test for Context creation
  - **Property 29: Each task gets unique Context**
  - **Validates: Requirements 10.1**

- [ ]* 10.2 Write property test for context inheritance
  - **Property 30: Context inherits execution context**
  - **Validates: Requirements 10.2**

- [ ]* 10.3 Write property test for task timeout
  - **Property 31: Task timeout cancels context**
  - **Validates: Requirements 10.3**

- [ ]* 10.4 Write property test for handler chain order
  - **Property 38: Handler chain order is correct**
  - **Validates: Requirements 11.3**

- [x] 11. Implement parallel task scheduling
  - Spawn goroutine for each ready task
  - Use WaitGroup to track active tasks
  - On task completion, atomically decrement pendingDeps for dependents
  - Add dependents with zero pendingDeps to ready queue
  - _Requirements: 5.1, 5.2, 5.3, 5.4_

- [ ]* 11.1 Write property test for concurrent scheduling
  - **Property 17: Ready tasks execute concurrently**
  - **Validates: Requirements 5.1**

- [ ]* 11.2 Write property test for dependency count decrement
  - **Property 18: Completion decrements dependent counts**
  - **Validates: Requirements 5.2**

- [ ]* 11.3 Write property test for task readiness
  - **Property 19: Zero pending count triggers scheduling**
  - **Validates: Requirements 5.3**

- [ ]* 11.4 Write property test for goroutine per task
  - **Property 20: Tasks execute in separate goroutines**
  - **Validates: Requirements 5.4**

- [x] 12. Implement Fail-Fast error handling
  - On task error, mark task as FAILED in TaskReport
  - Cancel execution-level context
  - Mark all pending (not started) tasks as SKIPPED
  - Continue until all active tasks complete or are cancelled
  - _Requirements: 6.1, 6.2, 6.3, 6.4_

- [ ]* 12.1 Write property test for failure marking
  - **Property 22: Failed tasks are marked FAILED**
  - **Validates: Requirements 6.1**

- [ ]* 12.2 Write property test for context cancellation
  - **Property 23: Failure cancels execution context**
  - **Validates: Requirements 6.2**

- [ ]* 12.3 Write property test for skipped task marking
  - **Property 24: Unstarted tasks are marked SKIPPED**
  - **Validates: Requirements 6.3**

- [x] 13. Implement execution completion and result
  - Wait for all tasks to reach terminal state using WaitGroup
  - Create ExecutionResult with ExecutionID, Success, Reports, Store
  - Determine Success based on all tasks having SUCCESS status
  - Implement GetResult() method on ExecutionResult
  - _Requirements: 4.6, 5.5, 6.5, 13.5, 13.6, 14.1, 14.2, 14.3, 14.4, 14.5, 14.6_

- [ ]* 13.1 Write property test for execution completion
  - **Property 21: Execution completes when all tasks terminate**
  - **Validates: Requirements 5.5**

- [ ]* 13.2 Write property test for failure result
  - **Property 25: Failure result contains error details**
  - **Validates: Requirements 6.5**

- [ ]* 13.3 Write property test for result structure
  - **Property 16: Execution result contains required fields**
  - **Validates: Requirements 4.6**

- [ ]* 13.4 Write property test for success determination
  - **Property 46: Success is true when all tasks succeed**
  - **Validates: Requirements 14.2**

- [ ]* 13.5 Write property test for failure determination
  - **Property 47: Success is false when any task fails**
  - **Validates: Requirements 14.3**

- [x] 14. Implement task status tracking
  - Initialize all tasks with PENDING status
  - Update to SUCCESS when handler returns nil
  - Update to FAILED when handler returns error
  - Update to SKIPPED when cancelled before execution
  - Create TaskReport with all required fields
  - _Requirements: 13.1, 13.2, 13.3, 13.4, 13.5, 13.6_

- [ ]* 14.1 Write property test for initial status
  - **Property 41: Initial status is PENDING**
  - **Validates: Requirements 13.1**

- [ ]* 14.2 Write property test for success status
  - **Property 42: Successful tasks are marked SUCCESS**
  - **Validates: Requirements 13.2**

- [ ]* 14.3 Write property test for task report structure
  - **Property 43: Task reports contain required fields**
  - **Validates: Requirements 13.5**

- [ ]* 14.4 Write property test for report completeness
  - **Property 44: All tasks have reports**
  - **Validates: Requirements 13.6**

- [ ] 15. Implement timeout handling
  - Apply default timeout from Engine options when task timeout not set
  - Wrap task context with timeout using context.WithTimeout
  - Handle deadline exceeded errors as task failures
  - Mark timed-out tasks as FAILED with timeout error
  - _Requirements: 15.1, 15.2, 15.3, 15.4, 15.5_

- [ ]* 15.1 Write property test for default timeout
  - **Property 49: Default timeout is used when task timeout is unset**
  - **Validates: Requirements 15.2**

- [ ]* 15.2 Write property test for timeout cancellation
  - **Property 50: Timeout cancels context with deadline error**
  - **Validates: Requirements 15.3**

- [ ]* 15.3 Write property test for timeout failure
  - **Property 51: Timeout results in FAILED status**
  - **Validates: Requirements 15.4**

- [ ] 16. Implement middleware execution chain
  - Store handlers slice in Context
  - Implement Next() to advance index and call next handler
  - Ensure global middleware executes for all tasks
  - Ensure task middleware executes only for that task
  - Verify execution order and reverse order after Next()
  - _Requirements: 11.1, 11.2, 11.3, 11.4, 11.5, 11.6, 11.7_

- [ ]* 16.1 Write property test for global middleware
  - **Property 36: Global middleware applies to all tasks**
  - **Validates: Requirements 11.1**

- [ ]* 16.2 Write property test for task middleware
  - **Property 37: Task middleware applies only to that task**
  - **Validates: Requirements 11.2**

- [ ]* 16.3 Write property test for Next invocation
  - **Property 39: Next invokes next handler**
  - **Validates: Requirements 11.5**

- [ ]* 16.4 Write property test for reverse execution
  - **Property 40: Middleware executes in reverse on return**
  - **Validates: Requirements 11.6**

- [ ] 17. Implement Logger integration
  - Create child Logger with TaskID and ExecutionID fields for each task
  - Ensure Context.Logger includes task metadata
  - Verify logs include TaskID and ExecutionID automatically
  - _Requirements: 16.1, 16.2, 16.3, 16.4, 16.5_

- [ ]* 17.1 Write property test for logger metadata
  - **Property 33: Context logger has task metadata**
  - **Validates: Requirements 10.6**

- [ ]* 17.2 Write property test for log field inclusion
  - **Property 52: Logs include task metadata**
  - **Validates: Requirements 16.5**

- [ ] 18. Implement disconnected graph support
  - Ensure all zero-indegree tasks are identified regardless of connectivity
  - Execute all subgraphs in the same execution
  - Ensure Fail-Fast cancels all subgraphs
  - Verify all tasks in all subgraphs reach terminal states
  - _Requirements: 4.5, 17.1, 17.2, 17.3, 17.4, 17.5_

- [ ]* 18.1 Write property test for disconnected graph execution
  - **Property 15: Disconnected graphs execute completely**
  - **Validates: Requirements 4.5**

- [ ]* 18.2 Write property test for cross-subgraph cancellation
  - **Property 53: Failure in one subgraph cancels all subgraphs**
  - **Validates: Requirements 17.4**

- [ ] 19. Implement remaining Context properties
  - Verify Context contains Datastore reference
  - Verify SetResult uses task ID as key
  - Verify Context contains ExecutionID
  - Verify Context has start time
  - Verify Result provides Datastore access
  - _Requirements: 8.1, 8.2, 10.7, 10.8, 14.5_

- [ ]* 19.1 Write property test for Datastore reference
  - **Property 26: Context contains Datastore reference**
  - **Validates: Requirements 8.1**

- [ ]* 19.2 Write property test for SetResult key
  - **Property 27: SetResult uses task ID as key**
  - **Validates: Requirements 8.2**

- [ ]* 19.3 Write property test for ExecutionID in Context
  - **Property 34: Context contains execution ID**
  - **Validates: Requirements 10.7**

- [ ]* 19.4 Write property test for start time
  - **Property 35: Context has start time**
  - **Validates: Requirements 10.8**

- [ ]* 19.5 Write property test for result Datastore access
  - **Property 48: Result provides Datastore access**
  - **Validates: Requirements 14.5**

- [ ]* 19.6 Write property test for result ExecutionID
  - **Property 45: Result contains execution ID**
  - **Validates: Requirements 14.1**

- [ ] 20. Create example recovery middleware
  - Implement recovery middleware that catches panics
  - Convert panics to errors
  - Log panic details with stack trace
  - Demonstrate middleware usage in examples
  - _Requirements: 12.1, 12.2, 12.3, 12.4, 12.5_

- [ ]* 20.1 Write unit tests for recovery middleware
  - Test panic recovery
  - Test error conversion
  - Test logging behavior
  - _Requirements: 12.1, 12.2, 12.3, 12.4_

- [ ] 21. Final checkpoint - Ensure all tests pass
  - Run all unit tests
  - Run all property tests with race detector
  - Verify test coverage
  - Ensure all tests pass, ask the user if questions arise
