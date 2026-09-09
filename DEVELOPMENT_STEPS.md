# CortexGo 开发步骤

本文档以学习为主线，记录 CortexGo 每个阶段需要掌握的知识、代码对应关系、实践任务和验收标准。

## 如何使用这份路线

每个阶段按以下顺序学习：

1. 先理解概念和系统边界。
2. 阅读对应代码，不急着修改实现。
3. 运行现有测试，观察输入、输出和错误路径。
4. 完成阶段练习，再进入下一阶段。
5. 能回答“为什么这样设计”，而不只是“代码能运行”。

建议学习顺序：Go 基础 → Provider → Agent → Tool → RAG/Knowledge → Memory → 企业化。

## 总体原则

1. 先定义稳定接口，再替换底层实现。
2. 核心能力优先提供可测试的本地实现，再接入外部服务。
3. Provider、Agent、Memory、Tool、Knowledge 分层，业务代码不绑定具体模型或数据库。
4. 每次改动必须补充测试，并通过 `go test`、`go test -race`、`go vet` 和格式检查。
5. 外部依赖通过接口注入，默认保留离线可运行路径。

## 阶段一：最小内核（已完成）

### 本阶段要学什么

- Go package、interface、struct、method 和依赖注入。
- `context.Context` 的用途：取消、超时和请求边界。
- 会话消息的基本数据模型：user、assistant、tool。
- 为什么要把模型、记忆和编排层分开。
- 内存存储中的并发安全：`sync.RWMutex` 和数据复制。

### 对应代码

- `internal/provider/provider.go`：模型接口和消息结构。
- `internal/memory/memory.go`：Store 接口和内存实现。
- `internal/agent/agent.go`：最小调用链。
- `cmd/cortexgo/main.go`：CLI 入口。

### 学习练习

- 为 Memory 增加按 session 清空的方法。
- 写一个固定回复的 `ChatModel`，替换 Echo。
- 解释为什么 `List` 不能直接返回内部 slice。

### 阶段验收问题

- Agent 为什么依赖 interface，而不是直接依赖 Echo？
- 一次对话从 CLI 到 Memory 经过哪些边界？
- 如果两个 goroutine 同时写一个 session，会发生什么？

目标：建立可运行的 Agent 最小闭环。

- 定义 `provider.ChatModel`。
- 定义 `memory.Store`。
- 提供线程安全的内存会话存储。
- 提供 CLI 交互式对话。
- 提供本地 `Echo` 模型作为离线 fallback。

验收：

```bash
go run ./cmd/cortexgo
go test ./...
```

## 阶段二：真实对话与模型适配（已完成）

### 本阶段要学什么

- HTTP 客户端生命周期、请求构造和响应解码。
- OpenAI Chat Completions 的 wire format。
- SSE（Server-Sent Events）和增量 token。
- 超时与取消的区别，以及重试的适用边界。
- 指数退避、最大重试次数和幂等性风险。
- 错误包装、`errors.Is`、`errors.As` 和错误上下文。
- Prompt/Completion/Total Token 的含义和成本控制。
- 为什么 Provider 适配层不能把 HTTP 细节泄漏到 Agent。

### 对应代码

- `internal/provider/provider.go`：Chat、SSE、重试、日志和错误封装。
- `internal/provider/embedding.go`：Embeddings API。
- `internal/provider/provider_test.go`：HTTP wire-format 和超时测试。

### 学习练习

- 增加 429 响应的重试测试。
- 增加响应 JSON 缺少 choices 的错误测试。
- 实现一个只支持非流式响应的本地 Provider。
- 解释为什么请求总超时应覆盖重试等待时间。

### 阶段验收问题

- 什么时候应该重试，什么时候不应该重试？
- SSE 中为什么不能只读取最后一个事件？
- Provider 的 `Error` 为什么需要 `Unwrap`？
- Chat 模型和 Embedding 模型为什么要分开配置？

目标：将模型调用从本地 Echo 扩展为真实 LLM Provider。

