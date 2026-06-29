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
SyncHub 主技术栈确定为 Go + Gin。

选择 Go 的原因：
- 项目目标之一是训练 Go 工程能力，服务端、CLI 和 Agent 都适合用 Go 实现。
- SyncHub 的主要瓶颈预计在网络、磁盘、数据库和对象存储 IO，不存在必须依赖 Rust 才能解决的性能瓶颈。
- Go 的开发体验、编译速度、部署方式和并发模型更适合快速推进 MVP。
- Go 在 HTTP 服务、后台任务、文件监听、CLI、WebDAV、S3-compatible storage 等场景都有成熟生态。
- 统一使用 Go 可以避免服务端与 Agent 在模型、协议、错误处理和构建流程上的割裂。

Rust 仍是可行方案，但不作为首期主栈。除非后续出现明确的内存安全、极限性能或 Rust 生态依赖，否则不在 MVP 阶段引入 Rust。

## 客户端策略
SyncHub 采用 API-first + Agent-first 设计。服务端 REST / WebDAV / Sync API 定义稳定后，Agent 可以适配 CLI、Web、桌面壳层、移动端或第三方工具。

GUI 不是核心技术栈决策因素，也不绑定任何框架。后续如需 GUI，可在稳定 API 和 Agent 之上选择任意客户端技术。

## 目标技术组合
- Language: Go stable
- Web: Gin
- Runtime: Go runtime + goroutine
- DB: PostgreSQL + sqlc / pgx
- Cache / queue: Redis（Phase 2 起引入）
- Auth: JWT access token + refresh token；OAuth2 作为后续登录扩展
- Storage: Local FS first，S3 / OSS / MinIO compatible storage later
- API schema: OpenAPI
- Observability: slog / zap + OpenTelemetry + metrics
- Packaging: Docker / Docker Compose

## 系统边界
SyncHub 不负责：
- 在线文档协作编辑
- 富媒体转码、缩略图、内容分析等处理能力
- Git 托管服务本身
- 终端、编辑器或 AI 工具的完整替代品
