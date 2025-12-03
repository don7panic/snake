# snake：Golang 进程内任务编排框架设计文档（MVP）

## 1. 总体目标与定位

snake 是一个 进程内任务编排框架，用 有向无环图（DAG） 描述任务间依赖关系，并具备：

- 显式依赖建模：每个任务声明依赖的其他任务；
- 自动数据流转：任务结果通过 Datastore 在依赖链路上传递；
- 并行执行能力：根据 DAG 与依赖情况自动进行并行调度；
- 可插拔 Middleware：参考 gin 的 middleware 模型，为任务执行链路提供统一的扩展点（recovery / tracing / logging / metrics 等）。

整体工作流：

1. 创建 Engine，通过 option 注入配置（并发度、日志、错误策略等）。
2. 定义 Task，在 Task 中声明依赖关系，并注册到 Engine。
3. Engine 构建并校验 DAG（cycle 检测、依赖存在性等）。
4. Engine 执行 DAG，创建任务级 Context，调度任务并行执行，通过 Datastore 传递数据。
5. 获取执行结果（整体状态 + 指定任务输出 + 每个 task 的执行报告）。

---

## 2. Engine：图构建 & 调度核心

### 2.1 Engine 的职责

Engine 是 snake 的核心，负责：

1. 任务图构建与管理
   - 维护所有注册的 Task 节点；
   - 构建任务间的 DAG 结构（邻接表、入度、拓扑序等）；
   - 支持非连通图（森林结构）：一次执行中可以有多个入度为 0 的起点任务。

2. 图校验
   - 检测循环依赖（保证 DAG 无环）；
   - 校验依赖存在性（依赖的 Task 是否已注册）；
   - 可选：根据 Task 的声明，做输出/输入类型或数据契约的静态检查（MVP 可暂缓）。

3. 执行调度
   - 基于拓扑排序 / 入度算法管理「就绪任务队列」；
   - 使用 原子操作维护 Task 的剩余依赖数（indegree），支持并发调度；
   - 为每个任务构造 任务级 Context 并执行 middleware + handler 链；
   - 维护每个任务的执行状态（成功、失败、跳过/未执行等）。
   - `Build()` 为公开方法，推荐在并发 `Execute()` 前调用一次确认 DAG；`Execute()` 支持并发调用，每次基于 DAG 快照和独立的 Datastore 运行。

4. 共享资源管理
   - 持有全局 Datastore 实例；
   - 持有全局 Logger、全局中间件列表、执行策略配置等。

### 2.2 Engine 的关键配置（通过 options）

Engine 在创建时可以通过 options 注入配置，包括但不限于：

1. 错误处理策略
   - MVP 仅支持 Fail-Fast：
     - 任一任务失败，立刻取消整个执行（通过取消执行级 `context.Context` 实现）。
   - 失败会导致：
     - 当前 Task 状态为 Failed；
     - 尚未开始执行的 Task 标记为 Skipped（或 Canceled，需在文档中约定）。

2. 超时与取消
   - 全局执行超时时间：在外层 `context.Context` 上控制；
   - 默认 Task 超时时间：可在 Engine 级配置，在具体 Task 级别覆盖；
   - 任务级 Context 会基于执行级 ctx 派生子 ctx，加上 Task 级 timeout。

3. 日志与可观测性
   - 自定义 Logger 接口；
   - 全局 tracing / metrics 开关或抽象接口（通过 middleware 实现）。

4. 中间件配置
   - 全局 middleware 列表（类似 `gin.Engine.Use(...)`），对所有 Task 生效；
   - MVP 可不实现「按标签筛选中间件」，但可以预留 Task 标签字段。

5. 并发调度策略（MVP 约定）
   - 基于每个 Task 的 indegree 原子计数 + 就绪队列实现并行执行；
   - MVP 阶段不限制最大并发度：每个就绪 Task 可直接启动一个 goroutine；
   - 后续如需限制并发度，可在执行 Task 前增加信号量 / worker pool，不影响现有 API。

### 2.3 Engine 对外 API 草图（建议）

