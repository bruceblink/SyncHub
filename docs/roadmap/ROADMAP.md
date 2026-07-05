# SyncHub Roadmap

## 技术栈结论

SyncHub 主技术栈确定为 Go + Gin。

选择 Go 的原因：

- 项目目标之一是训练 Go 工程能力。
- Go 在本项目中的开发体验和迭代效率优于 Rust。
- SyncHub 的主要瓶颈预计在网络、磁盘、数据库和对象存储 IO，不存在必须依赖 Rust 才能解决的性能瓶颈。
- 服务端、CLI 和 Agent 可以统一使用 Go，减少模型、协议、错误处理和构建流程割裂。

核心技术组合：

- Language: Go stable
- Web: Gin
- DB: SQLite for current MVP; PostgreSQL / MySQL adapters later only if SQLite limits are proven
- Migration: embedded SQLite bootstrap first; external migration tools are deferred
- Auth: JWT access token + refresh token
- Storage: Local FS for current MVP; S3 / OSS / MinIO compatible storage is deferred
- API schema: OpenAPI
- Observability: slog / zap + OpenTelemetry + metrics
- Packaging: Docker image release / Linux Docker Compose deployment

## 总体目标

先做一个可靠的个人开发者工作区同步闭环：单用户、多设备、SQLite、Local FS、CLI / Agent。

优先级顺序：

1. 文件上传下载正确。
2. 元数据、版本和变更日志正确。
3. Agent 能稳定增量同步。
4. 冲突不会静默覆盖用户文件。
5. Docker 镜像发布、Linux Compose 部署和恢复流程稳定。

当前阶段明确不做：

- WebDAV adapter。
- S3 / OSS / MinIO storage backend。
- 团队空间、共享目录和复杂权限模型。
- Agent SDK、第三方客户端适配层、Web / desktop / mobile 客户端规划。
- PostgreSQL / MySQL 生产级 adapter，除非 SQLite 单机闭环已经稳定且出现明确瓶颈。

## Phase 0: Go 工程基础

目标：建立可持续开发的 Go module、本地运行环境和基础工程规范。

任务：

- 创建 Go module。
- 建立目录结构：
  - `cmd/synchub-api`
  - `cmd/synchub-agent`
  - `cmd/synchub-cli`
  - `internal/api`
  - `internal/auth`
  - `internal/config`
  - `internal/domain`
  - `internal/db`
  - `internal/file`
  - `internal/storage`
  - `internal/sync`
  - `internal/worker`
  - `pkg/client`
  - `migrations`
- 引入基础依赖：gin、SQLite driver、jwt、argon2、uuid、OpenTelemetry。
- 建立配置加载：环境变量 + typed config。
- 建立错误模型：domain error -> API error response。
- 建立本地 SQLite 开发数据库、Docker 镜像构建和 Linux Compose API 部署。
- 建立 CI 命令：fmt、vet、test。

验收标准：

- `go test ./...` 通过。
- `go vet ./...` 通过。
- `GET /healthz` 和 `GET /readyz` 可用。
- 空数据库可以执行 migration。

## Phase 1: API Server MVP

目标：完成可登录、可上传、可下载、可管理文件元数据的最小服务端。

### 1.1 Auth

任务：

- users、refresh_tokens migration。
- SQLite repository wrapper。
- 注册、登录、refresh、logout API。
- Argon2id password hash。
- JWT access token 和 refresh token。
- Gin auth middleware。

验收标准：

- 用户可注册登录。
- access token 过期后可 refresh。
- refresh token 可撤销。
- 未授权请求返回统一错误。

### 1.2 File Metadata

任务：

- file_nodes、file_versions、change_events migration。
- SQLite repository wrapper。
- 目录创建、列表、按路径查询、移动、删除 API。
- path normalization。
- 用户级数据隔离。
- 乐观锁版本号。

验收标准：

- 同一用户路径唯一。
- 不同用户可以拥有相同路径。
- 删除为 soft delete 并生成 change event。
- 用户不能访问其他用户文件。

### 1.3 Local Storage

任务：

- 定义 Storage interface。
- 实现 Local FS backend。
- 支持 put chunk、compose、read、delete。
- 支持 Range read。
- 使用临时目录完成 storage tests。

验收标准：

- 大文件读写不需要完整载入内存。
- Range 下载返回正确字节范围。
- compose 后 sha256 与客户端声明一致。

### 1.4 Chunk Upload

任务：

- upload_sessions、upload_chunks migration。
- upload init / put chunk / status / commit API。
- chunk checksum 校验。
- commit 事务：锁定 upload session、校验 chunks、写版本、更新 file_nodes、写 change_events。
- 幂等提交和 chunk 重传处理。
- 过期 session 清理任务：API 进程内轻量 worker 周期性将过期 pending session 标记为 expired。

验收标准：

- 支持断点续传。
- checksum mismatch 可被检测。
- commit 重试不会产生重复版本。
- base_version 过旧时返回冲突，不覆盖现有文件。

### 1.5 Download

任务：

- download content API。
- ETag / If-None-Match。
- Range header。
- 下载权限校验。

