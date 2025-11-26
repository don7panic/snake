# golang 任务编排框架 snake 设计文档

## 1. 总体目标与定位

snake 是一个 进程内的任务编排框架，用 有向无环图（DAG） 描述任务间依赖关系，并具备：

- 显式依赖建模：每个任务声明依赖的其他任务；
- 自动数据流转：任务结果通过 Datastore 在依赖链路上传递；
- 并行执行能力：根据 DAG 与依赖情况自动进行并行调度；
- middleware 可插拔能力：参考 gin 的 middleware 模型，为任务执行链路提供统一的扩展点（recovery / tracing / logging / metrics 等）。

整体工作流：

1. 创建 Engine，通过 option 注入配置（日志、错误策略等）。
2. 定义 Task，在 Task 中声明依赖关系，并注册到 Engine。
3. Engine 构建并校验 DAG（cycle 检测、依赖存在性等）。
4. Engine 执行 DAG，创建任务级 Context，调度任务并行执行，通过 Datastore 传递数据。
5. 获取执行结果（整体状态 + 指定任务输出 + 每个 task 的执行报告）。


## 2. Engine：图构建 & 调度核心

### 2.1 Engine 的职责

Engine 是 snake 的核心，负责：

1. 任务图构建与管理
  - 维护所有注册的 Task 节点；
  - 构建任务间的 DAG 结构（邻接表、入度、拓扑序等）。

2. 图校验
  - 检测循环依赖（保证 DAG 无环）；
  - 校验依赖存在性（依赖的 Task 是否已注册）；
  - 可选：根据 Task 的声明，做输出/输入类型或数据契约的静态检查。

3. 执行调度
  - 基于拓扑排序 / 入度算法管理「就绪任务队列」；
  - 为每个任务构造 任务级 Context 并执行 handler 链；
  - 维护每个任务的执行状态（成功、失败、跳过等）。

4. 共享资源管理
  - 持有全局 Datastore 实例；
  - 持有全局 Logger、全局中间件列表、执行策略配置等。

### 2.2 Engine 的关键配置（通过 options）

Engine 在创建时可以通过 options 注入配置，包括但不限于：

1. 错误处理策略
  - 任一任务失败，立刻取消整个执行；
  - 暂不支持其他

2. 超时与取消
  - 全局执行超时时间；
  - 默认 Task 超时时间（单 Task 级别亦可覆盖）。

3. 日志与观测
  - 自定义 Logger 实现；
  - 全局 tracing / metrics 开关或抽象接口。

4. 中间件配置
  - 全局 middleware 列表（类似 `gin.Engine.Use(...)`），对所有 Task 生效；
  - 可选：按 Task 标签选择性应用的中间件。

## 3. Task：任务节点和依赖关系

### 3.1 Task 的职责

Task 是 DAG 中的一个 节点，代表一个可执行的业务单元，负责：

- 实现具体业务逻辑（如调用下游服务、读写数据库、计算数据等）；
- 明确 产出的数据结果 写入 Datastore；
- 声明 依赖的其他 Task，框架根据依赖自动调度；
- 可选：定义自己特有的执行配置（超时、重试策略、自带的 middleware 等）。

### 3.2 Task 的核心要素

1. 唯一标识
  - 每个 Task 有一个 唯一 ID（string），用来：
    - 声明依赖：通过 ID 引用其他 Task；
    - 作为 Datastore 中结果的 key；

2. 依赖列表
  - 明确声明 `DependsOn: [TaskID1, TaskID2, ...]`；
  - Engine 在构图时将这些依赖转为有向边；
  - 依赖的意义：
    - 执行顺序：只有当依赖全部成功完成后，当前 Task 才会被调度执行（除非错误策略允许其它行为）；
    - 数据可见性：当前 Task 可以从 Datastore 中读取依赖 Task 的输出结果。

3. 处理函数（Handler）
  - 每个 Task 有一个核心业务 handler：
    - 通过 Context 读取依赖数据；
    - 执行业务逻辑；
    - 把结果写回自己的 TaskID 对应的 Datastore 条目中；
    - 返回成功/失败状态（error）。
  - 实际执行时，handler 会被包裹在一条由 middleware + handler 组成的链路里，middleware 也是 handlerFunc。

