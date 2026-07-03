# 模块设计

## 模块划分

### cmd/synchub-api

职责：

- API server 进程入口。
- 加载配置、初始化日志、数据库、storage、router。
- 处理 graceful shutdown。

### cmd/synchub-agent

职责：

- Agent 进程入口。
- 文件监听、本地 manifest 扫描、同步循环。
- 与服务端执行 pull / push / ack。

### cmd/synchub-cli

职责：

- CLI 入口。
- login、workspace init、sync、status、pull、push 等命令。
- 与 Agent local API 或服务端 API 交互。

### internal/api

职责：

- Gin router、handler、middleware。
- 请求参数校验和响应 DTO。
- OpenAPI schema 暴露。
- 将业务错误映射为统一 HTTP 响应。

不负责：

- 直接拼 SQL。
- 直接访问文件系统或对象存储。
- 承载复杂业务逻辑。

### internal/domain

职责：

- 领域模型：User、Device、FileNode、FileVersion、UploadSession、ChangeEvent。
- 业务错误类型。
- 通用 ID、时间、分页、path normalization。
- 跨模块共享 policy，例如 chunk size、文件名规则、冲突命名规则。

### internal/auth

职责：

- 密码哈希和校验。
- JWT access token 签发与校验。
- refresh token 生命周期。
- OAuth2 登录扩展点。
- 用户上下文和权限检查。

### internal/db

职责：

- SQLite 开发库初始化和 schema bootstrap。
- repository 实现。
- transaction helper。

Later：

- PostgreSQL / MySQL adapter 的连接初始化。
- migration 运行入口或 migration 工具集成。
- sqlc 生成 query 的封装（大型关系型数据库 adapter 阶段）。

Repository 边界：

- `UserRepository`
- `TokenRepository`
- `FileRepository`
- `UploadRepository`
- `ChangeRepository`
- `DeviceRepository`

### internal/storage

职责：

- Storage interface。
- Local FS backend。
- staging object、commit object、delete object。
- streaming read / write。

Later：

- S3-compatible backend 扩展。

建议 interface：

```go
type Storage interface {
    PutChunk(ctx context.Context, key string, r io.Reader, checksum string) error
    Compose(ctx context.Context, targetKey string, chunkKeys []string) error
    Read(ctx context.Context, key string, br *ByteRange) (io.ReadCloser, ObjectInfo, error)
    Delete(ctx context.Context, key string) error
}
```

### internal/file

职责：

- 文件元数据应用服务。
- 目录创建、移动、删除。
- 上传会话编排。
- upload commit 事务流程。
- 文件下载权限校验和 storage 调用。

### internal/sync

职责：

- 设备注册和同步游标。
- change event 查询和 diff。
- hash / version compare。
- 冲突检测。
- 同步计划生成。

### internal/worker

职责：

- 清理过期 upload session。
- 清理 staging object。
- 最小版本保留策略任务。

Later：

- 孤儿对象扫描。
- 队列化异步任务。

### pkg/client

职责：

- 可选的 Go API client。
- 供 CLI / Agent-style commands 复用。
- 只暴露稳定 API，不暴露 internal 包。

## 依赖方向

```text
cmd/*
  -> internal/api
  -> internal/config

internal/api
  -> internal/auth
  -> internal/file
  -> internal/sync
  -> internal/domain

internal/file
  -> internal/db
  -> internal/storage
  -> internal/domain

internal/sync
  -> internal/db
  -> internal/domain

internal/auth
  -> internal/db
  -> internal/domain

internal/db -> internal/domain
internal/storage -> internal/domain
```

API 层可以组合多个 service，但底层模块不要反向依赖 API DTO。

## 错误处理

- 业务错误定义在 `internal/domain`。
- DB、storage、auth 的底层错误在模块内转换为业务错误。
- API 层负责把业务错误转换为 HTTP status 和错误码。
- 使用 `errors.Is` / `errors.As` 保持错误可判定。
- 日志记录错误链路，HTTP 响应不泄漏敏感细节。

## 配置

统一 `AppConfig`：

- `database_driver`
- `database_url`
- `jwt_secret`
- `storage_backend`
- `local_storage_root`
- `upload_chunk_size`
- `upload_session_ttl_seconds`
- `redis_url`（Phase 2+）
