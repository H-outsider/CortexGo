# CortexGo

基于 Go 语言构建的企业级 AI Agent 框架，提供对话 AI、知识管理、记忆系统和工具调用能力。

## 当前进度：Step 2 — 真实对话

现在已经可以运行一条完整链路：`交互式输入 → 读取会话记忆 → 调用模型 → 写回记忆 → 输出`。CLI 默认使用本地 Echo 模型，也可以接入任意 OpenAI-compatible 服务；模型返回会保留内容、模型名、finish reason 和 Token 用量。

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

常用参数：`-provider`、`-base-url`、`-model`、`-session`、`-retries`、`-retry-backoff`、`-timeout`、`-stream=false`、`-show-usage`。API key 只从 `CORTEXGO_API_KEY` 读取，避免出现在命令行参数里。

代码边界：

- `internal/provider`：模型适配层，内置 `Echo` 和 `OpenAICompatible`，支持流式输出、Token 用量、请求超时和指数退避重试。
- `internal/memory`：会话记忆存储，当前是线程安全的内存实现。
- `internal/agent`：Agent 编排核心。
- `internal/tool`：工具定义和注册表，下一步接入模型工具调用循环。

## 分阶段路线

1. 最小内核（已完成）：接口、会话记忆、可运行 CLI。
2. 真实对话（当前）：统一模型接口、流式输出、重试/超时、Token 用量和观测。
3. 工具调用：Schema 校验、权限、超时、审计和多轮 tool loop。
4. 知识管理：文档解析、切分、Embedding、向量检索、混合搜索和引用。
5. 记忆系统：短期会话记忆、长期用户记忆、摘要压缩和可控遗忘。
6. 企业能力：多租户、RBAC、密钥管理、限流、持久化、OpenTelemetry 和 HTTP API。

每一步都会先保持接口稳定，再替换实现，避免把业务代码绑定到某一个模型或数据库。
