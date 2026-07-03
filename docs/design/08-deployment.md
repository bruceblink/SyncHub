# 部署设计

## 首期部署形态

Phase 1 使用 Docker Compose 部署：

- `synchub-api`
- SQLite database file for development
- `local-storage` volume

Later 按明确需求再评估：

- `redis`
- `synchub-worker`
- `minio`（本地模拟 S3-compatible storage）
- `postgres` 或 `mysql`（生产或多人部署）

## 环境变量

必需：

- `JWT_SECRET`
- `STORAGE_BACKEND`
- `LOCAL_STORAGE_ROOT`

可选：

- `DATABASE_DRIVER`，默认 `sqlite`
- `DATABASE_URL`，SQLite 默认 `./.data/synchub.db`
- `LOG_LEVEL`
- `HTTP_ADDR`，默认 `:8765`
- `UPLOAD_CHUNK_SIZE`
- `UPLOAD_SESSION_TTL_SECONDS`
- `UPLOAD_CLEANUP_INTERVAL_SECONDS`
- `CLEANUP_BATCH_LIMIT`，默认 `1000`
- `VERSION_RETENTION_MIN_VERSIONS`，默认 `20`
- `VERSION_RETENTION_MAX_AGE_DAYS`，默认 `30`，设为 `0` 可禁用版本历史自动清理

Later adapter 配置：

- `REDIS_URL`
- `S3_ENDPOINT`
- `S3_BUCKET`
- `S3_REGION`
- `S3_ACCESS_KEY_ID`
- `S3_SECRET_ACCESS_KEY`

## 数据卷

- SQLite 开发数据库文件必须持久化。
- Local FS storage root 必须持久化。
- staging storage 可以和 object storage 放在同一 volume，但需要后台清理策略。

## 发布流程

1. 构建镜像。
2. 运行 migration。
3. 滚动替换 API container。
4. 健康检查通过后开放流量。

## Docker 构建排查

镜像构建依赖 Docker 能拉取基础镜像：

- `golang:1.26-alpine`
- `alpine:3.22`

构建时可以通过 `--build-arg VERSION=0.0.1` 写入 `/version` 返回的版本号。

如果 `docker build` 在 `load metadata` 阶段失败，并出现 `failed to resolve source metadata`、`registry-1.docker.io` 连接超时或代理提示，通常说明 Docker Desktop 无法访问 Docker Hub，而不是项目编译失败。优先检查：

- Docker Desktop proxy / registry mirror 配置。
- 当前网络是否能访问 Docker Hub。
- 本机是否已有可用的基础镜像缓存。

## 健康检查

- `GET /version`: 当前服务名称和版本。
- `GET /healthz`: 进程存活。
- `GET /readyz`: 数据库、storage 可用。

## 指标

- `GET /metrics`: Prometheus text format，当前输出 API 请求总数和请求耗时累计值。
- metrics 是进程内指标；容器重启后会重新计数。

## API 文档

- Swagger UI: `GET /swagger/`
- OpenAPI YAML: `GET /swagger/openapi.yaml`

## 备份

- SQLite 开发数据库可随本地数据目录备份；生产级 PostgreSQL / MySQL 需要定期备份。
- Local FS storage 按对象目录备份。
- 数据库和 storage 备份需要时间点接近，否则恢复后可能出现孤儿对象；孤儿对象由修复任务处理。