```go
type Engine struct {
    tasks        map[string]*Task
    middlewares  []HandlerFunc
    storeFactory DatastoreFactory
    opts         Options
}

type Option func(*Options)

func NewEngine(opts ...Option) *Engine

// 注册 task，要求 ID 唯一，重复注册返回错误。
func (e *Engine) Register(task *Task) error

// 执行整个 DAG
func (e *Engine) Execute(ctx context.Context) (*ExecutionResult, error)

// Execute 会内部构建并校验 DAG，并返回拓扑序（TopoOrder）用于排障。
// Engine 支持并发 Execute 调用，每次执行使用独立的 Datastore 实例。
```

---

## 3. Task：任务节点和依赖关系

### 3.1 Task 的职责

Task 是 DAG 中的一个 节点，代表一个可执行的业务单元，负责：

- 实现具体业务逻辑（如调用下游服务、读写数据库、计算数据等）；
- 明确 产出的数据结果 写入 Datastore；
- 声明 依赖的其他 Task，框架根据依赖自动调度；
- 可选：定义自己特有的执行配置（超时、重试策略、自带的 middleware 等）。

### 3.2 Task 的核心要素

```go
type Task struct {
    ID        string
    DependsOn []string

    Handler   HandlerFunc
    Middlewares []HandlerFunc // 只作用于本 Task 的链路（在全局中间件之后）

    Timeout   time.Duration   // 可选，覆盖默认 Task 超时
    Retry     *RetryPolicy    // MVP 可选实现
}
```

1. 唯一标识
   - 每个 Task 有一个 唯一 ID（string），用于：
     - 声明依赖：通过 ID 引用其他 Task；
     - 作为 Datastore 中结果的 key；
     - 在日志、trace、metrics 中标识任务。

2. 依赖列表
   - 明确声明 `DependsOn: []TaskID`；
   - Engine 在构图时将这些依赖转为有向边 `DependsOn -> 当前 Task`；
   - 依赖语义：
     - 执行顺序约束：只有当依赖全部成功完成后，当前 Task 才会被调度执行（Fail-Fast 时，一旦上游失败则不会调度）；
     - 数据可见性：当前 Task 可以从 Datastore 中读取依赖 Task 的输出结果。

3. 处理函数（Handler）
   - 签名统一为：

    ```go
    type HandlerFunc func(c context.Context, ctx *Context) error
    ```

   - Handler 负责：
     - 通过 Context 读取依赖数据；
     - 执行业务逻辑；
     - 把结果写回自己的 TaskID 对应的 Datastore 条目中；
     - 返回成功/失败状态（error）。
   - 实际执行时，Handler 会被包裹在一条链路里：
     - 全局 middleware + Task 级 middleware + 最终业务 handler。

4. Task 级配置（可选）
   - 超时时间（覆盖 Engine 默认 Task 超时时间）；
   - 重试策略（最大重试次数、退避策略）——MVP 可以简单实现或先预留字段；
   - Task 标签（用于中间件筛选、日志分组等）；
   - Task 级别 middleware（只作用于该 Task）。

---

## 4. Datastore：数据流与结果存储

### 4.1 Datastore 的职责

Datastore 是 任务间数据流转的载体，负责：

- 存储每个 Task 的执行结果（典型是 `TaskID -> Result` 映射）；
- 在 Task 之间 传递数据：下游 Task 能读到上游依赖 Task 的输出；
- 提供必要的 并发安全 支持（多 Task 并行读写）。

### 4.2 数据流设计原则

- 单写多读
  - 每个 Task 只负责写自己的结果（使用自己的 TaskID 作为 key）；
  - 任何 Task 不能写别的 Task 的结果；
  - 防止数据被其他 Task 意外覆盖。

### 4.3 实现与并发一致性（MVP）

- Datastore 实现是：`map[string]any + sync.RWMutex`：
  - 写入时加写锁；
  - 读取时加读锁；
- Engine 通过 DatastoreFactory 创建 Datastore 实例；
- 每次 `Execute` 时都会通过工厂创建一份全新的 Datastore，避免跨执行串数据；
- 各任务 Context 的 `Datastore` 字段只是指向同一份底层实现的引用；
- 提供基础接口示例：

  ```go
  type Datastore interface {
      Set(taskID string, value any)
      Get(taskID string) (value any, ok bool)
  }

  // DatastoreFactory 用于为每次执行创建新的 Datastore 实例
  type DatastoreFactory func() Datastore
  ```

  MVP 中可以是简单实现：

  ```go
  type MapStore struct {
      mu  sync.RWMutex
      data map[string]any
  }
  ```

