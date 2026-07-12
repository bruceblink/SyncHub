# SyncHub 项目总览

## 项目定位

SyncHub 是一个面向个人开发者工作区的多设备同步工具，当前重点是 REST API、桌面后台同步、增量同步、版本恢复和用户级隔离存储。
发布和部署主路径是 Linux 服务器上的 Docker 镜像；Windows 仅作为本地开发和验证环境。

第一阶段优先服务以下场景：

- 在多台设备之间同步项目配置、笔记、AI 会话上下文和轻量工作区文件。
- 通过配套 SyncHub Desktop 自动同步本地变更并拉取远端更新。
- 通过 HTTP API 支持脚本化和自动化操作。

## 核心能力

- 多设备文件同步（SyncHub Desktop）
- REST API 与 CLI 访问
- 分片上传与断点续传
- 基于 hash / version 的增量同步
- 文件版本管理与恢复
- 多用户隔离存储
- 本地文件系统存储

## 技术栈决策

SyncHub 主技术栈确定为 Go + Gin。

选择 Go 的原因：

- 服务端使用 Go，桌面同步客户端使用 Rust 与 GPUI。
- SyncHub 的主要瓶颈预计在网络、磁盘、数据库和对象存储 IO，不存在必须依赖 Rust 才能解决的性能瓶颈。
- Go 的开发体验、编译速度、部署方式和并发模型更适合快速推进 MVP。
- Go 在 HTTP 服务、后台任务、文件监听和 CLI 等场景都有成熟生态。
- 统一使用 Go 可以避免服务端与客户端同步逻辑在模型、协议、错误处理和构建流程上的割裂。

Rust 仍是可行方案，但不作为首期主栈。除非后续出现明确的内存安全、极限性能或 Rust 生态依赖，否则不在 MVP 阶段引入 Rust。

## 客户端策略

SyncHub 采用 API-first 设计。服务端提供 REST API，配套 SyncHub Desktop 直接承载登录、工作区、文件、版本、冲突和后台同步流程。

GUI、WebDAV、S3 和第三方客户端适配都属于 Later，不参与当前 MVP 主线。

## 目标技术组合

- Language: Go stable
- Web: Gin
- Runtime: Go runtime + goroutine
- DB: PostgreSQL for server metadata in every environment
- Cache / queue: none for current MVP
- Auth: JWT access token + refresh token；OAuth2 作为后续登录扩展
- Storage: Local FS
- API schema: OpenAPI
- Observability: slog / zap + OpenTelemetry + metrics
- Packaging: Docker image release / Linux Docker Compose deployment

## 系统边界

SyncHub 不负责：

- 在线文档协作编辑
- 富媒体转码、缩略图、内容分析等处理能力
- Git 托管服务本身
- 终端、编辑器或 AI 工具的完整替代品
