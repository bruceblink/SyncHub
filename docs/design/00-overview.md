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

不选择 Go Gin 作为主栈的原因：
- Gin 更适合快速交付常规 CRUD / REST 服务，但 SyncHub 的长期重点不是普通 API，而是可靠的文件同步和存储一致性。
- 若服务端用 Go、Agent / CLI / GUI 核心用 Rust，会过早引入双语言维护成本。
- Go 生态仍可作为未来边缘组件或运维工具选项，但不作为核心后端栈。

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
