# 开发指南

## 工具链

- Go stable
- SQLite local database
- sqlc（PostgreSQL / MySQL adapter 阶段）
- golang-migrate 或 goose（PostgreSQL / MySQL adapter 阶段）
- Docker / Docker Compose

## 推荐命令

```bash
go fmt ./...
go vet ./...
go test ./...
go run ./cmd/synchub-api
go run ./cmd/synchub-cli login --server http://localhost:8080 --email user@example.com --password password
go run ./cmd/synchub-cli workspace init --path . --remote-path /workspace
go run ./cmd/synchub-cli manifest scan --path .
go run ./cmd/synchub-cli sync status --path .
```

## 本地配置

服务端通过环境变量读取配置：

- `DATABASE_DRIVER=sqlite`
- `DATABASE_URL=./.data/synchub.db`
- `JWT_SECRET`
- `STORAGE_BACKEND=local`
- `LOCAL_STORAGE_ROOT=./.data/storage`
- `UPLOAD_CLEANUP_INTERVAL_SECONDS=3600`
- `VERSION_RETENTION_MIN_VERSIONS=20`
- `VERSION_RETENTION_MAX_AGE_DAYS=30`
- `LOG_LEVEL=debug`

## Migration

开发默认 SQLite schema 由 API 启动时自动 bootstrap。

示例使用 golang-migrate：

```bash
migrate create -ext sql -dir migrations <name>
migrate -path migrations -database "$DATABASE_URL" up
```

PostgreSQL / MySQL adapter 阶段再启用独立 migration 工具。如果改用 goose，需要在项目 README 和 CI 中统一命令。

## SQL 生成

PostgreSQL / MySQL adapter 优先使用 sqlc：

```bash
sqlc generate
```

SQL 文件应按模块组织，例如：

```text
internal/db/queries/
  users.sql
  files.sql
  uploads.sql
  changes.sql
```

## 代码规范

- 所有 Go 代码通过 `gofmt`。
- 使用 `go vet ./...` 作为基础静态检查。
- API handler 保持薄层，只做提取、校验、调用 service 和响应转换。
- 数据库访问集中在 repository 或 sqlc query wrapper，业务层不直接依赖具体数据库 driver。
- 文件内容必须使用 streaming API，禁止在 handler 中一次性读取大文件到内存。
- 错误类型需要保留可排查上下文，但 API 响应不能泄漏敏感信息。
- context 必须从请求入口向下传递到 DB、storage 和外部调用。

## 分支与提交

- 使用 feature branch。
- 提交信息使用 Conventional Commits，例如 `feat(api): add upload init endpoint`。
- 每个 PR 至少包含对应测试或说明不可测试原因。

## Definition of Done

- 功能代码合并。
- SQLite bootstrap 或后续 migration 可在空数据库执行。
- 单元测试和关键集成测试通过。
- `go fmt ./...`、`go vet ./...`、`go test ./...` 通过。
- 文档或 API spec 已同步更新。
