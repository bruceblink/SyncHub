# 运维手册

## 常见问题

- 上传失败
- 上传会话过期
- 同步延迟

## 排查

- logs: API 请求日志包含 method、path、status、duration_ms 和 trace_id，日志级别由 `LOG_LEVEL` 控制。
- metrics: `GET /metrics` 输出 Prometheus text format 的请求计数、状态族计数和耗时累计值，可用 4xx / 5xx 状态族估算错误率。
- readiness: `GET /readyz` 同时检查数据库和 storage，成功响应会列出各组件状态。
- 过期上传会话由 API 进程内 worker 周期性标记为 `expired`，间隔由 `UPLOAD_CLEANUP_INTERVAL_SECONDS` 控制。
- 回收站项目默认保留 30 天，并由同一清理周期自动永久删除；`TRASH_RETENTION_DAYS=0` 可关闭该任务。
- 过期文件版本由 API 进程内 worker 周期性清理，间隔由 `VERSION_CLEANUP_INTERVAL_SECONDS` 控制；未设置时跟随上传清理间隔。
- 清理任务每轮处理数量由 `CLEANUP_BATCH_LIMIT` 控制，默认 `1000`。
- SyncHub Desktop 登录后会为所有已注册 workspace 启动进程内后台同步。
- 在桌面 Daemon 页面查看最近状态、失败次数和错误，并执行暂停、恢复或状态重置。
- 退出登录会停止后台任务；工作区暂停状态保存在 `.synchub/daemon-control.json` 中。

## Linux Docker 部署

发布版以 GHCR Docker 镜像为主交付物。Linux 服务器使用 Release 附带的 `docker-compose.release.yml`：

```bash
export JWT_SECRET=change-me
export DATABASE_URL='postgresql://user:password@host:5432/synchub?sslmode=require'
export SYNCHUB_IMAGE=ghcr.io/bruceblink/synchub:0.2.0
docker compose -f docker-compose.release.yml pull
docker compose -f docker-compose.release.yml up -d
docker compose -f docker-compose.release.yml ps
curl -fsS http://127.0.0.1:8765/readyz
```

升级时更新 `SYNCHUB_IMAGE` 后重复 `pull` 和 `up -d`。PostgreSQL 由 `DATABASE_URL` 指向的外部服务持久化；`synchub-data` volume 持久化 `/data/storage`。

## Fly.io 部署

Fly.io 使用项目 Dockerfile 和 `fly.toml` 从源码构建部署。MVP 推荐单 Machine + 单 Fly Volume：

```powershell
# Edit fly.toml: set app name and primary_region.
fly apps create synchub-your-name
fly volumes create synchub_data --app synchub-your-name --region nrt --size 1
fly secrets set --app synchub-your-name JWT_SECRET="replace-with-a-long-random-secret"
fly secrets set --app synchub-your-name DATABASE_URL="postgresql://user:password@host:5432/synchub?sslmode=require"
fly deploy --config .\fly.toml
fly checks list --app synchub-your-name
curl.exe -fsS https://synchub-your-name.fly.dev/readyz
```

自动部署由 Fly.io GitHub 集成负责；本仓库 CI 保持测试职责，Fly.io 会在 push 后报告独立部署检查。

Cloudflare 托管自定义域名时，用 Fly 证书命令生成 DNS 指引，然后在 Cloudflare 添加记录：

```powershell
$env:FLY_APP = "synchub-your-name"
$env:SYNCHUB_DOMAIN = "sync.example.com"
fly certs add $env:SYNCHUB_DOMAIN --app $env:FLY_APP
fly certs setup $env:SYNCHUB_DOMAIN --app $env:FLY_APP
fly certs check $env:SYNCHUB_DOMAIN --app $env:FLY_APP
curl.exe -fsS "https://$env:SYNCHUB_DOMAIN/readyz"
```

首选 `AAAA` 或 `CNAME` 记录并保持 Cloudflare `DNS only`；如需开启代理，补充 `fly certs setup` 输出的 ownership `TXT` 记录。

不要把当前 MVP 扩成多 Machine。Fly Volume 不会自动复制，多个实例会让 `/data/storage` 产生分叉。需要高可用时，先设计对象存储复制方案。

## 备份与恢复

所有环境的元数据都位于 PostgreSQL，应使用数据库提供方的 dump、快照或 PITR 能力。文件对象位于 `LOCAL_STORAGE_ROOT` 或 Fly Volume，需要单独备份或保留可恢复快照。

恢复前停止 API 写入，并选择时间点尽量一致的 PostgreSQL 与对象存储备份。先恢复数据库和对象，再启动单个 API 实例并检查 `/readyz`、文件下载和同步游标。
