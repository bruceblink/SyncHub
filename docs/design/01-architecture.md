# 架构设计

## 架构风格
SyncHub 采用模块化单体优先、可演进微服务的架构。Phase 1 不拆分独立服务进程，而是在一个 Axum server 中保持清晰 crate / module 边界；当同步任务、存储任务或 WebDAV 适配出现独立扩缩容需求时，再拆成独立 worker 或服务。

## 运行时组件
- API Server: Axum HTTP 服务，承载 REST API、认证中间件、上传下载流处理和 OpenAPI 文档。
- Auth Module: 用户认证、JWT 签发、refresh token、权限校验。
- File Module: 文件元数据、目录树、版本、上传会话和文件操作编排。
- Sync Engine: 设备状态、变更日志、hash diff、冲突检测和同步计划生成。
- Storage Module: 统一 Storage trait，首期实现 Local FS，后续实现 S3-compatible storage。
- Metadata Store: PostgreSQL，保存用户、设备、文件元数据、chunk、版本、变更日志。
- Background Worker: 处理清理过期上传会话、异步校验、版本压缩、同步事件 fan-out。

## 推荐 Workspace 结构
```text
crates/
  synchub-api/        # Axum server, routes, middleware, request/response DTO
  synchub-core/       # domain model, service traits, business errors
  synchub-auth/       # password, JWT, OAuth2 integration
  synchub-storage/    # Storage trait, local/s3 implementations
  synchub-db/         # SQLx repositories and migrations
  synchub-sync/       # sync planner, conflict detection, change log
  synchub-cli/        # later phase CLI / agent entry
```

## 请求数据流
```text
Client / Agent / WebDAV
  -> Axum Router
  -> Tower middleware: trace, request id, auth, rate limit
  -> API handler
  -> domain service
  -> repository / storage adapter
  -> PostgreSQL + object storage
```

## 上传数据流
1. Client 调用 upload init 创建上传会话。
2. Client 按 chunk 上传数据，服务端流式写入 staging storage，并记录 chunk checksum。
3. Client 调用 commit。
4. 服务端校验 chunk 完整性、合并对象、写入 file version、生成 change event。
5. Sync Engine 根据 change event 为其他设备生成增量同步结果。

## 同步数据流
1. Agent 上报 device_id、last_seen_change_id、本地文件摘要。
2. 服务端查询用户变更日志并与客户端摘要做 diff。
3. 服务端返回需要 pull、push、delete、conflict 的操作列表。
4. Agent 执行上传 / 下载后提交同步结果。
5. 服务端推进设备游标并记录冲突或版本变更。

## 关键设计原则
- 元数据与文件内容解耦：数据库只保存元数据和对象指针，文件内容进入 storage backend。
- 先保证单机正确性，再做分布式扩展：Phase 1 不引入复杂消息系统。
- 所有跨模块依赖通过 trait 或 repository 边界表达，避免 API handler 直接拼接 SQL 或访问文件系统。
- 大文件路径必须使用 streaming，不把完整文件载入内存。
- 每个写入动作要产生可追踪 change event，为同步、审计和后续通知做基础。

## 演进路径
- Phase 1: 单进程 Axum API + PostgreSQL + Local FS。
- Phase 2: 增加 Agent、文件监听和同步引擎。
- Phase 3: 增加后台 worker 和 Redis，用于异步任务、限流和同步事件 fan-out。
- Phase 4: WebDAV adapter 复用 file service 和 auth module。
- Phase 5+: 按实际瓶颈拆分 storage worker、sync worker 或独立 API gateway。

