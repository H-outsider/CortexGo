# CortexGo

基于 Go 语言构建的企业级 AI Agent 框架，提供对话 AI、知识管理、记忆系统和工具调用能力。

## 当前进度：Step 4 — 知识管理基础层

现在已经可以运行一条完整链路：`交互式输入 → 读取会话记忆 → 调用模型 → 执行工具 → 写回记忆 → 输出`。CLI 默认使用本地 Echo 模型，也可以接入任意 OpenAI-compatible 服务；模型返回会保留内容、模型名、finish reason 和 Token 用量。

```bash
go run ./cmd/cortexgo
go test ./...
go run ./cmd/cortexgo -show-usage
```

接入真实模型：

```bash
CORTEXGO_PROVIDER=openai \
CORTEXGO_BASE_URL=https://api.openai.com \
CORTEXGO_MODEL=gpt-4o-mini \
CORTEXGO_API_KEY=your-key \
go run ./cmd/cortexgo
```

知识库导入与查询（默认持久化到 `.cortexgo-knowledge.json`）：

```bash
CORTEXGO_PROVIDER=openai \
CORTEXGO_BASE_URL=https://api.openai.com \
CORTEXGO_MODEL=gpt-4o-mini \
CORTEXGO_EMBEDDING_MODEL=text-embedding-3-small \
CORTEXGO_API_KEY=your-key \
go run ./cmd/cortexgo -knowledge-file=README.md -knowledge-query="向量检索"
```

可通过 `-knowledge-index` 或 `CORTEXGO_KNOWLEDGE_INDEX` 指定索引文件路径。

常用参数：`-provider`、`-base-url`、`-model`、`-embedding-model`、`-session`、`-memory-file`、`-retries`、`-retry-backoff`、`-timeout`、`-stream=false`、`-show-usage`、`-tools=false`。API key 只从 `CORTEXGO_API_KEY` 读取，避免出现在命令行参数里。

代码边界：

- `internal/provider`：模型适配层，内置 `Echo` 和 `OpenAICompatible`，支持 Chat/Embeddings、流式输出、Token 用量、请求超时和指数退避重试。
- `internal/memory`：会话记忆存储，提供线程安全的内存实现和本地 JSON 文件实现。
- `internal/agent`：Agent 编排核心。
- `internal/tool`：工具定义、Schema 校验、注册表和 `current_time` 内置工具。

当前工具调用已支持基础 Schema 校验、权限策略、超时、多轮循环和结构化审计；更细粒度的企业 RBAC 与持久化审计仍在后续迭代中。

长期记忆基础层提供用户事实存储和可控遗忘接口；事实默认只存在内存中，后续再接入持久化与隐私策略。

会话上下文提供可注入摘要函数的压缩能力，可将旧消息合并为摘要并保留最近对话，避免上下文无限增长。

Agent 可通过消息数上限和 `SummaryFunc` 自动触发上下文压缩；压缩后的历史会写回支持替换的 Memory Store，避免每轮重复摘要。

知识管理基础层已支持 Unicode 文档切分、线程安全内存索引、关键词检索、向量检索和混合搜索；Embedding provider 与持久化向量数据库通过接口接入。

## 分阶段路线

1. 最小内核（已完成）：接口、会话记忆、可运行 CLI。
2. 真实对话（已完成）：统一模型接口、流式输出、重试/超时、Token 用量和观测。
3. 工具调用：Schema 校验、权限、超时、审计和多轮 tool loop。
4. 知识管理（当前）：文档解析、切分、Embedding、向量检索、混合搜索和引用。
5. 记忆系统（基础层已开始）：短期会话记忆、长期用户记忆、摘要压缩和可控遗忘。
6. 企业能力：多租户、RBAC、密钥管理、限流、持久化、OpenTelemetry 和 HTTP API。

每一步都会先保持接口稳定，再替换实现，避免把业务代码绑定到某一个模型或数据库。
