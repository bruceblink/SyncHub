# 开发指南

## 工具链
- Rust stable
- PostgreSQL 16+
- SQLx CLI
- Docker / Docker Compose

## 推荐命令
```bash
cargo fmt --all
cargo clippy --workspace --all-targets -- -D warnings
cargo test --workspace
cargo run -p synchub-api
```

## 本地配置
服务端通过环境变量读取配置：
- `DATABASE_URL`
- `JWT_SECRET`
- `STORAGE_BACKEND=local`
- `LOCAL_STORAGE_ROOT=./.data/storage`
- `RUST_LOG=synchub=debug,tower_http=debug`

## Migration
```bash
sqlx migrate add <name>
sqlx migrate run
```

提交涉及 SQLx query macro 的代码前，需要保证离线元数据或数据库连接可用。

## 代码规范
- 所有 crate 使用 `rustfmt` 默认格式。
- Clippy warning 作为失败处理。
- API handler 保持薄层，只做提取、校验、调用 service 和响应转换。
- 数据库访问集中在 repository。
- 文件内容必须使用 streaming API，禁止在 handler 中一次性读取大文件到内存。
- 错误类型需要保留可排查上下文，但 API 响应不能泄漏敏感信息。

## 分支与提交
- 使用 feature branch。
- 提交信息使用 Conventional Commits，例如 `feat(api): add upload init endpoint`。
- 每个 PR 至少包含对应测试或说明不可测试原因。

## Definition of Done
- 功能代码合并。
- migration 可在空数据库执行。
- 单元测试和关键集成测试通过。
- `cargo fmt`、`cargo clippy`、`cargo test` 通过。
- 文档或 API spec 已同步更新。
