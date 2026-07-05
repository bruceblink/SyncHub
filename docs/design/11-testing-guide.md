# 测试体系

## 测试分层

- Unit tests: 领域逻辑、path normalization、冲突命名、hash diff、权限判断。
- Repository tests: SQLite repository、后续 sqlc query wrapper、repository 与 migration，使用测试数据库。
- API integration tests: Gin router + test database + mock/local storage。
- Storage tests: Local FS backend 的 put/read/delete/compose/range。
- Sync tests: change cursor、manifest diff、冲突检测。
- CLI E2E tests: 使用 SQLite API test server 和临时 workspace 验证双设备同步闭环。

## 首期必须覆盖

- 注册、登录、refresh token、登出。
- 创建目录、按路径查询、移动、删除。
- 上传 init、chunk 重传、checksum mismatch、commit。
- 下载完整文件和 Range。
- upload commit 幂等。
- base_version 冲突返回。
- 用户 A 不能访问用户 B 的文件。
- 版本历史、restore、pin / unpin。
- sync conflicts 查询和 resolution 更新。
- 两个本地 workspace 通过同一个 SQLite API server 完成 push / pull。

## 测试工具建议

- `testing` 标准库作为默认测试框架。
- `net/http/httptest` 测试 Gin router。
- `t.TempDir()` 测试 Local FS storage。
- SQLite 使用 `t.TempDir()` 创建临时数据库文件。
- `testcontainers-go` 或 Docker Compose 测试后续 PostgreSQL / MySQL adapter。
- 手写 fake storage / fake repository 测试 service。

## CI 检查

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-mvp.ps1
```

`scripts/test-mvp.ps1` 串联 `go fmt ./...`、`go vet ./...`、`go test ./...`、本地 API smoke test 和本地备份恢复 smoke test。

Docker 镜像交付链路可以单独验证：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-docker-image.ps1 -Version 0.1.0 -Image synchub:0.1.0
```

该脚本会构建镜像、校验镜像内 CLI 版本、启动 API container，并验证 `/readyz` 和 `/version`。Release workflow 将它作为 Docker 镜像发布前的必过 smoke gate。

Docker Compose 本地部署链路也可以单独验证：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-docker-compose.ps1
```

该脚本会使用独立 compose project、临时端口和独立 volume，执行 `docker compose build`、`up`、`GET /readyz`，最后自动 `down --volumes` 清理。它依赖 Docker Desktop 和当前网络可拉取基础镜像，因此不放入默认 MVP 检查链路；CI 中作为独立 `docker-compose` job 在 Go 测试通过后执行。

## 测试数据原则

- 每个测试独立用户和独立根目录。
- 测试完成后清理临时 storage。
- 不依赖执行顺序。
- 不使用生产 JWT secret 或真实云存储凭证。
