# 模块设计

## 模块划分

### synchub-api
职责：
- Axum router、handler、extractor、middleware。
- 请求参数校验和响应 DTO。
- OpenAPI schema 暴露。
- 将 HTTP 错误映射为统一错误响应。

不负责：
- 直接拼 SQL。
- 直接访问文件系统或对象存储。
- 承载复杂业务逻辑。

### synchub-core
职责：
- 领域模型：User、Device、FileNode、FileVersion、UploadSession、ChangeEvent。
- 业务 service trait 和错误类型。
- 通用 ID、时间、分页、path normalization。
- 跨模块共享的 policy，例如 chunk size、文件名规则、冲突命名规则。

### synchub-auth
职责：
- 密码哈希和校验。
- JWT access token 签发与校验。
- refresh token 生命周期。
- OAuth2 登录扩展点。
- 用户上下文和权限检查。

### synchub-db
职责：
- SQLx pool 初始化。
- migration。
- repository 实现。
- transaction helper。

Repository 边界：
- `UserRepository`
- `TokenRepository`
- `FileRepository`
- `UploadRepository`
- `ChangeRepository`
- `DeviceRepository`

### synchub-storage
职责：
- Storage trait。
- Local FS backend。
- S3-compatible backend 扩展。
- staging object、commit object、delete object。
- streaming read / write。

建议 trait：
```rust
pub trait Storage: Send + Sync + 'static {
    async fn put_chunk(&self, key: &str, bytes: ByteStream, checksum: &str) -> Result<()>;
    async fn compose(&self, target_key: &str, chunk_keys: &[String]) -> Result<()>;
    async fn read(&self, key: &str, range: Option<ByteRange>) -> Result<ByteStream>;
    async fn delete(&self, key: &str) -> Result<()>;
}
```

### synchub-sync
职责：
- 设备注册和同步游标。
- change event 查询和 diff。
- hash / version compare。
- 冲突检测。
- 同步计划生成。

### synchub-cli / agent
职责：
- 用户登录和 token 保存。
- 本地配置。
- 文件监听。
- 本地 hash 扫描。
- 与服务端执行 pull / push / ack。

## 依赖方向
```text
synchub-api
  -> synchub-auth
  -> synchub-core
  -> synchub-db
  -> synchub-storage
  -> synchub-sync

synchub-db -> synchub-core
synchub-storage -> synchub-core
synchub-sync -> synchub-core
synchub-auth -> synchub-core
```

API 层可以组合多个 service，但底层模块不要反向依赖 API DTO。

## 错误处理
- 领域错误定义在 `synchub-core`。
- DB、storage、auth 的底层错误在模块内转换为领域错误。
- API 层负责把领域错误转换为 HTTP status 和错误码。

## 配置
统一 `AppConfig`：
- `database_url`
- `jwt_secret`
- `storage_backend`
- `local_storage_root`
- `upload_chunk_size`
- `upload_session_ttl_seconds`
- `redis_url`（Phase 2+）
