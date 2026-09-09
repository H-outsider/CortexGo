# CortexGo

CortexGo 是一个使用 Go 构建的模块化 AI Agent 框架。它把模型调用、工具执行、知识检索、记忆、任务调度和可观测性拆成可替换接口，适合学习 Agent 工程，也适合作为企业应用的服务端基础。

## 能力概览

- **模型 Provider**：OpenAI-compatible Chat、流式响应、Tool Calling、超时、重试和 Token 统计；提供 GLM、DeepSeek、Qwen、Groq 预设。
- **ReAct Agent**：模型与工具多轮循环、权限校验、超时、审计，以及 `run/model/tool` 事件流。
- **知识库**：txt/Markdown/HTML/PDF/Office 导入，OCR 检测，Unicode 分块，BM25 风格词法检索、向量检索、Hybrid Search、metadata 过滤、文档版本和增量导入。
- **向量存储**：本地持久化 VectorDatabase，以及 Milvus 适配器。
- **记忆系统**：会话 JSON 持久化、长期事实、隐私策略、TTL、按用户遗忘和 Token 预算上下文压缩。
- **任务平台**：异步 Worker 队列、Cron 调度、GORM/PostgreSQL 任务持久化和 Lease 领取。
- **可靠性与成本**：租户级请求/Token/成本预算、并发背压、Redis/PostgreSQL 适配契约、备份恢复工具。
- **可观测性**：统一 Task/Run/Event/Tool 抽象，OpenTelemetry 桥接接口和 Langfuse ingestion sink。
- **HTTP 服务**：`/healthz`、`/chat`、OpenAI-compatible `/v1/chat/completions`、SSE 流式响应和基础指标。

## 快速开始

```bash
go run ./cmd/cortexgo
```

接入模型：

```bash
CORTEXGO_PROVIDER=openai CORTEXGO_BASE_URL=https://api.openai.com \
CORTEXGO_MODEL=gpt-4o-mini CORTEXGO_API_KEY=your-key \
go run ./cmd/cortexgo
```

```bash
make ci       # 格式检查、vet、测试、竞态测试、构建
make coverage # 覆盖率
make bench    # 基准测试
```

## Provider 与观测

```go
model, _ := provider.NewProvider("glm", provider.ModelConfig{APIKey: "your-key", Model: "glm-4-plus"})
langfuse := &observability.Langfuse{BaseURL: "https://cloud.langfuse.com", PublicKey: os.Getenv("LANGFUSE_PUBLIC_KEY"), SecretKey: os.Getenv("LANGFUSE_SECRET_KEY")}
a := agent.New(model, store, agent.WithTelemetry(langfuse), agent.WithEventSink(langfuse))
```

## 架构

```text
HTTP / CLI → Agent (ReAct + Memory + Tools)
                 ├─ Provider (OpenAI-compatible / GLM / DeepSeek / ...)
                 ├─ Knowledge (Loader / BM25 / Vector / Milvus)
                 ├─ Tasks (Queue / Cron / GORM)
                 ├─ Core (Task / Run / Event / Tool)
                 └─ Observability (OTel bridge / Langfuse)
```

主要目录：`internal/agent`（编排）、`internal/provider`（模型）、`internal/knowledge`（知识库）、`internal/memory`（记忆）、`internal/tasks`（任务）、`internal/core`（抽象）、`internal/observability`（观测）、`internal/api`（HTTP）。

## 生产化说明

框架通过接口隔离数据库、队列、缓存、向量库和观测后端。生产部署建议接入 PostgreSQL、Redis、Milvus、OpenTelemetry Collector，并补充连接池、分布式锁、任务重试/死信、备份加密、敏感字段脱敏和租户隔离。

当前本地实现不提供静态加密、跨进程文件锁、完整 BM25 IDF、复杂 OCR 版面恢复和持久化任务进度；这些能力应由正式基础设施或专用适配器承担。

详细的分阶段学习与开发说明见 [`DEVELOPMENT_STEPS.md`](DEVELOPMENT_STEPS.md)。欢迎提交 Issue 或 Pull Request。
