# SyncHub 项目总览

## 项目定位
SyncHub 是一个面向开发者工作区的多端同步平台，提供 REST API、WebDAV 兼容访问、增量同步、版本管理和多用户隔离存储。

第一阶段优先服务以下场景：
- 在多台设备之间同步项目配置、笔记、AI 会话上下文和轻量工作区文件。
- 通过 CLI / Agent 自动监听本地变更并上报到服务端。
- 通过 HTTP / WebDAV 与现有工具集成。

## 核心能力
- 多端文件同步（PC / Mobile / Web / CLI Agent）
- REST API 与 WebDAV 兼容访问
- 分片上传与断点续传
- 基于 hash / version 的增量同步
- 文件版本管理与恢复
- 多用户隔离存储
- 本地文件系统与 S3-compatible storage 后端

## 技术栈决策
SyncHub 后端采用 Rust + Axum 作为主技术栈，不再在 Go Gin 与 Rust Axum 之间并行设计。

选择 Axum 的主要原因：
- SyncHub 的核心风险集中在文件 IO、并发上传、增量同步、元数据一致性和后台任务编排，Rust 的所有权、类型系统和错误处理能更早暴露问题。
- Axum 基于 Tokio / Tower / Hyper 生态，适合构建异步 HTTP 服务、中间件、流式上传下载和可组合的服务边界。
- 当前仓库已经是 Cargo Workspace，现有路线图、测试和开发规范也都偏向 Rust 工程化。
- 后续 Agent、CLI、Tauri GUI、同步核心算法可以复用 Rust crate，减少跨语言模型和协议胶水。

Go Gin 方案评估：
- 全 Go 技术栈也是可行方案。服务端、CLI、Agent 都可以用 Go 实现，桌面端也可以选择 Wails 或 Web UI，不存在双语言维护成本。
- Go 的优势是开发速度快、部署简单、并发模型直接、团队招聘和维护成本低，对文件同步服务同样足够成熟。
- 如果项目优先级是尽快交付服务端、CLI 和 Agent，并且不坚持 Tauri / Rust 生态复用，全 Go 是合理选择。
- 当前仍推荐 Rust + Axum，是因为仓库已经采用 Cargo Workspace，未来计划包含 Tauri GUI，并且同步核心、存储抽象、版本一致性这类长期复杂逻辑更能受益于 Rust 的类型系统和编译期约束。

## 目标技术组合
- Language: Rust stable
- Web: Axum
- Async runtime: Tokio
- Middleware / service abstraction: Tower
- DB: PostgreSQL + SQLx
- Cache / queue: Redis（Phase 2 起引入）
- Auth: JWT access token + refresh token；OAuth2 作为后续登录扩展
- Storage: Local FS first，S3 / OSS / MinIO compatible storage later
- API schema: OpenAPI
- Observability: tracing + metrics + structured logs
- Packaging: Docker / Docker Compose

## 系统边界
SyncHub 不负责：
- 在线文档协作编辑
- 富媒体转码、缩略图、内容分析等处理能力
- Git 托管服务本身
- 终端、编辑器或 AI 工具的完整替代品