---

## 5. Context：任务级执行上下文

### 5.1 Context 的定位

Context 表示 某个 Task 在某次 Engine.Execute 调用中的一次执行上下文。主要目的：

- 封装标准库 `context.Context`，传递取消/超时信号；
- 暴露当前 Task 可访问的 Datastore 视图；
- 暴露已绑定 Task 信息的 Logger / Tracer / Metrics handle 等；
- 为 middleware 和 handler 提供统一的访问入口，类似 gin.Context。

### 5.2 Context 的层级区分

1. 执行级（workflow 级）上下文
   - 由用户传给 `Engine.Execute(ctx)` 中的 ctx；
   - 表示这次编排执行的整体生命周期；
   - 用于全局超时 / cancel 控制；
   - 在内部被传播到各个 Task 的 Context（作为父 context）。
   - 每个 Engine 实例仅允许调用一次 `Execute`，不支持并发或重复执行。

2. 任务级 Context（snake 自己的 Context 结构）

   ```go
   type Context struct {
       // 标准库上下文，包含取消/超时
       ctx context.Context

       // 当前 Task 信息
       TaskID   string

       // 数据访问
       Store    Datastore

       // 日志 / 观测
       Logger   Logger

       // 中间件调度相关
       handlers []HandlerFunc
       index    int

       // 执行元数据
       ExecutionID string
       StartTime   time.Time
   }
   ```

   - 每个 Task 被调度执行时，Engine 为它 创建一个独立的 Context 实例；
   - 不在不同 Task 之间共享，避免并发写共享字段的问题；
   - 执行级 `context.Context` 通常是加了 Task 级超时等的子上下文。

### 5.3 Context 与 Datastore 的关系

- 每个任务级 Context 中都持有一个 Datastore 接口/视图的引用；
- 这些引用底层都指向同一个执行级的 Datastore 实现实例；
- Datastore 的 key 设计为开放的字符串空间：
  - 默认主结果可直接用 TaskID 作为 key；
  - 也可以自定义 key（如 `taskID:subKey`）存储多个结果；
  - 不限制不同 Task 是否可以写同名 key，覆盖/共享语义由业务自控。
- Task 通过 Context 操作 Datastore，例如：

  ```go
  // 默认主结果：使用 TaskID 作为 key
  func (c *Context) SetResult(value any) {
      c.Store.Set(c.TaskID, value)
  }

  func (c *Context) GetResult(taskID string) (any, bool) {
      return c.Store.Get(taskID)
  }

  // 扩展：自定义 key 写/读多个结果（示意）
  func (c *Context) SetResultWithKey(key string, value any) {
      c.Store.Set(key, value)
  }

  func (c *Context) GetResultWithKey(key string) (any, bool) {
      return c.Store.Get(key)
  }
  ```

---

## 6. Middleware：对齐 gin 的可插拔执行链

### 6.1 Middleware 的定位

snake 的 Middleware 模型直接借鉴 gin：

- 目标：为所有 Task 的执行链提供统一、可插拔的扩展点，解耦非业务功能；
- 作用范围：
  - 全局 middleware：适用于所有 Task；
  - Task 级 middleware：只作用于某些 Task；
- 典型能力举例（完全由用户自定义）：
  - panic recovery：防止某个 Task panic 导致整个编排崩溃；
  - tracing：在每个 Task 周围创建 span，连接成完整 trace；
  - logging：统一输出 Task 开始/结束/耗时/状态等；
  - 限流、熔断等。

### 6.2 Middleware 与 Handler 签名

- 统一签名：

  ```go
  type HandlerFunc func(c context.Context, ctx *Context) error
  ```

