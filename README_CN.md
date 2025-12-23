# snake

一个轻量的 Golang 进程内 DAG 任务编排框架。显式依赖、自动数据流转、并行调度，可插拔中间件（recovery / tracing / logging / metrics）。

```bash
go get github.com/don7panic/snake
```

## 特性

- 显式依赖建模：用 TaskID 声明上下游关系，Engine 自动拓扑调度并并行执行。
- 数据共享：内置并发安全的 Datastore，任务用 `ctx.SetResult`/`ctx.GetResult` 读写数据；key 可用 TaskID 或自定义字符串，方便一个任务写多个结果，甚至跨任务共享同名 key（需业务自控覆盖语义）。
- 中间件链：与 gin 类似的 `HandlerFunc` 链，支持全局和任务级中间件。
- 错误处理策略：支持 `FailFast`（默认，一挂全挂）和 `RunAll`（尽力而为，运行所有无关任务）。
- 容错机制：任务级 `WithAllowFailure(true)`，允许特定任务失败而不中断整个工作流。
- 超时控制：局超时（`WithGlobalTimeout`）和任务级/默认超时。
- 排障友好：`ExecutionResult` 返回任务报告与拓扑序 `TopoOrder`，便于检查执行顺序。
- 可复用的 Engine：一次构建 DAG 后可多次、并发执行，每次 `Execute` 接受独立的输入参数。

## 快速开始

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/don7panic/snake"
)

// 定义输入类型
type WorkflowInput struct {
    Value string
}

func main() {
    engine := snake.NewEngine(
        snake.WithGlobalTimeout(30*time.Second),
        snake.WithDefaultTaskTimeout(2*time.Second),
    )

    // 全局中间件，例如 panic recovery
    engine.Use(func(c context.Context, ctx *snake.Context) error {
        fmt.Println("start", ctx.TaskID())
        err := ctx.Next(c)
        fmt.Println("done", ctx.TaskID(), "err:", err)
        return err
    })

    a := snake.NewTask("A", func(c context.Context, ctx *snake.Context) error {
        // 从 Context 获取输入参数（Input 为本次 Execute 的只读快照）
        input, ok := ctx.Input().(*WorkflowInput)
        if !ok {
            return fmt.Errorf("invalid input type")
        }

        // 使用输入参数处理业务逻辑
        result := fmt.Sprintf("processed: %s", input.Value)
        ctx.SetResult("A", result)
        return nil
    })

	b := snake.NewTask("B", func(c context.Context, ctx *snake.Context) error {
		// 读取上游任务结果
		v, _ := ctx.GetResult("A")
		ctx.SetResult("B", fmt.Sprintf("b got %v", v))
		return nil
	}, snake.WithDependsOn(a))

    if err := engine.Register(a, b); err != nil {
        panic(err)
    }

    // 确认 DAG，无环且依赖齐全
    if err := engine.Build(); err != nil {
        panic(err)
    }

    // 执行时传入输入参数
    input := &WorkflowInput{Value: "hello"}
    result, err := engine.Execute(context.Background(), input)
    if err != nil {
        panic(err)
    }

    fmt.Println("success:", result.Success)
    fmt.Println("topo:", result.TopoOrder) // e.g. [A B]

    if v, ok := result.GetResult("B"); ok {
        fmt.Println("B result:", v)
    }
    
    // Engine 可以用不同的输入多次执行
    input2 := &WorkflowInput{Value: "world"}
    result2, err := engine.Execute(context.Background(), input2)
    if err != nil {
        panic(err)
    }
    fmt.Println("second execution success:", result2.Success)
}
```

## 任务与中间件

- 任务 Handler 签名：`func(c context.Context, ctx *snake.Context) error`。
- 在 handler 内通过 `ctx.Next(c)` 进入链路的下一个 handler；业务 handler 放在链尾。
- 任务级别可自定义超时：`snake.WithTimeout(d)`。

## 运行与限制

- 推荐在并发 `Execute` 前调用一次 `Build` 做依赖/环校验；`Execute` 可并发调用，每次使用 DAG 快照和独立的 Datastore。
- **新特性**：`Execute` 方法现在接受输入参数，允许同一个 Engine 用不同的输入多次执行。
- 每次执行都会通过 `DatastoreFactory` 创建全新的 Datastore 实例；可通过 `WithDatastoreFactory` 注入自定义工厂函数。
- Handler 通过 `ctx.Input()` 访问本次执行传入的输入参数（视为只读引用，避免在并行任务中原地修改），需要进行类型断言；如需变更请先拷贝。
- 遇到第一个任务失败（或超时）且策略为 FailFast 时触发全局取消，未开始的任务标记为 `CANCELLED`。
- 如果策略为 `RunAll`，则失败任务仅影响其下游依赖，其他独立分支继续运行。
- 如果策略为 `RunAll`，则失败任务仅影响其下游依赖，其他独立分支继续运行。
- 如果任务标记为 `WithAllowFailure(true)`，即使失败也会被视为“已处理”，下游任务视逻辑决定是否运行。

## 高级特性

### 条件执行

使用 `WithCondition` 根据运行时条件跳过任务。

```go
check := snake.NewTask("check", func(c context.Context, ctx *snake.Context) error {
    // ... 业务逻辑 ...
    return nil
}, snake.WithCondition(func(c context.Context, ctx *snake.Context) bool {
    // 返回 true 执行任务，false 跳过
    return shouldRun()
}))
```

被跳过的任务状态为 `SKIPPED`。下游依赖任务通常仍会运行，除非它们强依赖于被跳过任务的输出。

### 并发控制

使用 `WithMaxConcurrency` 限制并行执行的任务数量。

```go
engine := snake.NewEngine(
    snake.WithMaxConcurrency(5), // 最多同时运行 5 个任务
)
```

### 中间件与恢复

Snake 支持类似 Web 框架的中间件链。常见场景是 Panic 恢复。

```go
// 添加 Recovery 中间件以安全处理任务中的恐慌
engine.Use(snake.Recovery())
```

### 任务选项

`NewTask` 的完整选项列表：

- `WithDependsOn(tasks ...*Task)`: 声明上游依赖。
- `WithTimeout(d time.Duration)`: 设置任务级硬超时。
- `WithCondition(fn ConditionFunc)`: 动态跳过逻辑。
- `WithAllowFailure(allow bool)`: 如果为 true，即使失败也不会触发 FailFast 取消。
- `WithMiddlewares(m ...HandlerFunc)`: 添加任务级中间件。


## 观察与排障

- `ExecutionResult.Reports` 包含每个任务的状态/错误/耗时。
- `ExecutionResult.TopoOrder` 返回拓扑排序，便于确认执行顺序/依赖。
- 可通过中间件扩展日志、Tracing、Metrics、限流等。

## 开发与测试

```bash
# 运行测试
go test ./...

# 查看测试覆盖率
go test -v -cover ./...

# 格式化代码
go fmt ./...

# 静态检查
go vet ./...
```

## 安装

```bash
go get github.com/don7panic/snake
```

## 示例

更多使用示例见 `examples/` 目录：
- `examples/recovery_example.go` - 恢复中间件示例
- `examples/custom_store_test.go` - 自定义数据存储示例
- `examples/e2e/order_workflow.go` - 端到端订单处理示例

## 架构设计

详细架构说明见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

## 贡献

欢迎提交 Issue 和 Pull Request！

## 许可证

MIT License
