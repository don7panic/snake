# snake

一个轻量的 Golang 进程内 DAG 任务编排框架。显式依赖、自动数据流转、并行调度，可插拔中间件（recovery / tracing / logging / metrics）。

## 特性

- 显式依赖建模：用 TaskID 声明上下游关系，Engine 自动拓扑调度并并行执行。
- 数据共享：内置并发安全的 Datastore，任务用 `ctx.SetResult`/`ctx.GetResult` 读写数据。
- 中间件链：与 gin 类似的 `HandlerFunc` 链，支持全局和任务级中间件。
- 超时与 Fail-Fast：全局超时（`WithGlobalTimeout`）和任务级/默认超时；任务失败触发 Fail-Fast。
- 排障友好：`ExecutionResult` 返回任务报告与拓扑序 `TopoOrder`，便于检查执行顺序。
- 单实例单次执行：一个 Engine 只允许调用一次 `Execute`（不可并发/重复）。

## 快速开始

```go
package main

import (
    "context"
    "fmt"

    "snake"
)

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
        ctx.SetResult("a-value")
        return nil
    })
    b := snake.NewTask("B", func(c context.Context, ctx *snake.Context) error {
        v, _ := ctx.GetResult("A")
        ctx.SetResult(fmt.Sprintf("b got %v", v))
        return nil
    }, snake.WithDependsOn("A"))

    if err := engine.Register(a, b); err != nil {
        panic(err)
    }

    // 确认 DAG，无环且依赖齐全
    if err := engine.Build(); err != nil {
        panic(err)
    }

    result, err := engine.Execute(context.Background())
    if err != nil {
        panic(err)
    }

    fmt.Println("success:", result.Success)
    fmt.Println("topo:", result.TopoOrder) // e.g. [A B]

    if v, ok := result.GetResult("B"); ok {
        fmt.Println("B result:", v)
    }
}
```

## 任务与中间件

- 任务 Handler 签名：`func(c context.Context, ctx *snake.Context) error`。
- 在 handler 内通过 `ctx.Next(c)` 进入链路的下一个 handler；业务 handler 放在链尾。
- 任务级别可自定义超时：`snake.WithTimeout(d)`。

## 运行与限制

- 推荐在并发 `Execute` 前调用一次 `Build` 做依赖/环校验；`Execute` 可并发调用，每次使用 DAG 快照和独立的 Datastore。
- 每次执行都会使用干净的 Datastore；自定义 Datastore 需实现 `Init()` 返回新实例。
- 遇到第一个任务失败（或超时）即触发 Fail-Fast，未开始的任务标记为 `SKIPPED`。

## 观察与排障

- `ExecutionResult.Reports` 包含每个任务的状态/错误/耗时。
- `ExecutionResult.TopoOrder` 返回拓扑排序，便于确认执行顺序/依赖。
- 可通过中间件扩展日志、Tracing、Metrics、限流等。

## 开发与测试

```bash
go test ./...
```

更多设计细节见 `docs/ARCHITECTURE.md`，示例见 `examples/`。