4. Task 级配置（可选）
  - 超时时间；
  - 重试策略（最大重试次数、退避策略）；
  - Task 标签（用于中间件筛选、日志分组等）；
  - Task 级别中间件（只作用于该 Task）。

## 4. Datastore：数据流与结果存储

### 4.1 Datastore 的职责

Datastore 是 任务间数据流转的载体，负责：

- 存储每个 Task 的执行结果（典型是 `TaskID -> Result` 映射）；
- 在 Task 之间 传递数据：下游 Task 能读到上游依赖 Task 的输出；
- 提供必要的 并发安全 支持（多 Task 并行读写）；

### 4.2 数据流设计原则

- 单写多读
  - 每个 Task 只负责写自己的结果（使用自己的 TaskID 作为 key）；
  - 任何 Task 不能写别的 Task 的结果；

### 4.3 多任务并发下的数据一致性

- Datastore 实现应是 全局唯一实例，由 Engine 持有；
- 各任务 Context 的 `Datastore` 字段，只是指向同一份底层实现的引用；
- 底层存储结构（如内存 map、KV 等）负责：
  - 写时加锁或使用无锁并发结构；
  - 读写可见性保证（发生在内存中的任务间通信）。

## 5. Context：任务级执行上下文

### 5.1 为什么需要 Context

Context 是 「当前任务执行时的上下文载体」，对应的是某个 Task 在某次 Engine.Execute 调用中的一次执行。它主要用于：

- 封装 标准库 context.Context，传递取消/超时信号；
- 暴露当前 Task 可访问的 Datastore 视图；
- 暴露已绑定 Task 信息的 Logger / Tracer / Metrics handle 等；
- 为 middleware 和 handler 提供统一的访问入口，类似 gin.Context。

### 5.2 Context 的层级区分

在 snake 中，建议区分两个层级的「上下文」概念：

- 执行级（workflow 级）上下文
  - 由用户传给 Engine：`engine.Execute(ctx)` 中的 ctx；
  - 表示这次编排执行的整体生命周期；
  - 用于全局超时 / cancel 控制；
  - 在内部被传播到各个 Task 的 Context。

- 任务级 Context（snake 自己的 Context 结构）
  - 每个 Task 被调度执行时，Engine 为它 创建一个独立的 Context 实例；
  - 内容包括：
    - 执行级 `context.Context`（通常是加了 Task 级超时等的子上下文）；
    - 当前 Task 的元信息（ID、Name、标签等）；
    - 当前 Task 拥有的 Datastore 访问入口；
  - 不在不同 Task 之间共享，避免并发写共享字段的问题。

### 5.3 Context 与 Datastore 的关系

- 每个任务级 Context 中都持有一个 Datastore 接口/视图的引用；
- 这些引用底层都指向 同一个全局 Datastore 实现实例；

## 6. Middleware：对齐 gin 的可插拔执行链

### 6.1 Middleware 的定位

snake 的 Middleware 模型直接借鉴 gin：

- 目标：为所有 Task 的执行链提供统一、可插拔的扩展点，解耦非业务功能；
- 作用范围：
  - 全局 middleware：适用于所有 Task；
  - Task 级 middleware：只作用于某些 Task；

- 典型能力举例 middleware example (可以完全用户自定义)：
  - panic recovery：防止某个 Task panic 导致整个编排崩溃；
  - tracing：在每个 Task 周围创建 span，连接成完整 trace；
  - logging：统一输出 Task 开始/结束/耗时/状态等；
  - 限流、熔断等。

### 6.2 Middleware 与 Context 的关系

- Middleware 接口以 Context 为中心：
  - 从 Context 中读写必要信息（如在 Logger 加字段、记录 metrics 等）；
  - 调用 `Next()` 或等价机制继续调用后续 middleware/handler。

- Engine 在执行 Task 时，会为该 Task 构建一条 handler 链：
  - 全局 middleware + Task 级 middleware + 最终业务 handler；
  - 这一条链对一个 Task 有一个专属 Context 实例。

## 7. 执行流程：从 DAG 构建到结果返回

### 7.1 DAG 构建与校验阶段

1. 注册 Task
   - 用户通过 Engine 提供的注册接口，传入 Task 定义（ID、依赖、handler 等）。

2. 构建图
   - Engine 将 Task ID 作为节点；
   - 根据 Task 的依赖列表添加有向边；
   - 形成内部的 DAG 结构表示。

