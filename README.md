# SyncHub

[简体中文](README.md) | [English](README.en.md)

SyncHub 是面向开发者工作区的多设备同步平台。本仓库包含 Go API 服务端和 React 管理页面；最终用户通过配套的 SyncHub Desktop 桌面应用完成同步，不需要安装 CLI。

## 架构

```text
SyncHub Desktop -> REST API -> SyncHub Server -> PostgreSQL + Object Storage
                              -> React Admin
```

核心能力：

- 用户认证、设备注册与同步游标
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

## 验证

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-mvp.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-docker-image.ps1 -Version 0.2.0 -Image synchub:0.2.0
```

`test-mvp.ps1` 构建 React 管理页面，并运行 Go 格式化、vet 和全量测试。Docker smoke 会验证镜像标签、运行时文件、`/readyz` 与 `/version`。

## 发布与部署

构建并验证 API-only 发行包：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build-release.ps1 -Version 0.2.0
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify-release.ps1 -Version 0.2.0
```

Docker Compose：

```bash
export JWT_SECRET=change-me
export DATABASE_URL='postgresql://user:password@host:5432/synchub?sslmode=require'
export SYNCHUB_IMAGE=ghcr.io/bruceblink/synchub:0.2.0
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