验收标准：

- 完整下载与 Range 下载都可用。
- ETag 命中返回 304。
- 下载不存在或无权限文件返回统一错误。

## Phase 2: Agent 与增量同步

目标：打通多设备同步闭环。

### 2.1 Device Model

任务：

- devices migration。
- 设备注册、心跳、同步游标 API。
- device token 或绑定当前用户 token。

验收标准：

- 用户可注册多个设备。
- 服务端可记录设备最近在线时间和 last_applied_change_id。

### 2.2 Change Feed

任务：

- change_events repository。
- 所有文件 create/update/move/delete/restore 写 change event。
- 拉取 changes API。
- ack API。

验收标准：

- 设备可按游标拉取增量变更。
- ack 后游标推进。
- 游标失效时返回明确错误，引导 full scan。

### 2.3 CLI / Agent MVP

任务：

- CLI login：调用服务端登录 API，并将 token 写入本地配置。
- workspace init：创建本地 `.synchub/workspace.json`，记录本地根目录、远端路径和登录上下文。
- 本地 manifest 扫描：生成 `.synchub/manifest.json`，记录 path、relative_path、size、mtime、sha256。
- 文件监听。
- push 本地新增 / 修改文件。
- pull 服务端变更。
- sync status：读取工作区配置和本地 manifest，展示本地同步准备状态。

验收标准：

- 两台设备可以通过服务端同步同一目录。
- 新增、修改、删除可以在另一端体现。
- 中断后重新执行 sync 可继续。

### 2.4 Conflict Detection

任务：

- sync_conflicts migration。
- 基于 base_version 和 sha256 检测冲突。
- keep-both 默认策略。
- 冲突文件命名。
- CLI 展示冲突状态。

验收标准：

- 并发修改不会静默覆盖。
- 冲突版本可保留并可查询。

## Phase 3: 版本历史与恢复

目标：让 SyncHub 具备可靠的版本管理能力。

任务：

- 版本历史 API。
- restore version API。
- 版本 pin。
- 版本保留策略配置。
- 后台清理过期历史版本记录。

验收标准：

- 用户可查看文件历史版本。
- 用户可恢复指定版本。
- 删除 / 恢复都会产生 change event。
- 清理任务不会删除当前版本和 pinned version。
- 孤儿对象扫描保留在 Later，不进入当前 MVP 清理闭环。

## Later: WebDAV Adapter

目标：让系统能被支持 WebDAV 的客户端挂载或访问。该方向只作为长期扩展记录，不进入当前开发队列。

任务：

- WebDAV auth 集成。
- PROPFIND 映射目录列表和元数据。
- GET 映射下载。
- PUT 映射上传。
- MOVE 映射移动 / 重命名。
- DELETE 映射 soft delete。

验收标准：

- 常见 WebDAV 客户端可浏览目录。
- 上传、下载、重命名、删除可用。
- WebDAV 写入同样生成版本和 change event。

## Later: 云存储与后台任务

目标：支持更接近生产部署的对象存储和异步任务。当前 MVP 只保留 Local FS 和进程内轻量 worker。

任务：

- S3 / OSS / MinIO storage backend。
- Redis 任务队列或轻量 worker loop。
- 上传 staging 清理。
- 孤儿对象扫描。
- 版本清理任务。
- 指标：上传耗时、下载耗时、同步延迟、错误率。

验收标准：

- Local FS 与 S3 backend 通过同一 storage test suite。
- 后台任务可重复执行且幂等。
- readyz 能检查 DB 和 storage。

## Later: Client Adapter

目标：在稳定 CLI / Agent 能力之上，再考虑任意客户端形态接入。GUI 不是当前路线。

任务：

- 提供 Agent local API 或 IPC，用于外部客户端读取同步状态和触发操作。
- 固化客户端配置文件格式。
- 提供客户端适配文档。
- 提供 Web / desktop / mobile 接入建议，但不指定 GUI 框架。
- 暴露冲突列表、版本历史、同步进度和错误状态。

验收标准：

- 第三方客户端可以通过稳定 API 获取同步状态、冲突和版本信息。
- 客户端可以触发 login、workspace init、sync、pause、resume。
- 不引入对特定 GUI 框架的强依赖。

## Long-term

- Team Workspace。
- 共享目录与权限模型。
- Plugin System。
- 端到端加密。
- 搜索索引。
- 更细粒度的忽略规则。
- 面向 AI session 的结构化同步策略。

## 近期执行顺序

1. 创建 Go module 和目录骨架。
2. 实现 `cmd/synchub-api` 的 health / ready endpoint。
3. 建立 SQLite 开发 schema 和 users / refresh_tokens 表。
4. 保留数据库 repository 边界，后续配置 pgx / MySQL driver、sqlc、migration 工具和 Docker Compose。
5. 实现 Auth MVP。
6. 实现 file_nodes / file_versions / change_events migration。
7. 实现 Storage interface 和 Local FS backend。
8. 实现 chunk upload 闭环。

完成以上 8 步后，SyncHub 就具备继续做 Agent 同步的服务端基础。
