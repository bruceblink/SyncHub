# 架构设计

## 架构风格

SyncHub 采用单体优先架构。当前 MVP 不拆分独立服务进程，也不为未来微服务提前铺接口；只有当 PostgreSQL + Local FS 的个人同步闭环稳定后，才评估是否拆分 worker 或 adapter。

## 运行时组件

- API Server: Gin HTTP 服务，承载 REST API、认证中间件、上传下载流处理和 OpenAPI 文档。
- Auth Module: 用户认证、JWT 签发、refresh token、权限校验。
- File Module: 文件元数据、目录树、版本、上传会话和文件操作编排。
- Sync Engine: 设备状态、变更日志、hash diff、冲突检测和同步计划生成。
- Storage Module: 统一 Storage interface，当前只实现 Local FS。
- Metadata Store: PostgreSQL。
- Background Worker: 只处理当前需要的进程内周期任务，避免提前引入队列系统。

## 推荐 Go Module 结构

```text
cmd/
  synchub-api/        # Gin API server entry
  synchub-api/        # API server entrypoint
internal/
  api/                # routes, handlers, middleware, request/response DTO
  auth/               # password, JWT, OAuth2 integration
  config/             # typed config and env loading
  domain/             # domain model and business errors
  file/               # file application service
  storage/            # Storage interface, local implementation
  db/                 # PostgreSQL migrations, repository, transactions
  sync/               # sync planner, conflict detection, change log
  worker/             # background jobs
pkg/
  client/             # Go API client used by CLI
migrations/
```

## 请求数据流

```text
CLI / API Client
  -> Gin Router
  -> middleware: trace, request id, auth, rate limit
  -> API handler
  -> domain service
  -> repository / storage adapter
  -> PostgreSQL metadata database + Local FS object storage
```

## 上传数据流

1. Client 调用 upload init 创建上传会话。
2. Client 按 chunk 上传数据，服务端流式写入 staging storage，并记录 chunk checksum。
3. Client 调用 commit。
4. 服务端校验 chunk 完整性、合并对象、写入 file version、生成 change event。
5. Sync Engine 根据 change event 为其他设备生成增量同步结果。

## 同步数据流

1. SyncHub Desktop 上报 device_id、last_seen_change_id、本地文件摘要。
2. 服务端查询用户变更日志并与客户端摘要做 diff。
3. 服务端返回需要 pull、push、delete、conflict 的操作列表。
4. SyncHub Desktop 执行上传 / 下载后提交同步结果。
5. 服务端推进设备游标并记录冲突或版本变更。

## 关键设计原则

- 元数据与文件内容解耦：数据库只保存元数据和对象指针，文件内容进入 storage backend。
- 先保证单机正确性，再做分布式扩展：Phase 1 不引入复杂消息系统。
- 所有跨模块依赖通过 interface 或 repository 边界表达，避免 API handler 直接拼接 SQL 或访问文件系统。
- 大文件路径必须使用 streaming，不把完整文件载入内存。
- 每个写入动作要产生可追踪 change event，为同步、审计和后续通知做基础。

## 演进路径

- Phase 1: 单进程 Gin API + PostgreSQL + Local FS。
- Phase 2: 完成桌面后台同步、文件扫描和同步引擎。
- Phase 3: 补齐基础版本历史、恢复、pin 和最小版本清理。
- Later: WebDAV、S3-compatible storage、MySQL、Redis 队列和独立 worker，只有在 MVP 稳定后再评估。