- OpenAI-compatible `POST /v1/chat/completions`。
- Chat 与 Embeddings 的超时控制。
- 指数退避重试。
- SSE 流式响应解析。
- Prompt、Completion、Total Token 统计。
- `provider.Error` 统一错误封装，并保留 `Unwrap` 能力。
- 可选基础日志。
- 保持旧 `ChatModel` / `StreamChatModel` 接口兼容。
- 通过 `ToolChatModel` / `ToolStreamChatModel` 扩展工具调用能力。

主要文件：

- `internal/provider/provider.go`
- `internal/provider/embedding.go`
- `cmd/cortexgo/main.go`

真实模型配置：

```bash
CORTEXGO_PROVIDER=openai \
CORTEXGO_BASE_URL=https://api.openai.com \
CORTEXGO_MODEL=gpt-4o-mini \
CORTEXGO_EMBEDDING_MODEL=text-embedding-3-small \
CORTEXGO_API_KEY=your-key \
go run ./cmd/cortexgo
```

## 阶段三：工具调用（基础能力已完成）

### 本阶段要学什么

- Function calling 的协议：模型提出调用，应用执行，再把结果交回模型。
- Tool loop 的状态机和最大轮数保护。
- JSON Schema 的类型、required、properties、enum 和 additionalProperties。
- 不可信模型输出的校验与错误恢复。
- 权限控制、最小权限原则和工具白名单。
- 工具超时、panic 隔离和审计事件。
- 为什么工具结果必须是结构化 JSON。

### 对应代码

- `internal/tool/tool.go`：注册、校验、调用和错误处理。
- `internal/tool/current_time.go`：内置工具示例。
- `internal/agent/agent.go`：多轮工具编排、授权和审计。

### 学习练习

- 增加一个需要必填字符串参数的工具。
- 增加一个拒绝访问的 Authorizer。
- 增加一个超时工具，观察 Agent 如何继续处理错误结果。
- 记录一次工具调用的完整审计事件。

### 阶段验收问题

- 为什么不能直接执行模型返回的函数名和参数？
- 最大 tool round 防止了什么问题？
- Handler 忽略 context 时，超时实现有什么代价？
- 权限检查应该放在 Agent、Registry 还是 Handler 内部？

目标：让 Agent 能够安全地执行模型返回的工具调用。

- 工具定义与注册表。
- 工具名称、描述、Handler 校验。
- 基础 JSON Schema 校验。
- 拒绝未支持的 Schema 关键字，避免约束被静默忽略。
- 非流式和流式 tool loop。
- 最大工具轮数限制。
- 工具调用参数、ID 和类型校验。
- 工具执行超时。
- 可注入权限策略。
- 可注入结构化审计日志。
- 内置 `current_time` 工具。

主要文件：

- `internal/agent/agent.go`
- `internal/tool/tool.go`
- `internal/tool/current_time.go`

后续增强：

- 更细粒度的 RBAC。
- 审计事件持久化。
- 工具配额、速率限制和审批流。
- 使用成熟 JSON Schema 库替代当前基础实现。

## 阶段四：知识管理（当前）

### 本阶段要学什么

- RAG（Retrieval-Augmented Generation）的完整链路。
- 文档、Chunk、Metadata 和 Citation 的关系。
- Chunk size、overlap 对召回率和上下文长度的影响。
- Tokenization 与中文/英文混合文本检索。
- Embedding 的向量空间、维度和余弦相似度。
- Top-K、相关性分数和结果截断。
- 关键词检索、向量检索和 Hybrid Search 的差异。
- 持久化索引、原子写入和进程重启恢复。
- 为什么生产环境通常需要专用向量数据库。

### 对应代码

- `internal/knowledge/knowledge.go`：切分和关键词检索。
- `internal/knowledge/vector.go`：向量索引和混合检索。
- `internal/knowledge/persistent.go`：文件持久化。
- `internal/provider/embedding.go`：真实 Embedding API。

