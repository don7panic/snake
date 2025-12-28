/*
Package snake is a Go-based DAG (Directed Acyclic Graph) execution engine for orchestrating
parallel task execution with dependency management.

Its core components include:
  - Engine: Central orchestrator that manages task registry, DAG construction/validation, and parallel execution.
  - Task: Self-contained execution units with dependencies, handlers, middlewares, and timeouts.
  - Context: Task-level execution context providing datastore access, logging, and middleware chain execution.

Basic usage:

	engine := snake.NewEngine()
	task := snake.NewTask("taskID", handlerFunc)
	engine.Register(task)
	engine.Build()
	engine.Execute(ctx, input)
*/
package snake
