# SyncHub

[简体中文](README.md) | [English](README.en.md)

SyncHub 是面向开发者工作区和应用数据的多设备同步平台。本仓库包含 Go API 服务端和 React 管理页面；它为 KVideo、LatestNews 等应用提供用户历史和收藏同步能力。

## 架构

```text
SyncHub Desktop -> REST API -> SyncHub Server -> PostgreSQL + Object Storage
                              -> React Admin
```

核心能力：

- 用户认证、设备注册与同步游标
- 应用限定 API Key、订阅权益校验与用户元数据同步
- 文件上传、下载、目录管理与软删除
- 文件版本、固定版本与历史恢复
- 变更事件、冲突记录和回收站
- PostgreSQL 元数据与 Local FS / S3-compatible 存储抽象
- 健康检查、指标、Swagger 和 React 管理页面

## 快速开始

准备 `.env`，其中 `DATABASE_URL` 在所有环境中都是必需项：

```dotenv
DATABASE_URL=postgresql://user:password@host:5432/synchub?sslmode=require
JWT_SECRET=replace-with-a-long-random-secret
```

构建管理页面并启动 API：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build-web-admin.ps1
go run .\cmd\synchub-api
```

服务端默认监听 `http://localhost:8765`，启动时自动执行缺失的 PostgreSQL migration。进程环境变量和部署 secret 的优先级高于 `.env`。

常用端点：

- `GET /version`
- `GET /healthz`
- `GET /readyz`，包含 database 和 storage 检查
- `GET /metrics`
- `GET /swagger/`
- `GET /swagger/openapi.yaml`

## 桌面客户端

配套客户端位于 `F:\project\synchub-desktop`。在桌面应用中配置服务端地址、登录并初始化工作区后，应用会自动执行后台同步，并提供文件、版本、冲突、设备和回收站管理。

旧版本登录配置与工作区 registry 可继续读取以支持无损升级，但服务端发行物不再包含 CLI。

## 应用数据同步

用户使用 SyncHub 账号登录后，可用 Bearer Token 创建仅限 `kvideo` 或 `latestnews` 的 API Key。Key 只在创建响应中返回一次，服务端仅保存哈希；撤销后立即失效。每个 Key 只可读写对应应用支持的 collection：

- `KVideo`：`watch-history`、`favorites`
- `LatestNews`：`reading-history`、`favorites`

应用通过 `X-API-Key` 调用：

```text
GET /api/v1/metadata/kvideo/watch-history
PUT /api/v1/metadata/kvideo/watch-history
{ "payload": [...] }
```

服务端按用户、应用与 collection 隔离文档，每次写入增加 `version`。账户订阅为 `active` 时才允许创建或使用 API Key；支付和续费系统可直接更新 `subscriptions` 表的计划、状态与到期时间。

元数据接口允许浏览器跨域调用，所有实际读写请求都必须携带有效的 `X-API-Key`。服务端会校验 Key 是否未撤销、订阅是否有效，以及 Key 是否绑定到请求的应用；未通过校验的请求会直接被拒绝。

## 验证

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-mvp.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-docker-image.ps1 -Version 0.2.1 -Image synchub:0.2.1
```

`test-mvp.ps1` 构建 React 管理页面，并运行 Go 格式化、vet 和全量测试。Docker smoke 会验证镜像标签、运行时文件、`/readyz` 与 `/version`。

## 发布与部署

构建并验证 API-only 发行包：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build-release.ps1 -Version 0.2.1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify-release.ps1 -Version 0.2.1
```

Docker Compose：

```bash
export JWT_SECRET=change-me
export DATABASE_URL='postgresql://user:password@host:5432/synchub?sslmode=require'
export SYNCHUB_IMAGE=ghcr.io/bruceblink/synchub:0.2.1
docker compose -f docker-compose.release.yml up -d
```

Fly.io：

```powershell
fly apps create synchub-your-name
fly volumes create synchub_data --app synchub-your-name --region nrt --size 20
fly secrets set --app synchub-your-name JWT_SECRET="replace-with-a-long-random-secret"
fly secrets set --app synchub-your-name DATABASE_URL="postgresql://user:password@host:5432/synchub?sslmode=require"
fly deploy --config .\fly.toml
```

详细说明：

- [用户指南](docs/user-guide.md)
- [部署设计](docs/design/08-deployment.md)
- [发行检查清单](docs/release-checklist.md)
- [路线图](docs/roadmap/ROADMAP.md)