3. 图校验
   - 检测环路：使用拓扑排序或 DFS 检测循环依赖；
   - 校验依赖存在性：所有依赖的 TaskID 必须在已注册任务中；
   - 可选：类型/契约校验，在构图时提前发现数据类型不匹配。

### 7.2 执行阶段：调度与并发

1. 初始化执行上下文
   - 从用户的 `context.Context`（执行级）开始，为整个执行分配一个 ExecutionID；
   - 准备全局 Datastore 实例（或清空旧数据）。

2. 拓扑调度
   - 计算每个 Task 的「未完成依赖数量」（入度）；
   - 将入度为 0 且不被跳过的 Task 放入就绪队列；

3. 执行单个 Task
   - Worker 取到 Task 后：
     - 基于执行级 ctx 派生出一个 Task 级 ctx（可加入 Task 级超时控制）；
     - 为 Task 创建 Context：注入 Task 信息、Datastore 视图、Logger（带 taskID）、ExecutionID 等；
     - 组装 middleware + handler 链，依次执行；
   - 执行完成后：
     - 将执行结果（成功/失败/错误信息/耗时等）记录在 Engine 内部的执行状态表；
     - 通过 Datastore 写入 Task 结果（若成功）。

4. 调度下游 Task
   - 某 Task 成功完成后：
     - 使用 atomic 将其所有直接后继节点的「未完成依赖数」减 1 ；
     - 若某后继 Task 的未完成依赖数降为 0，并且未被错误策略跳过，则加入就绪队列。
   - 执行持续，直至：
     - 就绪队列为空，且所有 Task 终结（成功/失败/跳过）。

### 7.3 错误传播与执行策略

根据 Engine 的错误策略配置，处理：

- 某 Task 失败后：
  - Fail-Fast：立刻 cancel 执行级 ctx，快速结束所有任务；
  - 目前不支持其他策略。

- 无论哪种策略，Engine 都会维护一份 任务执行报告，记录：
  - Task 状态（Success / Failed / Skipped）；
  - 错误信息（如有）；
  - 开始时间 / 结束时间 / 耗时；
  - 以及可能的重试次数信息。

### 7.4 结果返回

- 执行完成后，Engine 对外返回：
  - 整体执行状态（是否全部成功、是否有失败）；
  - 指定 Task 的 最终结果（从 Datastore 读取目标 TaskID 的结果）；
  - 整体的 执行报告结构（可用于日志记录、对外接口返回或观测系统对接）。


## 8. 日志与可观测性

### 8.1 Logger 集成

- Engine 在创建时接收一个 自定义 Logger 接口实例，统一管理日志输出；
- 在构建任务级 Context 时：
  - 为当前 Task 创建一个 带 taskID 的子 logger；
- Handler 和 Middleware 使用 Context 内的 Logger 实例记录：
  - Task 开始/结束/耗时；
  - 错误和 panic；
  - 关键业务日志（可选）。

### 8.2 Tracing / Metrics（完全可以由用户定义）

- 通过 Middleware 扩展：
  - Tracing middleware：基于 Context 中的 Tracer，还原任务 DAG 到一串 trace span；
  - Metrics middleware：对每个 Task 记录执行次数、失败率、耗时直方图等。
- Engine 可以附带：
  - 导出 DAG 结构为可视化格式（Graphviz DOT、JSON）；
  - 提供执行报告结构，便于接入监控系统或管理后台展示。

## 9. 小结：四个核心概念的角色对齐

- Engine
  - 负责 DAG 构建与校验、任务调度与并发执行、全局资源管理（Datastore、Logger、中间件、配置）；
  - 是 snake 的「编排中枢」。

- Task
  - 代表 单个业务单元；
  - 显式声明依赖，决定 图结构与执行顺序；
  - 在执行时，通过 Context 操作 Datastore，完成数据的生产和消费。

- Datastore
  - 负责 跨任务的数据传递与结果存储；
  - 提供并发安全的访问机制；
  - 是 DAG 上「数据流」的载体。

- Context
  - 是 任务级执行上下文，为某个 Task 的一次执行提供：
    - 执行级 context.Context；
    - Task 信息；
    - Datastore 访问入口；
    - Logger；
  - 是 middleware 和 handler 之间交互的统一载体。