- 执行链模型：
  - Engine 在执行 Task 时，为该 Task 构建一条 handler 链：
    - 全局 middleware + Task 级 middleware + 最终业务 handler；
  - Context 内维护 `handlers []HandlerFunc` 和 `index`，提供类似 Gin 的 `Next()` 机制：

    ```go
    func (c *Context) Next() error {
        c.index++
        for c.index < len(c.handlers) {
            if err := c.handlers[c.index](c); err != nil {
                return err
            }
            c.index++
        }
        return nil
    }
    ```

  - 通常业务 handler 放在链的最后一个，由 Engine 注入。

---

## 7. 执行流程：从 DAG 构建到结果返回

### 7.1 DAG 构建与校验阶段

1. 注册 Task
   - 用户通过 Engine 提供的注册接口，传入 Task 定义（ID、依赖、handler 等）；
   - 若 TaskID 冲突或依赖未注册，`Register` 或后续 `Build` 返回错误。

2. 构建图（由 Execute 内部完成）
   - Engine 将 Task ID 作为节点；
   - 根据 Task 的依赖列表添加有向边 `dep -> task`；
   - 构建内部结构，例如：
     - 邻接表 `adj[TaskID] = []TaskID`（从当前节点指向的后继）；
     - 入度表 `indegree[TaskID]`。

3. 图校验
   - 检测环路：使用拓扑排序或 DFS 检测循环依赖；
   - 校验依赖存在性：所有依赖的 TaskID 必须在已注册任务中；
   - 若图无环，则记录一个拓扑序（可选）。

### 7.2 执行阶段：调度与并发（MVP）

1. 初始化执行上下文
   - 从用户的 `context.Context`（执行级）开始，为整个执行分配一个 ExecutionID；
   - 准备全局 Datastore 实例（新建或清空旧数据）；
   - 初始化每个 Task 的剩余依赖数 `pendingDeps`（可用原子整型）。

2. 拓扑调度
   - 将 `pendingDeps == 0` 的 Task 放入就绪队列;
   - 支持多个入度为 0 的起始节点（非连通图）；
   - MVP：每个就绪 Task 启动一个 goroutine 执行，不限制最大并发数。

3. 执行单个 Task
   - Worker/Goroutine 取到 Task 后：
     - 基于执行级 ctx 派生出一个 Task 级 ctx（可加入 Task 级超时控制）；
     - 为 Task 创建 Context：注入 Task 信息、Datastore 视图、Logger（带 taskID）、ExecutionID 等；
     - 组装 handler 链：全局 middleware + Task 级 middleware + 最终 handler；
     - 调用链首元素开始执行。
   - 执行完成后：
     - 将执行结果（成功/失败/错误信息/耗时等）记录在执行状态表；
     - 成功时，通过 Datastore 写入 Task 结果；
     - 返回 error 时，根据错误策略触发 Fail-Fast（取消执行级 ctx）。

4. 调度下游 Task
   - 当某 Task 成功完成时：
     - 对其所有直接后继节点的 `pendingDeps` 做原子减 1；
     - 若某后继 Task 的 `pendingDeps` 降为 0，并且执行级 ctx 尚未取消，则加入就绪队列；
   - 执行持续，直至：
     - 就绪队列为空，且所有 Task 均处于终结状态（Success / Failed / Skipped）。

### 7.3 错误传播与任务状态模型（MVP）

- 任务状态枚举（建议）：

  ```go
  type TaskStatus string

  const (
      TaskStatusPending   TaskStatus = "PENDING"
      TaskStatusSuccess   TaskStatus = "SUCCESS"
      TaskStatusFailed    TaskStatus = "FAILED"
      TaskStatusCancelled TaskStatus = "CANCELLED" // 因上游失败或全局取消导致未执行
      TaskStatusSkipped   TaskStatus = "SKIPPED"   // 预留给业务主动跳过（未来扩展）
  )
  ```

- Fail-Fast 策略：
  - 某 Task 在执行链中返回非 nil error：
    - 当前 Task 标记为 Failed；
    - Engine 通过 `cancel()` 取消执行级 ctx；
  - 对此后：
    - 已在执行中的 Task，可能收到 `ctx.Canceled` 或 `DeadlineExceeded`，也可能在处理中；
    - 尚未开始执行的 Task 不再调度，统一标记为 Skipped（或 Canceled，命名需在文档中固定）。