### 学习练习

- 调整 chunk 最大长度和 overlap，比较召回结果。
- 增加文档来源 URL、更新时间等 metadata。
- 为同一查询分别输出关键词分数和向量分数。
- 模拟进程重启，验证持久化索引仍可搜索。
- 实现一个简单的引用格式：文件名 + chunk 序号。

### 阶段验收问题

- 为什么 Chunk 不应该简单按字节截断？
- 余弦相似度为什么要忽略向量长度？
- Hybrid Search 中 alpha 增大意味着什么？
- Embedding 模型更换后，旧索引为什么通常必须重建？

目标：建立文档导入、切分、检索和引用基础设施。

### 4.1 文档切分

- `Document`、`Chunk`、`Result` 数据模型。
- Unicode 安全切分。
- 最大字符数和重叠窗口。
- 保留标题、文档 ID、序号和元数据。

### 4.2 关键词检索

- 线程安全 `InMemoryIndex`。
- 中文、英文、数字混合分词。
- 基础相关性排序。
- 稳定的结果排序。

### 4.3 向量检索

- `EmbeddingModel` 接口。
- `VectorIndex` 接口。
- 内存向量索引。
- 余弦相似度。
- 向量维度和空向量校验。

### 4.4 混合检索

- `HybridIndex`。
- 关键词分数与向量分数加权融合。
- 可配置 `alpha` 权重。

### 4.5 持久化

- `FileVectorIndex`。
- JSON 文件存储。
- 原子写入。
- 进程重启后恢复向量和 Chunk。
- `NewPersistentHybridIndex` 自动重建关键词索引。
- `knowledge.SearchTool` 将知识检索暴露为带引用的 Agent 工具。
- Knowledge 检索支持 metadata 全匹配过滤；同一文档 ID 重复导入会删除旧 chunks 后写入新版本，避免增量更新残留。
- CLI 只指定 `-knowledge-file` 时会在交互式 Agent 中自动注册知识检索工具。

主要文件：

- `internal/knowledge/knowledge.go`
- `internal/knowledge/vector.go`
- `internal/knowledge/persistent.go`
- `internal/provider/embedding.go`
- `internal/knowledge/tool.go`

CLI 验收：

```bash
go run ./cmd/cortexgo \
  -provider=openai \
  -base-url=https://api.openai.com \
  -model=gpt-4o-mini \
  -embedding-model=text-embedding-3-small \
  -knowledge-file=README.md \
  -knowledge-query="向量检索"
```

当前限制：

- 当前索引文件是本地 JSON，不适合多进程并发写入。
- 关键词检索仍是轻量实现，不是完整 BM25。
- 向量索引尚未接入专用向量数据库。
- 文档解析支持 txt、Markdown、基础 HTML，以及 PDF、DOCX、XLSX、PPTX 文本提取；扫描型 PDF 通过 pdftoppm + Tesseract OCR 处理，旧版二进制 Office（DOC/XLS/PPT）通过 LibreOffice/soffice 转换。

## 阶段五：记忆系统（基础能力已完成）

### 本阶段要学什么

- 短期记忆、长期记忆、用户画像和知识库的边界。
- 对话摘要、上下文压缩和重要性评分。
- 记忆写入策略：自动写入、用户确认和显式删除。
- 数据生命周期、隐私、可控遗忘和租户隔离。
- 记忆召回与知识召回的合并排序。

当前已实现 `FileStore`，支持 JSON 会话持久化、重启恢复、按 session 删除和原子写入；另有 `FactStore`/`InMemoryFactStore` 管理用户级长期事实并支持按 key 遗忘，`FileFactStore` 提供 JSON 持久化与重启恢复。事实存储支持隐私策略（敏感事实开关、值长度上限、默认 TTL、过期清理）和按用户全部遗忘；`memory.Store` 接口保持不变。文件事实存储目前未实现静态加密和跨进程锁。

