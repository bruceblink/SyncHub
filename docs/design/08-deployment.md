# 部署设计

## 首期部署形态

Phase 1 以 Docker 镜像为交付物，在 Linux 服务器使用 Docker Compose 部署：

- `synchub-api`
- PostgreSQL metadata database via `DATABASE_URL`
- Local object storage in `/data/storage`
- Persistent Docker volume mounted at `/data`
- Windows 仅作为本地开发和发布前验证环境

Fly.io 也是首期支持的 Docker 镜像部署目标：

- 单个 Fly Machine 运行 `synchub-api`
- Fly Volume `synchub_data` 挂载到 `/data`
- PostgreSQL metadata database via `DATABASE_URL`
- Local object storage in `/data/storage`
- `JWT_SECRET` 和 `DATABASE_URL` 使用 Fly secrets 管理
- 由于 Fly Volume 不自动复制，MVP 不做多 Machine 横向扩展

Later 按明确需求再评估：

- `redis`
- `synchub-worker`
- `minio`（本地模拟 S3-compatible storage）
- `mysql`（生产或多人部署）

## 环境变量

必需：

- `JWT_SECRET`

可选：

- `DATABASE_DRIVER`，部署文件默认 `postgres`，也可省略并从 `DATABASE_URL` 推断
- `DATABASE_URL`，PostgreSQL 连接串；部署时必须通过环境变量或 secret 提供
- `DATABASE_SCHEMA`，可选 PostgreSQL schema，测试隔离或多环境共用数据库时使用
- `STORAGE_BACKEND`，镜像内默认 `local`
- `LOCAL_STORAGE_ROOT`，镜像内默认 `/data/storage`
- `LOG_LEVEL`
- `HTTP_ADDR`，默认 `:8765`
- `UPLOAD_CHUNK_SIZE`
- `UPLOAD_SESSION_TTL_SECONDS`
- `UPLOAD_CLEANUP_INTERVAL_SECONDS`
- `CLEANUP_BATCH_LIMIT`，默认 `1000`
- `VERSION_CLEANUP_INTERVAL_SECONDS`，默认跟随 `UPLOAD_CLEANUP_INTERVAL_SECONDS`
- `VERSION_RETENTION_MIN_VERSIONS`，默认 `20`
- `VERSION_RETENTION_MAX_AGE_DAYS`，默认 `30`，设为 `0` 可禁用版本历史自动清理

Later adapter 配置：

- `REDIS_URL`
- `S3_ENDPOINT`
- `S3_BUCKET`
- `S3_REGION`
- `S3_ACCESS_KEY_ID`
- `S3_SECRET_ACCESS_KEY`

## Linux 服务器快速部署

```bash
docker pull ghcr.io/bruceblink/synchub:0.1.1
docker run -d --name synchub-api \
  -p 8765:8765 \
  -e JWT_SECRET=change-me \
  -e DATABASE_DRIVER=postgres \
  -e DATABASE_URL="$DATABASE_URL" \
  -v synchub-data:/data \
  ghcr.io/bruceblink/synchub:0.1.1
```

使用 Compose：

```bash
export JWT_SECRET=change-me
export DATABASE_URL='postgresql://user:password@host:5432/synchub?sslmode=require'
export SYNCHUB_IMAGE=ghcr.io/bruceblink/synchub:0.1.1
docker compose -f docker-compose.release.yml up -d
```

## Fly.io 快速部署

```powershell
# Edit fly.toml: set app name and primary_region.
fly apps create synchub-your-name
fly volumes create synchub_data --app synchub-your-name --region nrt --size 1
fly secrets set --app synchub-your-name JWT_SECRET="replace-with-a-long-random-secret"
fly secrets set --app synchub-your-name DATABASE_URL="postgresql://user:password@host:5432/synchub?sslmode=require"
fly deploy --config .\fly.toml
curl.exe -fsS https://synchub-your-name.fly.dev/readyz
```

Automatic Fly.io deployment is handled by the Fly.io GitHub integration. The repository CI workflow stays test-only, and Fly.io reports a separate deployment check on push.

### Cloudflare 自定义域名

域名托管在 Cloudflare 时，先在 Fly 上添加证书并查看 DNS 记录：

```powershell
$env:FLY_APP = "synchub-your-name"
$env:SYNCHUB_DOMAIN = "sync.example.com"
fly certs add $env:SYNCHUB_DOMAIN --app $env:FLY_APP
fly certs setup $env:SYNCHUB_DOMAIN --app $env:FLY_APP
```

在 Cloudflare DNS 中添加 `fly certs setup` 输出的 `AAAA` 或 `CNAME` 记录。首次配置建议使用 `DNS only`，证书验证通过后再按需开启 Cloudflare 代理；开启代理时补充 Fly 输出的 ownership `TXT` 记录。最后检查：

```powershell
fly certs check $env:SYNCHUB_DOMAIN --app $env:FLY_APP
curl.exe -fsS "https://$env:SYNCHUB_DOMAIN/readyz"
```

## 数据卷

- PostgreSQL 数据库由外部服务持久化，并需要独立备份。
- Local FS storage root 必须通过 `/data/storage` 持久化。
- staging storage 可以和 object storage 放在同一 volume，但需要后台清理策略。
- Fly.io 部署必须保持单实例，除非后续引入对象存储复制方案。

## 发布流程

1. Tag 触发 Release workflow。
2. Linux runner 运行 MVP gate。
3. 构建并 smoke-test Docker image。
4. 推送 `ghcr.io/bruceblink/synchub:<version>`、`:<tag>`、`:latest`。
5. 发布 GitHub Release，附带 `docker-compose.release.yml`、`fly.toml`、辅助二进制 archives 和 `SHA256SUMS.txt`。
6. Linux 服务器拉取新镜像并重启 API container。
7. 健康检查通过后开放流量。

## Docker 构建排查

镜像构建依赖 Docker 能拉取基础镜像：

- `golang:1.26-alpine`
- `alpine:3.22`

构建时可以通过 `--build-arg VERSION=0.0.1` 写入 `/version` 返回的版本号。
构建时可以通过 `--build-arg GOPROXY=https://goproxy.cn,direct` 指定 Go module proxy；`docker-compose.yml` 默认使用 `${GOPROXY:-https://goproxy.cn,direct}`。
构建时可以通过 `--build-arg GO_IMAGE=...` 和 `--build-arg RUNTIME_IMAGE=...` 覆盖基础镜像源。
本地网络对容器 NAT 不稳定时，compose 构建阶段使用 `build.network: host`，单独构建可使用 `docker build --network=host ...`。

如果 `docker build` 在 `load metadata` 阶段失败，并出现 `failed to resolve source metadata`、`registry-1.docker.io` 连接超时或代理提示，通常说明 Docker Desktop 无法访问 Docker Hub，而不是项目编译失败。优先检查：

- Docker Desktop proxy / registry mirror 配置。
- 当前网络是否能访问 Docker Hub。
- 本机是否已有可用的基础镜像缓存。
- 构建容器内访问 Go module proxy 是否稳定。

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

- PostgreSQL 需要定期备份；SQLite 开发数据库可随本地数据目录备份。
- Local FS storage 按对象目录备份。
- 数据库和 storage 备份需要时间点接近，否则恢复后可能出现孤儿对象；孤儿对象由修复任务处理。
