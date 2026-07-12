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
- `APP_ENV`，默认 `production`，用于区分运行环境；所有环境都要求 PostgreSQL
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
- `OBJECT_GC_INTERVAL_SECONDS`，默认 `3600`；延迟复查并删除已无引用的不可变对象

Later adapter 配置：

- `REDIS_URL`
- `S3_ENDPOINT`
- `S3_BUCKET`
- `S3_REGION`
- `S3_ACCESS_KEY_ID`
- `S3_SECRET_ACCESS_KEY`

## Linux 服务器快速部署

```bash
docker pull ghcr.io/bruceblink/synchub:0.2.0
docker run -d --name synchub-api \
  -p 8765:8765 \
  -e JWT_SECRET=change-me \
  -e DATABASE_DRIVER=postgres \
  -e DATABASE_URL="$DATABASE_URL" \
  -v synchub-data:/data \
  ghcr.io/bruceblink/synchub:0.2.0
```

使用 Compose：

```bash
export JWT_SECRET=change-me
export DATABASE_URL='postgresql://user:password@host:5432/synchub?sslmode=require'
export SYNCHUB_IMAGE=ghcr.io/bruceblink/synchub:0.2.0
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

### 重新部署后绑定 Cloudflare 自定义域名

重新创建 Fly app 或迁移到新 app 后，旧 app 的域名证书、IPv4/IPv6 地址不会自动迁移。完成新 app 的部署和健康检查后，按以下步骤将域名切换到新 app。以下示例使用 `sync.example.com`，全部以 `fly certs setup` 的实际输出为准。

#### 1. 确认新应用可用

```powershell
$env:FLY_APP = "synchub-your-name"
$env:SYNCHUB_DOMAIN = "sync.example.com"

fly status --app $env:FLY_APP
fly checks list --app $env:FLY_APP
curl.exe -fsS "https://$env:FLY_APP.fly.dev/readyz"
```

继续前必须确认 Machine 为 `started`、健康检查通过，且 `/readyz` 中 database 和 storage 都为 `ready`。

#### 2. 为新应用申请域名证书和入口地址

```powershell
fly certs add $env:SYNCHUB_DOMAIN --app $env:FLY_APP
fly certs setup $env:SYNCHUB_DOMAIN --app $env:FLY_APP
fly ips list --app $env:FLY_APP
```

`fly certs setup` 会给出新 app 专属的 DNS 记录。不要复用旧 app 的 Fly IPv4、IPv6、CNAME target 或 `_fly-ownership` 值。

如果 `fly ips list` 只有 IPv6，额外分配共享 IPv4。这能让没有 IPv6 网络的客户端直接访问，也便于 Cloudflare 连接源站：

```powershell
fly ips allocate-v4 --shared --app $env:FLY_APP
fly ips list --app $env:FLY_APP
```

#### 3. 在 Cloudflare 更新 DNS

进入 Cloudflare DNS，删除或替换指向旧 Fly app 的 `sync` 记录。首次切换使用 `DNS only`，添加以下记录：

```text
Type: A
Name: sync
Content: <fly ips list 输出的 IPv4>
Proxy status: DNS only

Type: AAAA
Name: sync
Content: <fly certs setup 或 fly ips list 输出的 IPv6>
Proxy status: DNS only
```

也可以使用 `fly certs setup` 给出的 CNAME target，但不要同时保留冲突的 A、AAAA 或 CNAME 记录。等待 DNS 生效后检查证书：

```powershell
Resolve-DnsName $env:SYNCHUB_DOMAIN -Type A
Resolve-DnsName $env:SYNCHUB_DOMAIN -Type AAAA
fly certs check $env:SYNCHUB_DOMAIN --app $env:FLY_APP
```

当状态为 `Issued` 后，Fly 的 Let's Encrypt 证书已签发并可提供 HTTPS。

#### 4. 可选：重新开启 Cloudflare 代理

只有在证书状态为 `Issued` 后，才将 Cloudflare 记录切换为 `Proxied`。同时添加 `fly certs setup` 输出的 ownership 记录：

```text
Type: TXT
Name: _fly-ownership.sync
Content: <fly certs setup 输出的 ownership value>
```

在 Cloudflare 的 SSL/TLS 设置中选择 `Full (strict)`；不要使用 `Flexible`。`Full (strict)` 要求 Cloudflare 验证 Fly 已签发的源站证书，能避免降级到 HTTP。

#### 5. 验收和 525 排查

```powershell
fly certs check $env:SYNCHUB_DOMAIN --app $env:FLY_APP
curl.exe -fsS "https://$env:SYNCHUB_DOMAIN/readyz"
curl.exe -fsS "https://$env:SYNCHUB_DOMAIN/version"
```

若 Cloudflare 返回 `525 SSL handshake failed`，表示 Cloudflare 无法与源站完成 TLS 握手。依次检查：

1. `fly certs check` 是否为 `Issued`；若为 `Not verified`，先将 Cloudflare 代理关闭为 `DNS only`，并确认 A/AAAA/CNAME 已指向新 app。
2. Cloudflare DNS 中是否仍有指向旧 Fly app 的记录，或同时存在冲突的 A、AAAA、CNAME 记录。
3. 已开启代理时，是否添加了新 app 的 `_fly-ownership` TXT 记录，且 Cloudflare SSL/TLS 模式为 `Full (strict)`。
4. `fly status` 和 `fly checks list` 是否显示新 app 正常；用 `https://<app>.fly.dev/readyz` 排除应用自身故障。

验证成功后再删除旧 app 的证书和 IP，避免在 DNS 传播期间中断服务。

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

- PostgreSQL 需要通过数据库提供方的备份、快照或 PITR 能力定期保护。
- Local FS storage 按对象目录备份。
- 数据库和 storage 备份需要时间点接近，否则恢复后可能出现孤儿对象；孤儿对象由修复任务处理。