`Compact` 已通过 `WithContextMessageLimit`、`WithContextTokenBudget` 和 `WithConversationSummarizer` 接入 Agent 主循环：超限时摘要旧消息、保留最近消息，并在支持 `ReplaceStore` 的 Memory Store 中写回压缩结果；已有摘要不会在每轮重复生成。Token 预算使用模型无关的近似估算，预算过小时优先保留摘要和最新消息。

### 学习练习

- 为会话增加最大 token 预算。
- 超出预算时生成摘要并替换旧消息。
- 增加按 session 删除记忆的接口。
- 设计一条“用户要求忘记某信息”的测试用例。

主要文件：

- `internal/memory/memory.go`
- `internal/memory/file_store.go`
- `internal/memory/facts.go`
- `internal/memory/compact.go`

- 短期会话记忆持久化。
- 长期用户记忆。
- 记忆摘要与压缩。
- 可控遗忘和数据删除。
- 记忆检索与知识检索的边界定义。

## 阶段六：企业能力（待开发）

### 本阶段要学什么

- 多租户架构和数据隔离。
- RBAC、API Key、密钥轮换和 Secret 管理。
- 限流、配额、成本统计和背压。
- PostgreSQL/Redis 等持久化选型。
- OpenTelemetry 的 trace、metric、log 三类信号。
- 当前 Agent 已提供可桥接 OpenTelemetry 的模型调用观测事件，并支持按模型 Token 单价计算成本；导出器和后端由应用层接入。
- `VectorDatabase` 提供专用向量存储契约，内置文件实现支持 Upsert、metadata 过滤、文档级删除和持久化恢复；后续可接入 Milvus、pgvector 等生产后端。
- `internal/reliability` 已定义 PostgreSQL/Redis 适配接口，并提供租户级请求、Token、成本预算和并发背压，以及 JSON 备份/恢复基础工具；生产部署仍需补充数据库迁移脚本、分布式锁和定时备份编排。
- Knowledge 已补充 OCR 运行时检测、文档版本哈希、异步增量导入/进度查询和 BM25 风格词频饱和；表格/图片/脚注的完整版面语义与生产级重排模型仍需专用解析器和模型适配。
- `internal/core` 定义统一 `Task`、`Run`、`Event`、`Tool` 和 `EventSink` 契约，作为后台任务、Agent 执行和事件观测的公共边界。
- HTTP API、服务部署、健康检查和优雅退出。
- 安全边界：SSRF、Prompt Injection、数据泄露和工具滥用。

### 学习练习

- 为每个请求加入 tenant ID。
- 为模型和工具调用增加成本统计。
- 增加按 API Key 的限流器。
- 用 trace ID 关联一次请求中的模型调用、工具调用和检索。

- 多租户。
- RBAC 和密钥管理。
- 限流、配额和成本统计。
- PostgreSQL/Redis 等持久化存储。
- OpenTelemetry。
- HTTP API 和服务化部署。

## 每次开发的标准流程

1. 阅读 `README.md` 与本文件，确认当前阶段和边界。
2. 检查工作区：

   ```bash
   git status --short --branch
   git diff --stat
   ```

3. 先定义或复用接口，再实现具体功能。
4. 为成功、失败、超时、取消和边界输入补测试。
5. 执行格式化与验证：

   ```bash
   gofmt -w <changed-go-files>
   GOCACHE=/tmp/cortexgo-gocache go test ./...
   GOCACHE=/tmp/cortexgo-gocache go test -race ./...
   GOCACHE=/tmp/cortexgo-gocache go vet ./...
   git diff --check
   ```

6. 检查是否破坏旧接口、默认 CLI 行为或离线 fallback。
7. 更新 README 和本开发步骤文档。
8. 汇报修改文件、设计取舍、测试结果和剩余限制。

## 当前工作区说明

当前仓库的阶段性改动会在每个开发阶段完成后提交并同步到远端；开始新任务时仍需先检查工作区，保留已有未提交修改，不要使用 `git reset --hard` 或覆盖性 checkout 操作。
