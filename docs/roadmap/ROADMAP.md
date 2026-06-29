# SyncHub Roadmap

## 技术栈结论
默认推荐后端主栈选择 Rust + Axum，但项目仍处于调研阶段，最终选择应根据团队熟练度、MVP 时间压力和桌面端路线确认。

推荐 Rust + Axum 的理由是：项目核心是文件同步、流式 IO、版本一致性、后台任务和长期可维护的多端客户端，而不是普通 CRUD API。Rust + Axum 能让服务端、同步核心、CLI、Agent 和未来 Tauri GUI 共享领域模型与同步逻辑，并把更多一致性问题前移到编译期。

全 Go + Gin 也是可行方案。服务端、CLI、Agent 可以统一用 Go 实现，桌面端可选择 Wails 或 Web UI，这种情况下不存在双语言维护成本。它的优势是开发速度快、部署简单、并发模型直接、团队维护成本低。

决策规则：
- 选择 Rust + Axum：更重视长期正确性、类型约束、Tauri 生态和跨端核心逻辑复用。
- 选择全 Go + Gin：更重视早期交付速度、团队上手成本、部署简洁性和服务端 / Agent 快速迭代。
- 若团队对 Rust 熟练度不足，或者 MVP 时间压力明显，应优先考虑全 Go。

## 总体目标
先做一个可靠的单用户 / 多设备同步闭环，再逐步扩展 WebDAV、版本恢复、团队空间和云对象存储。

优先级顺序：
1. 文件上传下载正确。
2. 元数据、版本和变更日志正确。
3. Agent 能稳定增量同步。
4. 冲突不会静默覆盖用户文件。
5. 再补 WebDAV、GUI、团队能力。

## Phase 0: 工程基础

目标：建立可持续开发的 Rust workspace 和本地运行环境。

任务：
- 创建 workspace crates:
  - `synchub-api`
  - `synchub-core`
  - `synchub-auth`
  - `synchub-db`
  - `synchub-storage`
  - `synchub-sync`
- 引入基础依赖：axum、tokio、tower-http、tracing、serde、thiserror、sqlx、uuid、time。
- 建立配置加载：环境变量 + typed config。
- 建立错误模型：domain error -> API error response。
- 建立 Docker Compose：PostgreSQL + API。
- 建立 CI 命令：fmt、clippy、test。

验收标准：
- `cargo test --workspace` 通过。
- `cargo clippy --workspace --all-targets -- -D warnings` 通过。
- `GET /healthz` 和 `GET /readyz` 可用。
- 空数据库可以执行 migration。

## Phase 1: API Server MVP

目标：完成可登录、可上传、可下载、可管理文件元数据的最小服务端。

### 1.1 Auth
任务：
- users、refresh_tokens migration。
- 注册、登录、refresh、logout API。
- Argon2id password hash。
- JWT access token 和 refresh token。
- Axum auth extractor。

验收标准：
- 用户可注册登录。
- access token 过期后可 refresh。
- refresh token 可撤销。
- 未授权请求返回统一错误。

### 1.2 File Metadata
任务：
- file_nodes、file_versions migration。
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
- 定义 Storage trait。
- 实现 Local FS backend。
- 支持 put chunk、compose、read、delete。
- 支持 Range read。
- 使用 tempfile 完成 storage tests。

验收标准：
- 大文件读写不需要完整载入内存。
- Range 下载返回正确字节范围。
- compose 后 sha256 与客户端声明一致。

### 1.4 Chunk Upload
任务：
- upload_sessions、upload_chunks migration。
- upload init / put chunk / status / commit API。
- chunk checksum 校验。
- commit 事务：写版本、更新 file_nodes、写 change_events。
- 幂等提交和 chunk 重传处理。
- 过期 session 清理任务。

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
- change_events migration 和 repository。
- 所有文件 create/update/move/delete/restore 写 change event。
- 拉取 changes API。
- ack API。

验收标准：
- 设备可按游标拉取增量变更。
- ack 后游标推进。
- 游标失效时返回明确错误，引导 full scan。

### 2.3 CLI / Agent MVP
任务：
- CLI login。
- workspace init。
- 本地 manifest 扫描：path、size、mtime、sha256。
- push 本地新增 / 修改文件。
- pull 服务端变更。
- sync status。

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
- 后台清理过期版本和孤儿对象。

验收标准：
- 用户可查看文件历史版本。
- 用户可恢复指定版本。
- 删除 / 恢复都会产生 change event。
- 清理任务不会删除当前版本和 pinned version。

## Phase 4: WebDAV Adapter

目标：让系统能被支持 WebDAV 的客户端挂载或访问。

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

## Phase 5: 云存储与后台任务

目标：支持更接近生产部署的对象存储和异步任务。

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

## Phase 6: Tauri GUI

目标：提供桌面端可视化同步体验。

任务：
- 登录与工作区选择。
- 同步状态面板。
- 冲突列表与处理。
- 版本历史浏览。
- Agent 后台运行控制。

验收标准：
- 用户无需命令行即可完成登录、选择目录、启动同步。
- GUI 能展示同步进度、错误和冲突。

## Long-term
- Team Workspace。
- 共享目录与权限模型。
- Plugin System。
- 端到端加密。
- 搜索索引。
- 更细粒度的忽略规则。
- 面向 AI session 的结构化同步策略。

## 近期执行顺序
若最终选择 Rust + Axum，优先执行：
1. 创建 workspace crates 和基础依赖。
2. 实现 `synchub-api` 的 health / ready endpoint。
3. 建立 PostgreSQL migration 目录和 users / refresh_tokens 表。
4. 实现 Auth MVP。
5. 实现 file_nodes / file_versions / change_events migration。
6. 实现 Local FS Storage trait。
7. 实现 chunk upload 闭环。

完成以上 7 步后，SyncHub 就具备继续做 Agent 同步的服务端基础。

若最终选择全 Go + Gin，对应执行顺序调整为：
1. 创建 Go module 和 internal package 边界。
2. 实现 `cmd/synchub-api` 的 health / ready endpoint。
3. 建立 PostgreSQL migration 目录和 users / refresh_tokens 表。
4. 实现 Auth MVP。
5. 实现 file_nodes / file_versions / change_events migration。
6. 实现 Storage interface 和 Local FS backend。
7. 实现 chunk upload 闭环。
