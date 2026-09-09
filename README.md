# CortexGo

基于 Go 语言构建的企业级 AI Agent 框架，提供对话 AI、知识管理、记忆系统和工具调用能力。

## 当前进度：Step 4 — 知识管理基础层

现在已经可以运行一条完整链路：`交互式输入 → 读取会话记忆 → 调用模型 → 执行工具 → 写回记忆 → 输出`。CLI 默认使用本地 Echo 模型，也可以接入任意 OpenAI-compatible 服务；模型返回会保留内容、模型名、finish reason 和 Token 用量。

```bash
go run ./cmd/cortexgo
go test ./...
go run ./cmd/cortexgo -show-usage
```

评测与 CI/CD：`make ci` 会依次执行 `vet`、单元测试、竞态测试和构建；`make coverage` 输出覆盖率明细，`make bench` 运行基准评测。推送到主分支或提交 Pull Request 时，GitHub Actions 自动执行格式检查、测试、竞态检测和构建；推送 `vX.Y.Z` 标签时自动构建 Linux/macOS/Windows 发布包并创建 GitHub Release。

接入真实模型：

```bash
CORTEXGO_PROVIDER=openai \
CORTEXGO_BASE_URL=https://api.openai.com \
CORTEXGO_MODEL=gpt-4o-mini \
CORTEXGO_API_KEY=your-key \
go run ./cmd/cortexgo
```

知识库导入与查询（默认持久化到 `.cortexgo-knowledge.json`，支持 txt、Markdown 和基础 HTML）：

```bash
CORTEXGO_PROVIDER=openai \
CORTEXGO_BASE_URL=https://api.openai.com \
CORTEXGO_MODEL=gpt-4o-mini \
CORTEXGO_EMBEDDING_MODEL=text-embedding-3-small \
CORTEXGO_API_KEY=your-key \
go run ./cmd/cortexgo -knowledge-file=README.md -knowledge-query="向量检索"
```

可通过 `-knowledge-index` 或 `CORTEXGO_KNOWLEDGE_INDEX` 指定索引文件路径。

只指定 `-knowledge-file` 时会进入正常对话，并自动注册 `knowledge_search` 工具，让模型在对话中检索该知识库；只指定 `-knowledge-query` 时则查询已有索引并退出。

常用参数：`-provider`、`-base-url`、`-model`、`-embedding-model`、`-session`、`-memory-file`、`-retries`、`-retry-backoff`、`-timeout`、`-stream=false`、`-show-usage`、`-tools=false`。API key 只从 `CORTEXGO_API_KEY` 读取，避免出现在命令行参数里。

代码边界：

- `internal/provider`：模型适配层，内置 `Echo` 和 `OpenAICompatible`，支持 Chat/Embeddings、流式输出、Token 用量、请求超时和指数退避重试。
- `internal/memory`：会话记忆存储，提供线程安全的内存实现和本地 JSON 文件实现。
- `internal/agent`：Agent 编排核心。
- `internal/tool`：工具定义、Schema 校验、注册表和 `current_time` 内置工具。

当前工具调用已支持基础 Schema 校验、权限策略、超时、多轮循环和结构化审计；更细粒度的企业 RBAC 与持久化审计仍在后续迭代中。

长期记忆基础层提供用户事实存储和可控遗忘接口：`InMemoryFactStore` 适合进程内使用，`FileFactStore` 以 JSON 原子写入并在重启后恢复。两者均支持隐私策略（敏感事实开关、值长度上限、默认 TTL、过期清理）和按用户遗忘；当前未实现静态加密及跨进程锁。

会话上下文提供可注入摘要函数的压缩能力，可将旧消息合并为摘要并保留最近对话，避免上下文无限增长。

Agent 可通过消息数上限或近似 Token 预算和 `SummaryFunc` 自动触发上下文压缩；压缩后的历史会写回支持替换的 Memory Store，避免每轮重复摘要。Token 估算与具体模型 tokenizer 存在差异，后续可替换为 Provider 原生 tokenizer。

知识管理基础层已支持 Unicode 文档切分、线程安全内存索引、关键词检索、向量检索和混合搜索；Embedding provider 与持久化向量数据库通过接口接入。

知识库还提供 `knowledge.SearchTool(index)`，可注册到 Agent 的工具 Registry，让模型在对话中检索知识并获得 chunk 引用；搜索支持按 metadata 精确过滤，重复添加同一文档 ID 会先移除旧 chunks，实现增量更新。

专用向量数据库接口 `knowledge.VectorDatabase` 已提供持久化实现 `OpenVectorDatabase(path)`，支持向量 Upsert、余弦相似度检索、metadata 过滤、文档删除和条目统计；底层文件格式便于本地开发，生产环境可替换为外部向量数据库。

知识导入层新增 OCR 依赖检测、文档版本哈希、`AsyncImporter` 后台导入和进度查询；同一内容不会重复生成版本。大文件仍通过 `SplitDocument` 分块，生产环境可将导入任务接入队列。

可靠性基础层 `internal/reliability` 提供 PostgreSQL/Redis 适配契约、租户请求/Token/成本预算、并发闸门（背压）以及 JSON 备份恢复工具。数据库驱动、分布式锁和 Redis 原子限流由部署层注入，避免核心库绑定具体基础设施。

Agent 提供 `WithTelemetry` 和 `WithCostMonitor` 观测钩子：每次模型调用记录耗时、模型、Token 用量和错误，并可按模型配置每 1K Token 价格计算 USD 成本。`internal/telemetry` 的无依赖接口可桥接到 OpenTelemetry SDK；框架不内置导出器，避免绑定具体后端。

## 分阶段路线

1. 最小内核（已完成）：接口、会话记忆、可运行 CLI。
2. 真实对话（已完成）：统一模型接口、流式输出、重试/超时、Token 用量和观测。
3. 工具调用：Schema 校验、权限、超时、审计和多轮 tool loop。
4. 知识管理（当前）：文档解析、切分、Embedding、向量检索、混合搜索和引用。
5. 记忆系统（基础层已开始）：短期会话记忆、长期用户记忆、摘要压缩和可控遗忘。
6. 企业能力：多租户、RBAC、密钥管理、限流、持久化、OpenTelemetry 和 HTTP API。

HTTP API 可通过 `internal/api` 使用：注册 `api.NewServer(agent).Handler()` 后挂载到 `http.Server`。提供 `GET /healthz`、`POST /chat` 和 OpenAI 兼容的 `POST /v1/chat/completions`；请求支持 `session_id`、`input`、`messages` 和 `stream` 字段，流式响应使用 SSE。

每一步都会先保持接口稳定，再替换实现，避免把业务代码绑定到某一个模型或数据库。