- 无论哪种情况，Engine 都会维护一份 任务执行报告，记录：
  - Task 状态（Success / Failed / Skipped）；
  - 错误信息（如有），包括是业务错误还是 context 错误；
  - 开始时间 / 结束时间 / 耗时；
  - 以及可能的重试次数信息（若实现了重试）。

### 7.4 结果返回

- 执行完成后，Engine 对外返回：

  ```go
  type TaskReport struct {
      TaskID     TaskID
      Status     TaskStatus
      Err        error
  }

  type ExecutionResult struct {
      ExecutionID string
      // 整体是否成功：所有目标 Task Success 即为成功
      Success     bool

      // 所有 Task 的报告
      Reports     map[string]*TaskReport

      // 便捷方法：获取某个 Task 的结果
      Store       Datastore

      // 拓扑序，便于排障和可视化
      TopoOrder   []string
  }
  ```

- 「指定 Task 的最终结果」：
  - 调用方可以在 `ExecutionResult.Store` 中，通过 TaskID 读取指定 Task 的输出；
  - 或者 Engine 可以提供便捷方法 `GetResult(taskID string) (any, bool)`。

---

## 8. 日志与可观测性

### 8.1 Logger 集成

- Engine 在创建时接收一个 自定义 Logger 接口实例，统一管理日志输出：
  - 接口可以类似：

    ```go
    type Logger interface {
        Info(ctx context.Context, msg string, fields ...Field)
        Error(ctx context.Context, msg string, err error, fields ...Field)
        With(fields ...Field) Logger
    }
    ```

- 在构建任务级 Context 时：
  - 为当前 Task 创建一个 带 taskID / ExecutionID 的子 logger；
- Handler 和 Middleware 使用 Context 内的 Logger 记录：
  - Task 开始/结束/耗时；
  - 错误和 panic；
  - 关键业务日志（可选）。

### 8.2 Tracing / Metrics（通过 Middleware 实现）

- Tracing 中间件：
  - 在 Task 执行前后创建 span；
  - span 标记 taskID、依赖关系、执行状态；
  - 将 DAG 在观测系统中还原为一串相连的 spans。

- Metrics 中间件：
  - 对每个 Task 记录：
    - 调用次数；
    - 失败率；
    - 耗时直方图等。

- Engine 可附带：
  - 导出 DAG 结构为可视化格式（Graphviz DOT、JSON）；
  - 提供执行报告结构，便于接入监控系统或管理后台展示。

---

## 9. 拓展点：条件分支与跳过（MVP 之后）

- 当前 MVP 版本仅支持 静态 DAG：依赖满足即执行。
- 后续可以考虑：
  - 在 Task 的 handler 返回一种特定「跳过错误」（如 `ErrSkipTask`），Engine 将当前 Task 标记为 Skipped，但不触发 Fail-Fast；
  - 根据 Task 状态模型，在下游 Task 中决定是否执行（例如某些 Task 只在上游 Success 时执行）。
- 为了兼容未来扩展，本设计已经预留：
  - TaskStatusCancelled；
  - TaskStatusSkipped（可选扩展保留位）。
  - 执行报告结构中携带状态与错误类型。

---

## 10. 小结：四个核心概念的角色对齐

- Engine
  - 负责 DAG 构建与校验、任务调度与并发执行、全局资源管理（Datastore、Logger、中间件、配置）；
  - 是 snake 的「编排中枢」。

- Task
  - 代表 单个业务单元；
  - 显式声明依赖，决定 图结构与执行顺序；
  - 在执行时，通过 Context 操作 Datastore，完成数据的生产和消费。

- Datastore
  - 负责 跨任务的数据传递与结果存储；
  - 使用 map[string]any + 读写锁 保证并发安全；
  - 是 DAG 上「数据流」的载体。

- Context
  - 是 任务级执行上下文，为某个 Task 的一次执行提供：
    - 执行级 `context.Context`；
    - Task 信息（ID / Name / Labels）；
    - Datastore 访问入口；
    - Logger / Tracer / Metrics 句柄；
  - 是 middleware 和 handler 之间交互的统一载体。
