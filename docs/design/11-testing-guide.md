# 测试体系

## 测试分层
- Unit tests: 领域逻辑、path normalization、冲突命名、hash diff、权限判断。
- Repository tests: SQLx repository 与 migration，使用测试数据库。
- API integration tests: Axum router + test database + mock/local storage。
- Storage tests: Local FS backend 的 put/read/delete/compose/range。
- Sync tests: change cursor、manifest diff、冲突检测。

## 首期必须覆盖
- 注册、登录、refresh token、登出。
- 创建目录、按路径查询、移动、删除。
- 上传 init、chunk 重传、checksum mismatch、commit。
- 下载完整文件和 Range。
- upload commit 幂等。
- base_version 冲突返回。
- 用户 A 不能访问用户 B 的文件。

## 测试工具建议
- `tokio::test` 用于异步单元测试。
- `tower::ServiceExt` 测试 Axum router。
- `tempfile` 测试 Local FS storage。
- `testcontainers` 或 Docker Compose 测试 PostgreSQL。
- `mockall` 或手写 fake storage 测试 service。

## CI 检查
```bash
cargo fmt --all -- --check
cargo clippy --workspace --all-targets -- -D warnings
cargo test --workspace
```

## 测试数据原则
- 每个测试独立用户和独立根目录。
- 测试完成后清理临时 storage。
- 不依赖执行顺序。
- 不使用生产 JWT secret 或真实云存储凭证。
