# 测试体系

## 测试分层

- Unit tests: 领域逻辑、path normalization、冲突命名、hash diff、权限判断。
- Repository tests: SQLite repository、后续 sqlc query wrapper、repository 与 migration，使用测试数据库。
- API integration tests: Gin router + test database + mock/local storage。
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

- `testing` 标准库作为默认测试框架。
- `net/http/httptest` 测试 Gin router。
- `t.TempDir()` 测试 Local FS storage。
- SQLite 使用 `t.TempDir()` 创建临时数据库文件。
- `testcontainers-go` 或 Docker Compose 测试后续 PostgreSQL / MySQL adapter。
- 手写 fake storage / fake repository 测试 service。

## CI 检查

```bash
go fmt ./...
go vet ./...
go test ./...
```

## 测试数据原则

- 每个测试独立用户和独立根目录。
- 测试完成后清理临时 storage。
- 不依赖执行顺序。
- 不使用生产 JWT secret 或真实云存储凭证。
